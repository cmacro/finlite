package store

import (
	"embed"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// upgradeFS 嵌入全部升级脚本。baseline 00-latest-schema.sql 与增量 NN-*.sql 同目录，
// 新库走 baseline，旧库按 user_version 顺序补齐增量（见 migrate）。
//
//go:embed upgrade/*.sql
var upgradeFS embed.FS

// upgradeStep 一个增量脚本。
type upgradeStep struct {
	from int
	to   int
	name string
	sql  string
}

// loadUpgradeSteps 读取 upgrade/ 下所有脚本，解析文件名前缀版本号并排序。
// 文件名规则：{fromVersion}-{kebab-description}.sql，如 01-receipt-no-unique.sql。
func loadUpgradeSteps() ([]upgradeStep, error) {
	entries, err := upgradeFS.ReadDir("upgrade")
	if err != nil {
		return nil, fmt.Errorf("读取升级脚本目录失败：%v", err)
	}
	var steps []upgradeStep
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		if strings.HasPrefix(name, "00-") {
			continue // baseline 由 00-latest-schema.sql 单独承载
		}
		from, err := strconv.Atoi(strings.SplitN(name, "-", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("升级脚本名不合规（需 {fromVersion}-{描述}.sql）：%s", name)
		}
		content, err := upgradeFS.ReadFile(path.Join("upgrade", name))
		if err != nil {
			return nil, fmt.Errorf("读取升级脚本失败：%s：%v", name, err)
		}
		steps = append(steps, upgradeStep{from: from, to: from + 1, name: name, sql: string(content)})
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].from < steps[j].from })
	// 版本必须连续，避免漏脚本导致半升级
	for i, s := range steps {
		if s.from != i+1 {
			return nil, fmt.Errorf("升级脚本版本不连续：期望 v%d，实际 %s", i+1, s.name)
		}
	}
	return steps, nil
}

// migrate 建 PRAGMA、按 user_version 判断是否需要升级，新库建 baseline，
// 旧库按脚本顺序逐个执行（每个脚本与其版本号在同一事务内提交）。
func (d *DB) migrate() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	pragmas := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA foreign_keys=ON`,
	}
	for _, p := range pragmas {
		if _, err := d.pool.Exec(p); err != nil {
			return fmt.Errorf("设置 %s 失败：%v", p, err)
		}
	}
	var version int
	if err := d.pool.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("读取 user_version 失败：%v", err)
	}
	if version >= SchemaVersion {
		return nil
	}
	steps, err := loadUpgradeSteps()
	if err != nil {
		return err
	}

	if version == 0 {
		// 新库：直接建 baseline（等价于顺序执行全部增量）并置最新版本。
		if _, err := d.pool.Exec(baselineSchema); err != nil {
			return fmt.Errorf("初始化基线表结构失败：%v", err)
		}
		if _, err := d.pool.Exec(fmt.Sprintf(`PRAGMA user_version=%d`, SchemaVersion)); err != nil {
			return fmt.Errorf("写入 user_version 失败：%v", err)
		}
		return nil
	}

	// 旧库：从 version 起按脚本递增升级，每步单独事务（DDL 与版本号同事务提交）。
	for _, s := range steps {
		if s.from != version {
			continue
		}
		tx, err := d.pool.Begin()
		if err != nil {
			return fmt.Errorf("开启升级事务失败（%s）：%v", s.name, err)
		}
		if _, err := tx.Exec(s.sql); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("执行升级脚本失败（%s）：%v", s.name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version=%d`, s.to)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("写入 user_version 失败（%s）：%v", s.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("提交升级事务失败（%s）：%v", s.name, err)
		}
		version = s.to
	}
	if version != SchemaVersion {
		return fmt.Errorf("升级未达目标版本：当前 v%d，期望 v%d（缺少后续脚本）", version, SchemaVersion)
	}
	return nil
}
