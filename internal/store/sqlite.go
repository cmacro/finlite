// Package store 拥有 SQLite 的打开/迁移/查询：WAL + 单写 mutex + database/sql 裸 SQL。
// 业务表结构见 upgrade/00-latest-schema.sql（与业务文档 §8 同步），
// 增量脚本见 upgrade/NN-*.sql，按 PRAGMA user_version 顺序执行（见 migrate.go）。
package store

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed upgrade/00-latest-schema.sql
var baselineSchema string

// SchemaVersion 当前 schema 版本：等于最后一个增量脚本的 to 版本。
// 新库直接建 baseline 并置 user_version=SchemaVersion；旧库按脚本递增升级。
const SchemaVersion = 3

// DB 封装单写互斥的 *sql.DB。读多并发由 database/sql 池处理，写路径统一加锁。
type DB struct {
	mu   sync.Mutex
	pool *sql.DB
	path string
}

// DefaultPath 默认库路径：可执行文件同目录 finlite.db（单机单账套 MVP）。
func DefaultPath() string {
	if p := os.Getenv("FINLITE_DB"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return "finlite.db"
	}
	return filepath.Join(filepath.Dir(exe), "finlite.db")
}

// Open 打开（不存在则创建）并执行迁移 + PRAGMA + 种子数据。
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败：%v", err)
	}
	pool, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败：%v", err)
	}
	pool.SetMaxOpenConns(1)
	db := &DB{pool: pool, path: path}
	if err := db.migrate(); err != nil {
		_ = pool.Close()
		return nil, err
	}
	if err := db.seed(); err != nil {
		_ = pool.Close()
		return nil, err
	}
	return db, nil
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
