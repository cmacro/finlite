package services

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"finlite/internal/store"
)

// RawFileService 原始数据证据层（F8 设置页 / 导入中心共用）。
//
// 定位：银行/订单原始文件是账簿依据，必须保留并归属到账套。文件本体在
// <账套目录>/raw/archive/YYYY/MM/ 只增不改；库内 raw_files 只存 sha256 与相对路径。
// 导入链路：sha256 → 落盘归档 → 登记清单 → 再解析入库（解析失败也留证）。
type RawFileService struct {
	DB *store.DB
}

func NewRawFileService(db *store.DB) *RawFileService { return &RawFileService{DB: db} }

// Info 当前账套信息与原始文件统计。
func (s *RawFileService) Info(ctx context.Context) (LedgerInfo, error) {
	l := s.DB.Ledger()
	info := LedgerInfo{
		Code:        l.Code,
		Root:        l.Root,
		DBPath:      s.DB.Path(),
		RawInbox:    l.RawInboxDir(),
		RawArchive:  filepath.Join(l.Root, "raw", "archive"),
		Attachments: filepath.Join(l.Root, "attachments"),
		Backups:     l.BackupDir(),
		Packages:    l.PackageDir(),
	}
	if id, err := l.ReadIdentity(); err == nil && id.Code != "" {
		info.DisplayName = id.DisplayName
	}
	if info.DisplayName == "" {
		_ = s.DB.Pool().QueryRowContext(ctx, `SELECT display_name FROM ledger_profile WHERE id=1`).Scan(&info.DisplayName)
	}
	if info.DisplayName == "" {
		info.DisplayName = l.Code
	}
	files, err := s.DB.ListRawFiles(ctx)
	if err != nil {
		return info, err
	}
	info.RawFileCount = len(files)
	for _, f := range files {
		info.RawBytes += f.SizeBytes
	}
	return info, nil
}

// List 原始文件清单（含磁盘实况：ok / missing / modified）。
func (s *RawFileService) List(ctx context.Context) ([]EvidenceFile, error) {
	files, err := s.DB.ListRawFiles(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]EvidenceFile, 0, len(files))
	for _, f := range files {
		out = append(out, EvidenceFile{
			SHA256: f.SHA256, RelPath: f.RelPath, OrigName: f.OrigName, SourceType: f.SourceType,
			SizeBytes: f.SizeBytes, RowCount: f.RowCount, IngestedAt: f.IngestedAt,
			Operator: f.Operator, Note: f.Note, Status: f.Status,
		})
	}
	return out, nil
}

// Verify 完整性校验：重算归档文件指纹并与清单比对，报告缺失/被改/未登记文件。
func (s *RawFileService) Verify(ctx context.Context) (EvidenceVerify, error) {
	r, err := s.DB.VerifyRawFiles(ctx)
	if err != nil {
		return EvidenceVerify{}, err
	}
	out := EvidenceVerify{
		LedgerCode: r.LedgerCode, LedgerRoot: r.LedgerRoot, CheckedAt: r.CheckedAt,
		Checked: r.Checked, OK: r.OK, Missing: r.Missing, Modified: r.Modified,
		Unregistered: r.Unregistered, Issues: make([]EvidenceIssue, 0, len(r.Issues)),
	}
	for _, it := range r.Issues {
		out.Issues = append(out.Issues, EvidenceIssue{
			SHA256: it.SHA256, RelPath: it.RelPath, OrigName: it.OrigName,
			Kind: it.Kind, Detail: it.Detail,
		})
	}
	return out, nil
}

// ExportBundle 导出账套交接包（库快照 + 原始文件 + 附件 + manifest.tsv）。
func (s *RawFileService) ExportBundle(ctx context.Context) (EvidenceBundle, error) {
	r, err := s.DB.ExportBundle(ctx)
	if err != nil {
		return EvidenceBundle{}, err
	}
	return EvidenceBundle{
		Path: r.Path, SHA256: r.SHA256, SizeBytes: r.SizeBytes,
		RawFiles: r.RawFiles, Attachments: r.Attachments, LedgerCode: r.LedgerCode,
	}, nil
}

