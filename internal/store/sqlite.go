// Package store 拥有 SQLite 的打开/迁移/查询：WAL + 单写 mutex + database/sql 裸 SQL。
// 业务表结构见 upgrade/00-latest-schema.sql（与业务文档 §8 同步），
// 增量脚本见 upgrade/NN-*.sql，按 PRAGMA user_version 顺序执行（见 migrate.go）。
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed upgrade/00-latest-schema.sql
var fullSchema string

// SchemaVersion 当前 schema 版本：等于最高增量脚本的文件名前缀（版本号可不连续），
// 也等于全量脚本版本头的 TO（见 verifyBaselineHeader）。
// 新库直接执行全量脚本并置 user_version=SchemaVersion；旧库按脚本目标版本顺序升级。
const SchemaVersion = 6

// DB 封装单写互斥的 *sql.DB。读多并发由 database/sql 池处理，写路径统一加锁。
type DB struct {
	mu     sync.Mutex
	pool   *sql.DB
	path   string
	ledger Ledger
}

// DefaultPath 默认库路径：<data root>/ledgers/ledger-1001/finlite.db（单机单账套 MVP）。
// FINLITE_DB 可覆盖（调试/测试用），此时账套根目录退化为库文件所在目录。
func DefaultPath() string {
	if p := os.Getenv("FINLITE_DB"); p != "" {
		return p
	}
	return DefaultLedger().DBPath()
}

// Open 按库文件路径打开（不校验账套身份，测试与 FINLITE_DB 调试用）。
// 账套根目录取库文件所在目录，便于原始文件/备份相对定位。
func Open(path string) (*DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("库路径无效：%s", path)
	}
	return openAt(Ledger{Code: DefaultLedgerCode, Root: filepath.Dir(abs)}, false)
}

// OpenLedger 打开指定账套：建目录 → 建/升级库 → 校验账套身份（目录名 / ledger.json / 库内）。
func OpenLedger(l Ledger) (*DB, error) {
	if err := l.checkDirCode(); err != nil {
		return nil, err
	}
	if err := l.Ensure(); err != nil {
		return nil, err
	}
	return openAt(l, true)
}

// openAt 打开（不存在则创建）并执行迁移 + PRAGMA + 账套身份 + 种子数据。
func openAt(l Ledger, enforceIdentity bool) (*DB, error) {
	path := l.DBPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败：%v", err)
	}
	pool, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败：%v", err)
	}
	pool.SetMaxOpenConns(1)
	db := &DB{pool: pool, path: path, ledger: l}
	// 附件外置前置：旧库 BLOB 先写盘回填指纹，v6 脚本再重建表去掉 content 列。
	if err := db.PreMigrateAttachments(context.Background()); err != nil {
		_ = pool.Close()
		return nil, err
	}
	if err := db.migrate(); err != nil {
		_ = pool.Close()
		return nil, err
	}
	if err := db.ensureLedgerIdentity(enforceIdentity); err != nil {
		_ = pool.Close()
		return nil, err
	}
	if err := db.seed(); err != nil {
		_ = pool.Close()
		return nil, err
	}
	return db, nil
}

