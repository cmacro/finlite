package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// 店面收款附件：内容外置到 <账套目录>/attachments/YYYY/MM/<sha256>.<ext>，
// 库内只存 sha256 与 rel_path。内容寻址 → 同一张照片被多条记录引用时只存一份。

// PreMigrateAttachments 开库前置：把旧版 collection_attachments.content 里的 BLOB
// 写到账套 attachments/ 目录并回填 sha256/rel_path，供 v6 脚本重建表时复制。
// 幂等：表不存在、或已无 content 列时直接返回。失败必须中止开库（不能丢凭据）。
func (d *DB) PreMigrateAttachments(ctx context.Context) error {
	hasTable, hasContent, hasSHA, err := d.attachmentColumns(ctx)
	if err != nil || !hasTable || !hasContent {
		return err
	}
	if !hasSHA {
		for _, col := range []string{
			`ALTER TABLE collection_attachments ADD COLUMN sha256 TEXT NOT NULL DEFAULT ''`,
			`ALTER TABLE collection_attachments ADD COLUMN rel_path TEXT NOT NULL DEFAULT ''`,
		} {
			if _, err := d.pool.ExecContext(ctx, col); err != nil {
				return fmt.Errorf("升级附件表失败：%v", err)
			}
		}
	}

	rows, err := d.pool.QueryContext(ctx,
		`SELECT id, ext, content FROM collection_attachments WHERE content IS NOT NULL AND length(content) > 0`)
	if err != nil {
		return fmt.Errorf("读取旧版附件失败：%v", err)
	}
	type legacy struct {
		id   string
		ext  string
		data []byte
	}
	var items []legacy
	for rows.Next() {
		var it legacy
		if err := rows.Scan(&it.id, &it.ext, &it.data); err != nil {
			rows.Close()
			return fmt.Errorf("读取旧版附件失败：%v", err)
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("读取旧版附件失败：%v", err)
	}

	for _, it := range items {
		sha, rel, err := d.PutAttachmentFile(ctx, d.pool, it.ext, it.data)
		if err != nil {
			return fmt.Errorf("附件内容外置失败（%s）：%v", it.id, err)
		}
		if _, err := d.pool.ExecContext(ctx,
			`UPDATE collection_attachments SET sha256=?, rel_path=? WHERE id=?`, sha, rel, it.id); err != nil {
			return fmt.Errorf("回填附件指纹失败（%s）：%v", it.id, err)
		}
	}
	return nil
}

// attachmentColumns 探测附件表形态：是否存在、是否仍有 content 列、是否已有 sha256 列。
func (d *DB) attachmentColumns(ctx context.Context) (hasTable, hasContent, hasSHA bool, err error) {
	rows, err := d.pool.QueryContext(ctx, `PRAGMA table_info(collection_attachments)`)
	if err != nil {
		return false, false, false, fmt.Errorf("读取附件表结构失败：%v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid       int
			name, typ string
			notNull   int
			dflt      sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return false, false, false, fmt.Errorf("读取附件表结构失败：%v", err)
		}
		hasTable = true
		switch name {
		case "content":
			hasContent = true
		case "sha256":
			hasSHA = true
		}
	}
	return hasTable, hasContent, hasSHA, rows.Err()
}

// Queryer 只读查询能力：*sql.DB 与 *sql.Tx 都满足。
// 注意：写事务内必须传 *sql.Tx——单连接池（MaxOpenConns=1）下用 *sql.DB 查会等待
// 自己持有的事务提交，直接死锁。
type Queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// PutAttachmentFile 写入附件内容（内容寻址）：同 sha256 已存在则复用既有路径，不重复落盘。
// q 传当前事务（事务内调用）或连接池（开库前置迁移）。
func (d *DB) PutAttachmentFile(ctx context.Context, q Queryer, ext string, data []byte) (sha, rel string, err error) {
	if len(data) == 0 {
		return "", "", fmt.Errorf("附件内容为空")
	}
	sum := sha256.Sum256(data)
	sha = hex.EncodeToString(sum[:])
	ext = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))

	// 内容寻址去重：同一份内容只存一份，多条附件行共用同一个文件。
	var existing string
	qerr := q.QueryRowContext(ctx,
		`SELECT rel_path FROM collection_attachments WHERE sha256=? AND rel_path<>'' LIMIT 1`, sha).
		Scan(&existing)
	if qerr == nil && existing != "" {
		if abs, aerr := d.ledger.Abs(existing); aerr == nil {
			if _, serr := os.Stat(abs); serr == nil {
				return sha, existing, nil
			}
		}
	} else if qerr != nil && qerr != sql.ErrNoRows {
		return "", "", fmt.Errorf("查询附件指纹失败：%v", qerr)
	}

	dir := d.ledger.AttachDir(time.Now())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("创建附件目录失败：%v", err)
	}
	abs := filepath.Join(dir, sha+path.Ext("x."+ext))
	if err := writeFileAtomic(abs, data); err != nil {
		return "", "", err
	}
	rel, err = d.ledger.Rel(abs)
	if err != nil {
		return "", "", err
	}
	return sha, rel, nil
}

// ReadAttachmentFile 读取附件内容（rel_path 相对账套目录，越界直接拒绝）。
func (d *DB) ReadAttachmentFile(rel string) ([]byte, error) {
	abs, err := d.ledger.Abs(rel)
	if err != nil {
		return nil, fmt.Errorf("附件路径非法：%s", rel)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("附件文件缺失（%s），请重新上传", filepath.Base(abs))
		}
		return nil, fmt.Errorf("读取附件失败：%v", err)
	}
	return data, nil
}

// RemoveAttachmentFileIfUnused 删除附件文件，但仍被其他行引用时保留（内容寻址共享）。
func (d *DB) RemoveAttachmentFileIfUnused(ctx context.Context, q Queryer, sha, rel string) error {
	if rel == "" {
		return nil
	}
	if sha != "" {
		var refs int
		if err := q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM collection_attachments WHERE sha256=?`, sha).Scan(&refs); err != nil {
			return fmt.Errorf("统计附件引用失败：%v", err)
		}
		if refs > 0 {
			return nil
		}
	}
	abs, err := d.ledger.Abs(rel)
	if err != nil {
		return nil // 路径非法：不删账套外文件
	}
	if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除附件文件失败：%v", err)
	}
	return nil
}
