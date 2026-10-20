package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rawBankTSV = "交易时间\t对方单位\t用途\t摘要\t附言\t转入金额\t转出金额\t余额\n" +
	"2026-07-10 02:45:36\tPOS\t\t1001****7575\t\t11,970.00\t\t709,537.49\n" +
	"2026-07-10 13:39:14\tB公司\t货款\t货款\t\t\t4,800.00\t704,737.49\n"

func writeRawFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
		t.Fatalf("写入测试文件失败：%v", err)
	}
}

// TestRawDirScanImportArchive 默认路径：扫描 → 导入 → 归档 → 再扫描（已归档文件消失）。
func TestRawDirScanImportArchive(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewImportService(db)

	dir := t.TempDir()
	got, err := svc.SetRawDir(ctx, dir)
	if err != nil {
		t.Fatalf("保存默认路径失败：%v", err)
	}
	if got != dir {
		t.Errorf("规范路径 = %q, want %q", got, dir)
	}
	if cur, err := svc.GetRawDir(ctx); err != nil || cur != dir {
		t.Fatalf("读取默认路径 = %q, %v", cur, err)
	}

	writeRawFile(t, dir, "银行流水_20260710.tsv", rawBankTSV)
	scan, err := svc.ScanRawDir(ctx)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	if !scan.DirExists || len(scan.Files) != 1 {
		t.Fatalf("扫描结果 = %+v, want 1 个文件且目录存在", scan)
	}
	f := scan.Files[0]
	if f.State != "new" || f.SourceType != "bank_flow" || len(f.MissingColumns) != 0 {
		t.Fatalf("文件状态 = %+v, want new/bank_flow/无缺列", f)
	}
	if f.Size != int64(len(rawBankTSV)) {
		t.Errorf("文件大小 = %d, want %d", f.Size, len(rawBankTSV))
	}

	preview, err := svc.PreviewRawFile(ctx, f.Name)
	if err != nil {
		t.Fatalf("预览失败：%v", err)
	}
	if preview.TotalRows != 2 || len(preview.Missing) != 0 {
		t.Errorf("预览 = %+v, want 2 行且无缺列", preview)
	}

	// 不归档导入：文件保留，再扫描应为 done 并带导入时间。
	results, err := svc.ImportRawFiles(ctx, ImportRawReq{Names: []string{f.Name}})
	if err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	if len(results) != 1 {
		t.Fatalf("结果数 = %d, want 1", len(results))
	}
	if results[0].Status != "imported" || results[0].Result.SuccessCount != 2 {
		t.Fatalf("导入结果 = %+v, want imported/2 行", results[0])
	}
	if results[0].Error != "" || results[0].ArchivedTo != "" {
		t.Fatalf("未开启归档却返回归档信息：%+v", results[0])
	}
	if _, err := os.Stat(filepath.Join(dir, f.Name)); err != nil {
		t.Errorf("未归档时原文件应保留：%v", err)
	}
	scan, err = svc.ScanRawDir(ctx)
	if err != nil {
		t.Fatalf("再扫描失败：%v", err)
	}
	if len(scan.Files) != 1 || scan.Files[0].State != "done" || scan.Files[0].ImportedAt == "" {
		t.Fatalf("已导入后扫描 = %+v, want done + importedAt", scan.Files)
	}

	// 归档导入：同名文件第二次导入全部去重跳过，随后移动到 archived/。
	results, err = svc.ImportRawFiles(ctx, ImportRawReq{Names: []string{f.Name}, Archive: true})
	if err != nil {
		t.Fatalf("归档导入失败：%v", err)
	}
	if results[0].Status != "imported" || results[0].Result.SkipCount != 2 {
		t.Fatalf("重复导入结果 = %+v, want imported/2 行去重跳过", results[0])
	}
	if results[0].ArchivedTo == "" {
		t.Fatalf("归档信息为空：%+v", results[0])
	}
	if _, err := os.Stat(filepath.Join(dir, f.Name)); !os.IsNotExist(err) {
		t.Error("归档后原文件仍存在，want 已移动")
	}
	if _, err := os.Stat(results[0].ArchivedTo); err != nil {
		t.Errorf("归档文件不存在：%v", err)
	}

	scan, err = svc.ScanRawDir(ctx)
	if err != nil {
		t.Fatalf("再扫描失败：%v", err)
	}
	if len(scan.Files) != 0 {
		t.Errorf("归档后再扫描文件数 = %d, want 0", len(scan.Files))
	}
}