// MigrateLegacy 存量迁移（幂等）：旧版 `<账套>/raw/*` 收进 raw/inbox/，
// `raw/archived/*` 收进 raw/archive/legacy/，逐文件补登记指纹，并回填
// import_batches.raw_sha256；匹配不上的历史批次保持空值（标「缺原始文件」）。
func (s *RawFileService) MigrateLegacy(ctx context.Context) (EvidenceMigrate, error) {
	l := s.DB.Ledger()
	var out EvidenceMigrate

	// 1) 旧版投放区 raw/*.tsv → raw/inbox/
	rawDir := filepath.Join(l.Root, "raw")
	if entries, err := os.ReadDir(rawDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			src := filepath.Join(rawDir, e.Name())
			dst := filepath.Join(l.RawInboxDir(), e.Name())
			if err := moveIfAbsent(src, dst); err != nil {
				return out, err
			}
			out.MovedInbox++
		}
	}

	// 2) 旧版归档 raw/archived/* → raw/archive/legacy/
	legacyArchive := filepath.Join(rawDir, "archived")
	if entries, err := os.ReadDir(legacyArchive); err == nil {
		dir := filepath.Join(l.Root, "raw", "archive", "legacy")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return out, fmt.Errorf("创建归档目录失败：%v", err)
		}
		for _, e := range entries {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if err := moveIfAbsent(filepath.Join(legacyArchive, e.Name()), filepath.Join(dir, e.Name())); err != nil {
				return out, err
			}
			out.MovedArchive++
		}
	}

	// 3) 归档目录逐文件补登记（未登记者：按批次补齐类型/行数，否则按文件名识别）。
	archiveRoot := filepath.Join(l.Root, "raw", "archive")
	walkErr := filepath.WalkDir(archiveRoot, func(p string, e os.DirEntry, err error) error {
		if err != nil || e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			return nil
		}
		rel, rerr := l.Rel(p)
		if rerr != nil {
			return nil
		}
		var exists int
		if err := s.DB.Pool().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM raw_files WHERE rel_path=?`, rel).Scan(&exists); err != nil {
			return fmt.Errorf("读取原始文件清单失败：%v", err)
		}
		if exists > 0 {
			return nil
		}
		name := e.Name()
		sourceType := ""
		rowCount := 0
		importedAt := ""
		// 优先按原文件名匹配历史批次（迁移后文件名带日期/指纹前后缀，故用 LIKE 兜底）。
		_ = s.DB.Pool().QueryRowContext(ctx, `SELECT source_type, row_count, imported_at
			FROM import_batches WHERE file_name=? OR ? LIKE '%' || file_name
			ORDER BY imported_at DESC LIMIT 1`, name, name).
			Scan(&sourceType, &rowCount, &importedAt)
		if sourceType == "" {
			if st, derr := DetectSourceType(name); derr == nil {
				sourceType = st
			} else {
				sourceType = "unknown"
			}
		}
		if _, created, err := s.DB.RegisterRawFileInPlace(ctx, rel, name, sourceType, rowCount, importedAt); err != nil {
			return err
		} else if created {
			out.Registered++
		}
		return nil
	})
	if walkErr != nil {
		return out, walkErr
	}

	// 4) 回填 import_batches.raw_sha256（按原文件名 + 指纹清单匹配，幂等）。
	res, err := s.DB.Pool().ExecContext(ctx, `UPDATE import_batches
		SET raw_sha256 = COALESCE((
			SELECT r.sha256 FROM raw_files r
			WHERE r.orig_name = import_batches.file_name
			ORDER BY r.ingested_at DESC LIMIT 1), '')
		WHERE raw_sha256 = ''`)
	if err != nil {
		return out, fmt.Errorf("回填批次原始文件失败：%v", err)
	}
	n, _ := res.RowsAffected()
	out.LinkedBatches = int(n)

	var missing int
	_ = s.DB.Pool().QueryRowContext(ctx, `SELECT COUNT(*) FROM import_batches WHERE raw_sha256=''`).Scan(&missing)
	out.Message = fmt.Sprintf("迁移完成：投放区收拢 %d 个文件，旧归档收拢 %d 个文件，新登记 %d 个原始文件，回填批次 %d 个；仍有 %d 个历史批次缺原始文件",
		out.MovedInbox, out.MovedArchive, out.Registered, out.LinkedBatches, missing)
	return out, nil
}

// moveIfAbsent 目标已存在则保留两份（不覆盖），否则移动；跨盘时回退为复制+删除。
func moveIfAbsent(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建目标目录失败：%v", err)
	}
	if _, err := os.Stat(dst); err == nil {
		stem := strings.TrimSuffix(filepath.Base(dst), filepath.Ext(dst))
		dst = fmt.Sprintf("%s_%s%s", stem, time.Now().Format("20060102_150405"), filepath.Ext(dst))
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("迁移文件失败：%s：%v", filepath.Base(src), err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("迁移文件失败：%s：%v", filepath.Base(src), err)
	}
	_ = os.Remove(src)
	return nil
}
