package services

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 默认路径导入：扫描本地原始数据目录，批量导入 TSV/CSV/XLSX（导入中心「默认路径导入」）。
// 业务依据：docs/uidesign/import-center.html + 业务文档 F1（同一套解析/去重/余额校验，仅入口不同）。
// 原始表仍只读：文件导入后可选归档到 archived/，不提供编辑/删除。

const (
	rawDirSettingKey = "raw_dir"
	rawArchiveDir    = "archived"
)

// GetRawDir 返回默认原始数据目录：优先读本地设置，未设置则回退到「库文件同目录 /raw」。
func (s *ImportService) GetRawDir(ctx context.Context) (string, error) {
	var dir string
	err := s.DB.Pool().QueryRowContext(ctx,
		`SELECT value FROM app_settings WHERE key=?`, rawDirSettingKey).Scan(&dir)
	switch {
	case err == sql.ErrNoRows || (err == nil && strings.TrimSpace(dir) == ""):
		return s.defaultRawDir(), nil
	case err != nil:
		return "", fmt.Errorf("读取默认路径失败：%v", err)
	default:
		return dir, nil
	}
}

// SetRawDir 保存默认原始数据目录（目录须已存在），返回规范化后的绝对路径。
func (s *ImportService) SetRawDir(ctx context.Context, dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("未选择目录，请重新选择原始数据目录")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("目录路径无效：%s", dir)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("目录不存在：%s", abs)
	}
	if !st.IsDir() {
		return "", fmt.Errorf("选择的不是文件夹：%s", abs)
	}
	now := time.Now().Format(time.RFC3339)
	if err := s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
			rawDirSettingKey, abs, now)
		return err
	}); err != nil {
		return "", fmt.Errorf("保存默认路径失败：%v", err)
	}
	return abs, nil
}

// ScanRawDir 扫描默认路径：识别类型、缺列、是否已导入（同名批次且导入时间不早于文件修改时间）。
// 目录不存在时返回 DirExists=false 的空结果，不报错（业务要求：仅提示）。
func (s *ImportService) ScanRawDir(ctx context.Context) (RawScan, error) {
	dir, err := s.GetRawDir(ctx)
	if err != nil {
		return RawScan{}, err
	}
	scan := RawScan{Dir: dir, ScannedAt: time.Now().Format(time.RFC3339)}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return scan, nil
	}
	scan.DirExists = true
	entries, err := os.ReadDir(dir)
	if err != nil {
		return RawScan{}, fmt.Errorf("读取默认路径失败：%v", err)
	}
	files := make([]RawFile, 0, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() || isTempRawFileName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		name := entry.Name()
		f := RawFile{Name: name, Size: info.Size(), ModTime: info.ModTime().Format(time.RFC3339)}
		if !isSupportedRawFileName(name) {
			// 设计稿会把非 TSV/CSV/XLSX 的文件列出来并标「不支持」，便于用户清理目录。
			f.State = "bad"
			f.Error = "不支持的文件类型（仅受理 TSV / CSV / XLSX）"
			files = append(files, f)
			continue
		}
		sourceType, derr := DetectSourceType(name)
		if derr != nil {
			f.State = "bad"
			f.Error = derr.Error()
			files = append(files, f)
			continue
		}
		f.SourceType = sourceType
		content, rerr := os.ReadFile(filepath.Join(dir, name))
		if rerr != nil {
			f.State = "bad"
			f.Error = fmt.Sprintf("打不开文件：%v", rerr)
			files = append(files, f)
			continue
		}
		header, _, perr := ParseAnyFile(name, string(content))
		if perr != nil {
			f.State = "bad"
			f.Error = perr.Error()
			files = append(files, f)
			continue
		}
		f.MissingColumns = missingColumnsFor(sourceType, header)
		if importedAt, done := s.rawFileImported(ctx, name, info.ModTime()); done {
			f.State = "done"
			f.ImportedAt = importedAt
		} else {
			f.State = "new"
		}
		files = append(files, f)
	}
	// 待导入优先，其次按修改时间倒序；已导入/不支持沉底。
	sort.SliceStable(files, func(i, j int) bool {
		if (files[i].State == "new") != (files[j].State == "new") {
			return files[i].State == "new"
		}
		return files[i].ModTime > files[j].ModTime
	})
	scan.Files = files
	return scan, nil
}

// PreviewRawFile 预览默认路径内的单个文件（列映射弹窗用），避免前端读取任意路径。
func (s *ImportService) PreviewRawFile(ctx context.Context, name string) (ColumnMapping, error) {
	full, err := s.resolveRawPath(ctx, name)
	if err != nil {
		return ColumnMapping{}, err
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return ColumnMapping{}, fmt.Errorf("打不开文件：%v", err)
	}
	return s.PreviewColumns(ctx, name, string(data))
}