// TestRawDirScanStates 缺列仍可扫描（标缺列），不导入成功；不支持类型标 bad。
func TestRawDirScanStates(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewImportService(db)
	dir := t.TempDir()
	if _, err := svc.SetRawDir(ctx, dir); err != nil {
		t.Fatal(err)
	}

	// 缺「余额」列：状态 new（可预览），但导入必须失败并给出中文原因。
	missingBal := "交易时间\t对方单位\t用途\t摘要\t附言\t转入金额\t转出金额\n2026-07-10 09:00:00\tA公司\t货款\t货款\t\t100.00\t\n"
	writeRawFile(t, dir, "银行流水_20260706.tsv", missingBal)
	writeRawFile(t, dir, "说明.txt", "非表格文件")
	writeRawFile(t, dir, "~$银行流水.xlsx", "excel 临时文件")

	scan, err := svc.ScanRawDir(ctx)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	if len(scan.Files) != 2 {
		t.Fatalf("扫描文件数 = %d, want 2（跳过隐藏/临时文件）", len(scan.Files))
	}
	var bank, txt *RawFile
	for i := range scan.Files {
		switch scan.Files[i].Name {
		case "银行流水_20260706.tsv":
			bank = &scan.Files[i]
		case "说明.txt":
			txt = &scan.Files[i]
		}
	}
	if bank == nil || bank.State != "new" || len(bank.MissingColumns) != 1 || bank.MissingColumns[0] != "余额" {
		t.Fatalf("缺列文件 = %+v, want new + 缺「余额」", bank)
	}
	if txt == nil || txt.State != "bad" || txt.Error == "" {
		t.Fatalf("不支持文件 = %+v, want bad + 原因", txt)
	}

	results, err := svc.ImportRawFiles(ctx, ImportRawReq{Names: []string{bank.Name}})
	if err != nil {
		t.Fatalf("导入调用失败：%v", err)
	}
	if results[0].Status != "failed" || !strings.Contains(results[0].Error, "表头不匹配") {
		t.Fatalf("缺列导入结果 = %+v, want failed + 表头不匹配", results[0])
	}

	// 路径穿越必须拒绝
	results, err = svc.ImportRawFiles(ctx, ImportRawReq{Names: []string{"../evil.tsv"}})
	if err != nil {
		t.Fatalf("导入调用失败：%v", err)
	}
	if results[0].Status != "failed" || !strings.Contains(results[0].Error, "文件名非法") {
		t.Fatalf("路径穿越结果 = %+v, want failed + 文件名非法", results[0])
	}
}

// TestRawDirDefaultAndValidation 未设置时回退到库同目录 /raw；目录不存在不报错；非法目录拒绝保存。
func TestRawDirDefaultAndValidation(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewImportService(db)

	want := filepath.Join(filepath.Dir(db.Path()), "raw")
	got, err := svc.GetRawDir(ctx)
	if err != nil || got != want {
		t.Fatalf("默认路径 = %q, %v, want %q", got, err, want)
	}
	scan, err := svc.ScanRawDir(ctx)
	if err != nil {
		t.Fatalf("目录不存在的扫描应只提示，却报错：%v", err)
	}
	if scan.DirExists || len(scan.Files) != 0 {
		t.Errorf("扫描结果 = %+v, want DirExists=false 且无文件", scan)
	}
	if _, err := svc.SetRawDir(ctx, filepath.Join(t.TempDir(), "无此目录")); err == nil {
		t.Error("不存在的目录被接受，want 拒绝")
	}
}
