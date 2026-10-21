package store

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 证据层（原始文件）：文件本体在 <账套目录>/raw/archive/YYYY/MM/，
// 库内 raw_files 只存内容指纹与相对路径。规则：只增不改，去重靠 sha256。
// 业务依据：docs/business/00-overview-health-finance-flow.md §8（原始数据与分析数据分离）。

// RawFile 原始文件清单行（含校验状态，供设置页展示）。
type RawFile struct {
	SHA256     string `json:"sha256"`
	RelPath    string `json:"relPath"`
	OrigName   string `json:"origName"`
	SourceType string `json:"sourceType"` // bank_flow / order_flow / unknown
	SizeBytes  int64  `json:"sizeBytes"`
	RowCount   int    `json:"rowCount"`
	IngestedAt string `json:"ingestedAt"`
	Operator   string `json:"operator"`
	Note       string `json:"note"`
	Status     string `json:"status"` // ok / missing / modified / unregistered
}

// RegisterRawReq 登记一份原始文件：内容指纹 + 归档落盘 + 写清单（同一内容只落一份）。
type RegisterRawReq struct {
	OrigName   string
	SourceType string
	Data       []byte
	RowCount   int
	Operator   string
	Note       string
}

// RegisterRawFile 先算 sha256，命中清单直接复用；未命中则写入账套归档目录再登记。
// 返回值 Created 表示本次是否新落盘（false = 同一内容已存在，复用）。
func (d *DB) RegisterRawFile(ctx context.Context, req RegisterRawReq) (RawFile, bool, error) {
	if len(req.Data) == 0 {
		return RawFile{}, false, fmt.Errorf("原始文件内容为空，无法登记")
	}
	origName := filepath.Base(strings.TrimSpace(req.OrigName))
	if origName == "" || origName == "." {
		return RawFile{}, false, fmt.Errorf("原始文件名不能为空")
	}
	sum := sha256.Sum256(req.Data)
	sha := hex.EncodeToString(sum[:])

	// 内容级去重：同一份文件反复导入只保留一份。
	existing, err := d.rawFileBySHA(ctx, sha)
	if err != nil && err != sql.ErrNoRows {
		return RawFile{}, false, err
	}
	if err == nil {
		if existing.RowCount < req.RowCount {
			_, _ = d.pool.ExecContext(ctx, `UPDATE raw_files SET row_count=? WHERE sha256=?`, req.RowCount, sha)
			existing.RowCount = req.RowCount
		}
		existing.Status = statusOfFile(existing.RelPath, d.ledger, sha)
		return existing, false, nil
	}

	now := time.Now()
	dir := d.ledger.RawArchiveDir(now)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return RawFile{}, false, fmt.Errorf("创建原始文件归档目录失败：%v", err)
	}
	name := fmt.Sprintf("%s-%s-%s%s", now.Format("20060102"), kebab(req.SourceType), sha[:8], strings.ToLower(filepath.Ext(origName)))
	abs := filepath.Join(dir, name)
	if err := writeFileAtomic(abs, req.Data); err != nil {
		return RawFile{}, false, fmt.Errorf("保存%w", err)
	}
	rel, err := d.ledger.Rel(abs)
	if err != nil {
		return RawFile{}, false, err
	}
	operator := strings.TrimSpace(req.Operator)
	if operator == "" {
		operator = "local"
	}
	out := RawFile{
		SHA256:     sha,
		RelPath:    rel,
		OrigName:   origName,
		SourceType: req.SourceType,
		SizeBytes:  int64(len(req.Data)),
		RowCount:   req.RowCount,
		IngestedAt: now.Format(time.RFC3339),
		Operator:   operator,
		Note:       req.Note,
		Status:     "ok",
	}
	if _, err := d.pool.ExecContext(ctx, `INSERT INTO raw_files
		(sha256, rel_path, orig_name, source_type, size_bytes, row_count, ingested_at, operator, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		out.SHA256, out.RelPath, out.OrigName, out.SourceType, out.SizeBytes,
		out.RowCount, out.IngestedAt, out.Operator, out.Note); err != nil {
		return RawFile{}, false, fmt.Errorf("登记原始文件失败：%v", err)
	}
	return out, true, nil
}

// ListRawFiles 原始文件清单（新→旧）。Status 由磁盘实况填充。
func (d *DB) ListRawFiles(ctx context.Context) ([]RawFile, error) {
	rows, err := d.pool.QueryContext(ctx, `SELECT sha256, rel_path, orig_name, source_type, size_bytes,
		row_count, ingested_at, operator, note FROM raw_files ORDER BY ingested_at DESC, orig_name`)
	if err != nil {
		return nil, fmt.Errorf("读取原始文件清单失败：%v", err)
	}
	defer rows.Close()
	out := []RawFile{}
	for rows.Next() {
		var f RawFile
		if err := rows.Scan(&f.SHA256, &f.RelPath, &f.OrigName, &f.SourceType, &f.SizeBytes,
			&f.RowCount, &f.IngestedAt, &f.Operator, &f.Note); err != nil {
			return nil, fmt.Errorf("读取原始文件清单失败：%v", err)
		}
		f.Status = statusOfFile(f.RelPath, d.ledger, f.SHA256)
		out = append(out, f)
	}
	return out, rows.Err()
}

// RawFileIssue 完整性校验发现的问题行。
type RawFileIssue struct {
	SHA256   string `json:"sha256"`
	RelPath  string `json:"relPath"`
	OrigName string `json:"origName"`
	Kind     string `json:"kind"` // missing / modified / unregistered
	Detail   string `json:"detail"`
}

// VerifyReport 完整性校验结果：登记项逐个重算指纹，并报告归档目录里未登记的文件。
type VerifyReport struct {
	LedgerCode   string         `json:"ledgerCode"`
	LedgerRoot   string         `json:"ledgerRoot"`
	CheckedAt    string         `json:"checkedAt"`
	Checked      int            `json:"checked"`
	OK           int            `json:"ok"`
	Missing      int            `json:"missing"`
	Modified     int            `json:"modified"`
	Unregistered int            `json:"unregistered"`
	Issues       []RawFileIssue `json:"issues"`
}

// VerifyRawFiles 重算归档文件指纹并与清单比对；不修改任何数据（只读校验）。
func (d *DB) VerifyRawFiles(ctx context.Context) (VerifyReport, error) {
	report := VerifyReport{
		LedgerCode: d.ledger.Code,
		LedgerRoot: d.ledger.Root,
		CheckedAt:  time.Now().Format(time.RFC3339),
		Issues:     []RawFileIssue{},
	}
	rows, err := d.pool.QueryContext(ctx, `SELECT sha256, rel_path, orig_name FROM raw_files ORDER BY rel_path`)
	if err != nil {
		return report, fmt.Errorf("读取原始文件清单失败：%v", err)
	}
	type item struct{ sha, rel, name string }
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.sha, &it.rel, &it.name); err != nil {
			rows.Close()
			return report, fmt.Errorf("读取原始文件清单失败：%v", err)
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return report, fmt.Errorf("读取原始文件清单失败：%v", err)
	}

	registered := make(map[string]bool, len(items))
	for _, it := range items {
		report.Checked++
		registered[it.rel] = true
		abs, aerr := d.ledger.Abs(it.rel)
		if aerr != nil {
			report.Missing++
			report.Issues = append(report.Issues, RawFileIssue{it.sha, it.rel, it.name, "missing", aerr.Error()})
			continue
		}
		got, herr := hashFile(abs)
		switch {
		case herr != nil && os.IsNotExist(herr):
			report.Missing++
			report.Issues = append(report.Issues, RawFileIssue{it.sha, it.rel, it.name, "missing", "归档文件不存在"})
		case herr != nil:
			report.Missing++
			report.Issues = append(report.Issues, RawFileIssue{it.sha, it.rel, it.name, "missing", herr.Error()})
		case got != it.sha:
			report.Modified++
			report.Issues = append(report.Issues, RawFileIssue{it.sha, it.rel, it.name, "modified",
				fmt.Sprintf("内容已变更：现值 %s", got[:8])})
		default:
			report.OK++
		}
	}

	// 归档目录里存在但未登记的文件（手工拷贝、旧版残留）单独报出。
	archRoot := filepath.Join(d.ledger.Root, "raw", "archive")
	_ = filepath.WalkDir(archRoot, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			return nil
		}
		rel, rerr := d.ledger.Rel(p)
		if rerr != nil || registered[rel] {
			return nil
		}
		report.Unregistered++
		report.Issues = append(report.Issues, RawFileIssue{"", rel, e.Name(), "unregistered", "归档目录中存在未登记文件"})
		return nil
	})
	sort.Slice(report.Issues, func(i, j int) bool { return report.Issues[i].RelPath < report.Issues[j].RelPath })
	return report, nil
}

// BundleResult 交接包导出结果。
type BundleResult struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	SizeBytes   int64  `json:"sizeBytes"`
	RawFiles    int    `json:"rawFiles"`
	Attachments int    `json:"attachments"`
	LedgerCode  string `json:"ledgerCode"`
}

// ExportBundle 导出账套交接包：db 用 VACUUM INTO 取一致快照，附 ledger.json、原始文件、
// 附件与 manifest.tsv（逐文件 sha256），落到 <data root>/packages/。
func (d *DB) ExportBundle(ctx context.Context) (BundleResult, error) {
	now := time.Now()
	pkgDir := d.ledger.PackageDir()
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return BundleResult{}, fmt.Errorf("创建交接包目录失败：%v", err)
	}
	zipPath := filepath.Join(pkgDir, fmt.Sprintf("%s_%s.zip", d.ledger.Code, now.Format("20060102_150405")))

	tmpDB, err := os.CreateTemp(pkgDir, "snapshot-*.db")
	if err != nil {
		return BundleResult{}, fmt.Errorf("创建快照失败：%v", err)
	}
	tmpDBPath := tmpDB.Name()
	_ = tmpDB.Close()
	_ = os.Remove(tmpDBPath) // VACUUM INTO 要求目标不存在
	defer os.Remove(tmpDBPath)
	if _, err := d.pool.ExecContext(ctx, `VACUUM INTO ?`, tmpDBPath); err != nil {
		return BundleResult{}, fmt.Errorf("生成库快照失败：%v", err)
	}

	out, err := os.Create(zipPath)
	if err != nil {
		return BundleResult{}, fmt.Errorf("创建交接包失败：%v", err)
	}
	zw := zip.NewWriter(out)
	var manifest []string
	rawCount, attachCount := 0, 0

	addFile := func(zipName, srcPath, rel string) error {
		sum, err := hashFile(srcPath)
		if err != nil {
			return fmt.Errorf("读取%s失败：%v", filepath.Base(srcPath), err)
		}
		st, err := os.Stat(srcPath)
		if err != nil {
			return fmt.Errorf("读取%s失败：%v", filepath.Base(srcPath), err)
		}
		w, err := zw.Create(zipName)
		if err != nil {
			return fmt.Errorf("写入交接包失败：%v", err)
		}
		f, err := os.Open(srcPath)
		if err != nil {
			return fmt.Errorf("打开%s失败：%v", filepath.Base(srcPath), err)
		}
		defer f.Close()
		if _, err := io.Copy(w, f); err != nil {
			return fmt.Errorf("写入交接包失败：%v", err)
		}
		manifest = append(manifest, fmt.Sprintf("%s\t%d\t%s", sum, st.Size(), rel))
		return nil
	}

	fail := func(err error) (BundleResult, error) {
		zw.Close()
		out.Close()
		os.Remove(zipPath)
		return BundleResult{}, err
	}

	if err := addFile(d.ledger.Code+"/finlite.db", tmpDBPath, "finlite.db"); err != nil {
		return fail(err)
	}
	if _, err := os.Stat(d.ledger.IdentityPath()); err == nil {
		if err := addFile(d.ledger.Code+"/ledger.json", d.ledger.IdentityPath(), "ledger.json"); err != nil {
			return fail(err)
		}
	}
	for _, sub := range []string{"raw/archive", "attachments"} {
		root := filepath.Join(d.ledger.Root, filepath.FromSlash(sub))
		walkErr := filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
			if err != nil || e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				return nil
			}
			rel, rerr := d.ledger.Rel(p)
			if rerr != nil {
				return nil
			}
			if err := addFile(d.ledger.Code+"/"+rel, p, rel); err != nil {
				return err
			}
			if strings.HasPrefix(rel, "raw/archive/") {
				rawCount++
			} else {
				attachCount++
			}
			return nil
		})
		if walkErr != nil {
			return fail(walkErr)
		}
	}
	manifestName := d.ledger.Code + "/manifest.tsv"
	if w, err := zw.Create(manifestName); err == nil {
		_, _ = w.Write([]byte("sha256\tsize\trel_path\n" + strings.Join(manifest, "\n") + "\n"))
	}
	if err := zw.Close(); err != nil {
		return fail(fmt.Errorf("写入交接包失败：%v", err))
	}
	if err := out.Sync(); err != nil {
		return fail(fmt.Errorf("写入交接包失败：%v", err))
	}
	if err := out.Close(); err != nil {
		return fail(fmt.Errorf("写入交接包失败：%v", err))
	}
	sum, err := hashFile(zipPath)
	if err != nil {
		return BundleResult{}, fmt.Errorf("计算交接包指纹失败：%v", err)
	}
	st, err := os.Stat(zipPath)
	if err != nil {
		return BundleResult{}, fmt.Errorf("读取交接包失败：%v", err)
	}
	return BundleResult{
		Path:        zipPath,
		SHA256:      sum,
		SizeBytes:   st.Size(),
		RawFiles:    rawCount,
		Attachments: attachCount,
		LedgerCode:  d.ledger.Code,
	}, nil
}

// rawFileBySHA 按指纹查清单。
func (d *DB) rawFileBySHA(ctx context.Context, sha string) (RawFile, error) {
	var f RawFile
	err := d.pool.QueryRowContext(ctx, `SELECT sha256, rel_path, orig_name, source_type, size_bytes,
		row_count, ingested_at, operator, note FROM raw_files WHERE sha256=?`, sha).
		Scan(&f.SHA256, &f.RelPath, &f.OrigName, &f.SourceType, &f.SizeBytes,
			&f.RowCount, &f.IngestedAt, &f.Operator, &f.Note)
	if err != nil {
		return RawFile{}, err
	}
	return f, nil
}

// statusOfFile 磁盘实况：ok / missing / modified（只算哈希，不改数据）。
func statusOfFile(rel string, l Ledger, wantSHA string) string {
	abs, err := l.Abs(rel)
	if err != nil {
		return "missing"
	}
	got, err := hashFile(abs)
	if err != nil {
		return "missing"
	}
	if got != wantSHA {
		return "modified"
	}
	return "ok"
}

// writeFileAtomic 先写临时文件再 rename：跨盘/中断都不会留下半个文件。
func writeFileAtomic(dst string, data []byte) error {
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".raw-*.tmp")
	if err != nil {
		return fmt.Errorf("写入临时文件失败：%v", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入临时文件失败：%v", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("落盘失败：%v", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("落盘失败：%v", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("写入归档文件失败：%v", err)
	}
	return nil
}

// hashFile 计算文件 sha256（流式，不整文件进内存）。
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// kebab 把 source_type 转成文件名片段（bank_flow → bank-flow）。
func kebab(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "raw"
	}
	return strings.ReplaceAll(s, "_", "-")
}

// SetRawFileRowCount 解析成功后回填原始文件的数据行数（去重命中时也更新）。
func (d *DB) SetRawFileRowCount(ctx context.Context, sha string, n int) error {
	if sha == "" || n <= 0 {
		return nil
	}
	if _, err := d.pool.ExecContext(ctx, `UPDATE raw_files SET row_count=? WHERE sha256=?`, n, sha); err != nil {
		return fmt.Errorf("回填原始文件行数失败：%v", err)
	}
	return nil
}

// RegisterRawFileInPlace 登记已在归档目录里的文件（存量迁移用）：只算指纹写清单，
// 不再复制文件；同一指纹已登记时返回既有记录。
func (d *DB) RegisterRawFileInPlace(ctx context.Context, relPath, origName, sourceType string, rowCount int, ingestedAt string) (RawFile, bool, error) {
	abs, err := d.ledger.Abs(relPath)
	if err != nil {
		return RawFile{}, false, err
	}
	sum, err := hashFile(abs)
	if err != nil {
		return RawFile{}, false, fmt.Errorf("读取%s失败：%v", filepath.Base(abs), err)
	}
	if existing, gerr := d.rawFileBySHA(ctx, sum); gerr == nil {
		return existing, false, nil
	} else if gerr != sql.ErrNoRows {
		return RawFile{}, false, gerr
	}
	st, err := os.Stat(abs)
	if err != nil {
		return RawFile{}, false, fmt.Errorf("读取%s失败：%v", filepath.Base(abs), err)
	}
	if ingestedAt == "" {
		ingestedAt = time.Now().Format(time.RFC3339)
	}
	out := RawFile{
		SHA256:     sum,
		RelPath:    relPath,
		OrigName:   filepath.Base(origName),
		SourceType: sourceType,
		SizeBytes:  st.Size(),
		RowCount:   rowCount,
		IngestedAt: ingestedAt,
		Operator:   "local",
		Note:       "存量迁移登记",
		Status:     "ok",
	}
	if _, err := d.pool.ExecContext(ctx, `INSERT INTO raw_files
		(sha256, rel_path, orig_name, source_type, size_bytes, row_count, ingested_at, operator, note)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		out.SHA256, out.RelPath, out.OrigName, out.SourceType, out.SizeBytes,
		out.RowCount, out.IngestedAt, out.Operator, out.Note); err != nil {
		return RawFile{}, false, fmt.Errorf("登记原始文件失败：%v", err)
	}
	return out, true, nil
}