// ImportRawFiles 导入默认路径中选中的文件，逐个返回结果（单文件失败不阻断其他文件）。
// Archive=true 时导入成功后把文件移动到 archived/，避免下次扫描重复导入。
func (s *ImportService) ImportRawFiles(ctx context.Context, req ImportRawReq) ([]ImportRawResult, error) {
	if len(req.Names) == 0 {
		return nil, fmt.Errorf("未选择要导入的文件")
	}
	out := make([]ImportRawResult, 0, len(req.Names))
	for _, name := range req.Names {
		item := ImportRawResult{FileName: name}
		full, err := s.resolveRawPath(ctx, name)
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			out = append(out, item)
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			item.Status = "failed"
			item.Error = fmt.Sprintf("打不开文件：%v", err)
			out = append(out, item)
			continue
		}
		res, err := s.ImportFile(ctx, name, string(data))
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			out = append(out, item)
			continue
		}
		item.Status = "imported"
		item.Result = res
		if req.Archive {
			archived, aerr := archiveRawFile(full, name)
			if aerr != nil {
				// 导入已成功，归档失败只提示，不改变导入结果。
				item.Error = fmt.Sprintf("已导入，但归档失败：%v", aerr)
			} else {
				item.ArchivedTo = archived
			}
		}
		out = append(out, item)
	}
	return out, nil
}

// defaultRawDir 默认目录：库文件同目录 /raw（单机单账套，随库目录走，便于整体迁移）。
func (s *ImportService) defaultRawDir() string {
	return filepath.Join(filepath.Dir(s.DB.Path()), "raw")
}

// resolveRawPath 把「文件名」解析成默认目录内的绝对路径；拒绝子目录与路径分隔符，防路径穿越。
func (s *ImportService) resolveRawPath(ctx context.Context, name string) (string, error) {
	if name == "" || name != filepath.Base(name) || strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("文件名非法：%s", name)
	}
	dir, err := s.GetRawDir(ctx)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// rawFileImported 同名批次已导入且导入时间不早于文件修改时间 → 视为已导入（file done）。
// 返回该批次的导入时间（RFC3339），供扫描列表展示「已导入时间」。
func (s *ImportService) rawFileImported(ctx context.Context, name string, modTime time.Time) (string, bool) {
	var importedAt string
	if err := s.DB.Pool().QueryRowContext(ctx,
		`SELECT COALESCE(MAX(imported_at),'') FROM import_batches WHERE file_name=?`, name).
		Scan(&importedAt); err != nil || importedAt == "" {
		return "", false
	}
	t, err := time.Parse(time.RFC3339, importedAt)
	// imported_at 只到秒，mtime 带纳秒：截断到秒再比，避免同一秒内误判「未导入」。
	if err != nil || modTime.Truncate(time.Second).After(t) {
		return "", false
	}
	return importedAt, true
}

// isTempRawFileName 扫描时跳过隐藏文件与 Excel 临时文件（~$ 开头）。
func isTempRawFileName(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "~$")
}

// isSupportedRawFileName 扫描时只受理 TSV/CSV/XLSX（其余列出来标「不支持」）。
func isSupportedRawFileName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".tsv", ".csv", ".xlsx":
		return true
	default:
		return false
	}
}

// archiveRawFile 把已导入文件移到 <raw>/archived/；重名时追加时间戳，避免覆盖。
func archiveRawFile(fullPath, name string) (string, error) {
	dir := filepath.Join(filepath.Dir(fullPath), rawArchiveDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建归档目录失败：%v", err)
	}
	target := filepath.Join(dir, name)
	if _, err := os.Stat(target); err == nil {
		ext := filepath.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		target = filepath.Join(dir, fmt.Sprintf("%s_%s%s", stem, time.Now().Format("20060102_150405"), ext))
	}
	if err := os.Rename(fullPath, target); err != nil {
		return "", fmt.Errorf("移动文件失败：%v", err)
	}
	return target, nil
}

// columnSpec 各导入类型的关键列（预览与扫描共用）。
func columnSpec(sourceType string) []string {
	if sourceType == "order_flow" {
		return orderColumnSpec
	}
	return bankColumnSpec
}

// missingColumnsFor 计算缺失的关键列（与 PreviewColumns 同一口径）。
func missingColumnsFor(sourceType string, header []string) []string {
	var missing []string
	for _, name := range columnSpec(sourceType) {
		if colIdx(header, strings.Split(name, "/")...) < 0 {
			missing = append(missing, name)
		}
	}
	return missing
}
