package store

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// upgradeFS 嵌入全部升级脚本。全量脚本 00-latest-schema.sql 与增量 NN-*.sql 同目录，
// 新库只执行全量脚本，旧库按文件名前缀版本顺序补齐增量（见 migrate）。
//
//go:embed upgrade/*.sql
var upgradeFS embed.FS

// fullSchemaName 全量脚本文件名，固定不变。
const fullSchemaName = "00-latest-schema.sql"

// upgradeStep 一个增量脚本。
// 文件名前缀即脚本引入的版本（目标版本，执行后写回 user_version），版本号可以不连续。
type upgradeStep struct {
	to     int    // 文件名前缀：执行本脚本后的 user_version
	compat int    // 头部 COMPAT；0 表示 Form A（改动落在 v1 或全新对象，不携带 COMPAT）
	name   string // 文件名
	sql    string // 脚本正文（含版本头，注释由 SQLite 忽略）
}

var (
	// 脚本名：{版本}-{kebab 描述}.sql，版本至少两位（02、07、101）。
	reScriptName = regexp.MustCompile(`^(\d{2,})-([a-z0-9]+(-[a-z0-9]+)*)\.sql$`)
	// 增量脚本版本头 Form A / Form B，以及全量脚本版本头（db-schema 技能 §3）。
	reHeaderA    = regexp.MustCompile(`^-- v(\d+): (.+)$`)
	reHeaderB    = regexp.MustCompile(`^-- v(\d+) \(compatible with v(\d+)\+\): (.+)$`)
	reFullHeader = regexp.MustCompile(`^-- v0 -> v(\d+) \(compatible with v(\d+)\+\): (.+)$`)
)

// loadUpgradeSteps 读取并校验 upgrade/ 下全部增量脚本，返回按目标版本升序排列的步骤。
//
// 版本号允许不连续（如 03、04、101）：删除某个脚本后留下的缺口不补号、不重排，
// 执行时只看「目标版本 > 当前 user_version」，缺口自然跳过（db-schema 技能 §1）。
func loadUpgradeSteps() ([]upgradeStep, error) {
	return loadUpgradeStepsFS(upgradeFS, "upgrade")
}

// loadUpgradeStepsFS 与 loadUpgradeSteps 逻辑一致，文件系统可注入（测试用）。
func loadUpgradeStepsFS(fsys fs.FS, dir string) ([]upgradeStep, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("读取升级脚本目录失败：%v", err)
	}
	steps := make([]upgradeStep, 0, len(entries))
	seen := make(map[int]string, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		if name == fullSchemaName {
			continue // 全量脚本由 00-latest-schema.sql 单独承载
		}
		m := reScriptName.FindStringSubmatch(name)
		if m == nil {
			return nil, fmt.Errorf("升级脚本名不合规（需 {版本}-{kebab描述}.sql）：%s", name)
		}
		ver, err := strconv.Atoi(m[1])
		if err != nil || ver < 2 {
			return nil, fmt.Errorf("升级脚本版本号不合规（需 ≥ 02，且不允许 01-*.sql）：%s", name)
		}
		if prev, ok := seen[ver]; ok {
			return nil, fmt.Errorf("升级脚本版本重复：v%d 同时出现在 %s 与 %s", ver, prev, name)
		}
		seen[ver] = name
		content, err := fs.ReadFile(fsys, path.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("读取升级脚本失败：%s：%v", name, err)
		}
		to, compat, err := parseScriptHeader(name, string(content))
		if err != nil {
			return nil, err
		}
		if to != ver {
			return nil, fmt.Errorf("%s：版本头 v%d 与文件名前缀 v%d 不一致", name, to, ver)
		}
		steps = append(steps, upgradeStep{to: to, compat: compat, name: name, sql: string(content)})
	}
	sort.Slice(steps, func(i, j int) bool { return steps[i].to < steps[j].to })
	return steps, nil
}

// splitHeader 切出版本头与正文：首行是版本头，其后必须空一行。
func splitHeader(name, content string) (head, body string, err error) {
	lines := strings.SplitN(content, "\n", 3)
	if len(lines) < 2 {
		return "", "", fmt.Errorf("%s：缺少版本头（首行必须是版本头，其后空一行）", name)
	}
	head = lines[0]
	if strings.TrimRight(head, " \t\r") != head {
		return "", "", fmt.Errorf("%s：版本头存在行尾空白：%q", name, head)
	}
	if strings.TrimSpace(lines[1]) != "" {
		return "", "", fmt.Errorf("%s：版本头后必须空一行", name)
	}
	return head, strings.Join(lines[2:], "\n"), nil
}

// checkHeaderText 校验版本头描述：英文、句首大写、不以句号结尾（db-schema 技能 §3）。
func checkHeaderText(name, desc string) error {
	if desc == "" {
		return fmt.Errorf("%s：版本头缺少描述", name)
	}
	if strings.HasSuffix(desc, ".") {
		return fmt.Errorf("%s：版本头描述不得以句号结尾：%q", name, desc)
	}
	for _, r := range desc {
		if r < 0x20 || r > 0x7e {
			return fmt.Errorf("%s：版本头必须为英文（首行仅允许 ASCII）：%q", name, desc)
		}
	}
	if c := desc[0]; c < 'A' || c > 'Z' {
		return fmt.Errorf("%s：版本头描述须以大写字母开头：%q", name, desc)
	}
	return nil
}