// ensureLedgerIdentity 账套三角互校：目录名 ↔ ledger.json ↔ 库内 ledger_profile。
// 新库首次写入；enforceIdentity 时三方不一致直接拒绝打开（防把 A 账套的库拷进 B 目录）。
func (d *DB) ensureLedgerIdentity(enforce bool) error {
	var code, name, createdAt string
	err := d.pool.QueryRow(`SELECT code, display_name, created_at FROM ledger_profile WHERE id=1`).
		Scan(&code, &name, &createdAt)
	switch {
	case err == sql.ErrNoRows:
		id, ierr := d.ledger.ReadIdentity()
		if ierr != nil {
			return ierr
		}
		display, created := d.ledger.Code, time.Now().Format(time.RFC3339)
		if id.Code != "" {
			display, created = id.DisplayName, id.CreatedAt
		}
		if display == "" {
			display = d.ledger.Code
		}
		if _, err := d.pool.Exec(`INSERT INTO ledger_profile (id, code, display_name, created_at)
			VALUES (1, ?, ?, ?)`, d.ledger.Code, display, created); err != nil {
			return fmt.Errorf("写入账套身份失败：%v", err)
		}
		return nil
	case err != nil:
		return fmt.Errorf("读取账套身份失败：%v", err)
	}
	if !enforce {
		return nil
	}
	if code != d.ledger.Code {
		return fmt.Errorf("账套目录不匹配：当前目录 %s，库内账套 %s。请把 finlite.db 放回 %s 目录，或从 backups/ 恢复本账套的备份",
			d.ledger.Code, code, code)
	}

	id, ierr := d.ledger.ReadIdentity()
	if ierr != nil {
		return ierr
	}
	if id.Code != "" && id.Code != code {
		return fmt.Errorf("账套身份文件与库内账套不一致：ledger.json 为 %s，库内为 %s。请确认 ledger.json 是否属于本账套", id.Code, code)
	}
	if id.Code == "" {
		// 老账套补一份身份文件，便于整目录交接时自证。
		write := LedgerIdentity{Code: code, DisplayName: name, LedgerDir: d.ledger.Root, CreatedAt: createdAt}
		data, _ := json.MarshalIndent(write, "", "  ")
		if err := os.WriteFile(d.ledger.IdentityPath(), append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("补写账套身份文件失败：%v", err)
		}
	}
	return nil
}

// Pool 裸连接池，供 Service 组装查询使用（写事务请走 WithWriteTx）。
func (d *DB) Pool() *sql.DB { return d.pool }

// Close 关闭连接池。恢复备份前须先 Close + checkpoint，避免 WAL 未落盘。
func (d *DB) Close() error { return d.pool.Close() }

// Checkpoint 把 WAL 内容落盘并截断，备份/恢复前调用保证文件自洽。
func (d *DB) Checkpoint() error {
	if _, err := d.pool.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return fmt.Errorf("检查点失败：%v", err)
	}
	return nil
}

// Path 返回库文件路径（备份/恢复用）。
func (d *DB) Path() string { return d.path }

// Ledger 返回当前账套（账套目录、原始文件归档、附件、备份路径的唯一来源）。
func (d *DB) Ledger() Ledger { return d.ledger }

// WithWriteTx 单写互斥事务封装。
func (d *DB) WithWriteTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	tx, err := d.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启事务失败：%v", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交事务失败：%v", err)
	}
	return nil
}

// seed 写入 fee_rates + accounts 最小集（INSERT OR IGNORE，幂等）。
// 费率：借记 0.005 / 贷记 0.006 / 微信支付宝 0.0025（业务文档 §6.2）。
func (d *DB) seed() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now().Format(time.RFC3339)
	rates := [][2]string{
		{"debit_card", "0.005"},
		{"credit_card", "0.006"},
		{"wechat", "0.0025"},
		{"alipay", "0.0025"},
	}
	for _, r := range rates {
		if _, err := d.pool.Exec(
			`INSERT OR IGNORE INTO fee_rates (pay_method, rate, updated_at) VALUES (?, ?, ?)`,
			r[0], r[1], now,
		); err != nil {
			return fmt.Errorf("初始化费率失败：%v", err)
		}
	}
	accounts := [][4]string{
		{"1002", "银行存款", "", "asset"},
		{"6001", "主营业务收入", "", "income"},
		{"6401", "主营业务成本", "", "cost"},
		{"6601", "销售费用", "", "expense"},
		{"6602", "管理费用", "", "expense"},
		{"6603", "财务费用", "", "expense"},
	}
	for _, a := range accounts {
		if _, err := d.pool.Exec(
			`INSERT OR IGNORE INTO accounts (code, name, parent_code, account_type, is_active) VALUES (?, ?, ?, ?, 1)`,
			a[0], a[1], nullIfEmpty(a[2]), a[3],
		); err != nil {
			return fmt.Errorf("初始化科目失败：%v", err)
		}
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