// parseScriptHeader 校验增量脚本首行版本头（仅元数据，不参与执行），
// 返回头部声明的目标版本与 COMPAT（0 表示 Form A）。实际升级内容以脚本正文为准。
func parseScriptHeader(name, content string) (to, compat int, err error) {
	head, _, err := splitHeader(name, content)
	if err != nil {
		return 0, 0, err
	}
	if m := reHeaderB.FindStringSubmatch(head); m != nil {
		to, _ = strconv.Atoi(m[1])
		compat, _ = strconv.Atoi(m[2])
		if err := checkHeaderText(name, m[3]); err != nil {
			return 0, 0, err
		}
		if to < 2 {
			return 0, 0, fmt.Errorf("%s：增量脚本版本头须为 v2 或更高：%q", name, head)
		}
		if compat < 2 || compat >= to {
			return 0, 0, fmt.Errorf("%s：COMPAT 须落在 v2 ~ v%d 之间（不得为 v0+/v1+，也不得 ≥ 脚本版本）：%q", name, to-1, head)
		}
		return to, compat, nil
	}
	if m := reHeaderA.FindStringSubmatch(head); m != nil {
		to, _ = strconv.Atoi(m[1])
		if err := checkHeaderText(name, m[2]); err != nil {
			return 0, 0, err
		}
		if to < 2 {
			return 0, 0, fmt.Errorf("%s：增量脚本版本头须为 v2 或更高：%q", name, head)
		}
		return to, 0, nil
	}
	return 0, 0, fmt.Errorf("%s：版本头不合规（应为 \"-- v{版本}: {描述}\" 或 \"-- v{版本} (compatible with v{兼容}+): {描述}\"）：%q", name, head)
}

// validateBookkeeping 校验版本账目（db-schema 技能 §3.1 / §6）：
// 全量脚本的 TO 是版本真相，须等于 SchemaVersion；有增量脚本时还须等于最高脚本版本；
// 全量脚本的 COMPAT 取脚本 COMPAT 的最大值，没有脚本携带 COMPAT 时为 v1+。
func validateBookkeeping(steps []upgradeStep) error {
	head, _, err := splitHeader(fullSchemaName, fullSchema)
	if err != nil {
		return err
	}
	m := reFullHeader.FindStringSubmatch(head)
	if m == nil {
		return fmt.Errorf("全量脚本版本头不合规（应为 \"-- v0 -> v{N} (compatible with v{C}+): {描述}\"）：%q", head)
	}
	to, _ := strconv.Atoi(m[1])
	compat, _ := strconv.Atoi(m[2])
	if err := checkHeaderText(fullSchemaName, m[3]); err != nil {
		return err
	}
	if to != SchemaVersion {
		return fmt.Errorf("全量脚本版本头 TO=v%d 与 SchemaVersion=v%d 不一致", to, SchemaVersion)
	}
	if compat < 1 {
		return fmt.Errorf("全量脚本 COMPAT 不得为 v0+：%q", head)
	}
	wantCompat := 1
	if len(steps) > 0 {
		if max := steps[len(steps)-1].to; max != SchemaVersion {
			return fmt.Errorf("最高增量脚本 v%d 与 SchemaVersion=v%d 不一致（全量脚本 TO 为版本真相）", max, SchemaVersion)
		}
		for _, s := range steps {
			if s.compat > wantCompat {
				wantCompat = s.compat
			}
		}
	}
	if compat != wantCompat {
		return fmt.Errorf("全量脚本 COMPAT=v%d+ 应为 v%d+（取脚本 COMPAT 最大值）", compat, wantCompat)
	}
	return nil
}

// migrate 建 PRAGMA、校验脚本、按目标版本顺序升级。
// 新库只执行全量脚本；旧库执行所有「目标版本 > 当前 user_version」的脚本，
// 每个脚本与其版本号在同一事务内提交，中途失败不会留下半升级。
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
	// 版本头与文件名前缀先做一致性校验，避免「脚本能跑但版本账目漂移」。
	steps, err := loadUpgradeSteps()
	if err != nil {
		return err
	}
	if err := validateBookkeeping(steps); err != nil {
		return err
	}

	var version int
	if err := d.pool.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("读取 user_version 失败：%v", err)
	}
	if version >= SchemaVersion {
		// 已是最新（或库比程序新：不降级、不改动，交由上层提示）。
		return nil
	}

	if version == 0 {
		// 新库：直接执行全量脚本（等价于顺序执行全部增量）并置最新版本。
		if _, err := d.pool.Exec(fullSchema); err != nil {
			return fmt.Errorf("执行全量脚本建库失败：%v", err)
		}
		if _, err := d.pool.Exec(fmt.Sprintf(`PRAGMA user_version=%d`, SchemaVersion)); err != nil {
			return fmt.Errorf("写入 user_version 失败：%v", err)
		}
		return nil
	}

	// 旧库：按目标版本升序补齐；版本号可跳过（如 v2 库依次执行 03、04、101）。
	for _, s := range steps {
		if s.to <= version {
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
