package services

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"finlite/internal/store"
)

// bankTSVForEvidence 一份结构完整、余额连续可校验的银行流水。
const bankTSVForEvidence = "交易时间\t对方单位\t用途\t摘要\t附言\t转入金额\t转出金额\t余额\n" +
	"2026-07-10 02:45:36\tPOS\t\t1001****7575\t\t11,970.00\t\t709,537.49\n" +
	"2026-07-11 09:00:00\tA公司\t货款\t货款\t\t\t1,000.00\t708,537.49\n"

// countRawFiles 当前账套已登记的原始文件数。
func countRawFiles(t *testing.T, db *store.DB) int {
	t.Helper()
	var n int
	if err := db.Pool().QueryRow(`SELECT COUNT(*) FROM raw_files`).Scan(&n); err != nil {
		t.Fatalf("读取原始文件清单失败：%v", err)
	}
	return n
}

// TestImportFileArchivesEvidence 选文件导入：先归档原始文件再解析，批次带指纹，同内容不重复落盘。
func TestImportFileArchivesEvidence(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewImportService(db)

	res, err := svc.ImportFile(ctx, "银行流水_20260710.tsv", bankTSVForEvidence)
	if err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	if res.RawSHA256 == "" {
		t.Fatal("导入结果没有原始文件指纹")
	}
	if res.SuccessCount != 2 {
		t.Fatalf("成功行数 = %d，want 2", res.SuccessCount)
	}

	// 清单有 1 条，且归档文件存在、内容一致。
	files, err := db.ListRawFiles(ctx)
	if err != nil {
		t.Fatalf("读取清单失败：%v", err)
	}
	if len(files) != 1 {
		t.Fatalf("原始文件条数 = %d，want 1", len(files))
	}
	if files[0].SHA256 != res.RawSHA256 {
		t.Fatalf("批次指纹 %s 与清单 %s 不一致", res.RawSHA256, files[0].SHA256)
	}
	if files[0].RowCount != 2 {
		t.Errorf("清单行数 = %d，want 2", files[0].RowCount)
	}
	abs, err := db.Ledger().Abs(files[0].RelPath)
	if err != nil {
		t.Fatalf("归档路径非法：%v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("归档文件不存在：%v", err)
	}

	// 同内容换个文件名再导：清单仍 1 条（内容级去重），数据行全部跳过。
	res2, err := svc.ImportFile(ctx, "银行流水_20260710_副本.tsv", bankTSVForEvidence)
	if err != nil {
		t.Fatalf("二次导入失败：%v", err)
	}
	if res2.RawSHA256 != res.RawSHA256 {
		t.Errorf("二次导入指纹 = %s，want 复用 %s", res2.RawSHA256, res.RawSHA256)
	}
	if res2.SkipCount != 2 {
		t.Errorf("二次导入跳过行 = %d，want 2（去重）", res2.SkipCount)
	}
	if n := countRawFiles(t, db); n != 1 {
		t.Errorf("二次导入后清单条数 = %d，want 1", n)
	}
}

// TestImportFileKeepsEvidenceOnParseFailure 解析失败也必须留下原始文件：先归档、后解析。
func TestImportFileKeepsEvidenceOnParseFailure(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewImportService(db)

	// 缺「余额」列 → 表头不匹配，导入失败。
	bad := "交易时间\t对方单位\t转入金额\t转出金额\n2026-07-10 09:00:00\tA公司\t100.00\t\n"
	if _, err := svc.ImportFile(ctx, "银行流水_缺列.tsv", bad); err == nil {
		t.Fatal("缺列文件导入成功，want 失败")
	}
	files, err := db.ListRawFiles(ctx)
	if err != nil {
		t.Fatalf("读取清单失败：%v", err)
	}
	if len(files) != 1 {
		t.Fatalf("解析失败后原始文件条数 = %d，want 1（必须留证）", len(files))
	}
	abs, _ := db.Ledger().Abs(files[0].RelPath)
	if got, err := os.ReadFile(abs); err != nil || string(got) != bad {
		t.Fatalf("留证文件内容不符：%v", err)
	}
}

// TestScanRawDirDoneByFingerprint 「已导入」按内容指纹判定：改名/拷贝都算已导入。
func TestScanRawDirDoneByFingerprint(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewImportService(db)

	if _, err := svc.ImportFile(ctx, "银行流水.tsv", bankTSVForEvidence); err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	inbox := db.Ledger().RawInboxDir()
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRawFile(t, inbox, "银行流水_副本.tsv", bankTSVForEvidence)

	scan, err := svc.ScanRawDir(ctx)
	if err != nil {
		t.Fatalf("扫描失败：%v", err)
	}
	if len(scan.Files) != 1 || scan.Files[0].State != "done" {
		t.Fatalf("扫描结果 = %+v，want 1 个文件且 state=done（按指纹判定）", scan.Files)
	}
}

// TestMigrateLegacyEvidence 存量迁移：旧 raw/ 与 raw/archived/ 收进账套结构，补登记并回填批次。
func TestMigrateLegacyEvidence(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewRawFileService(db)
	root := db.Ledger().Root

	// 旧版布局：raw/ 下直接放待导入文件，raw/archived/ 存旧归档。
	legacyRaw := filepath.Join(root, "raw")
	legacyArchived := filepath.Join(legacyRaw, "archived")
	if err := os.MkdirAll(legacyArchived, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRawFile(t, legacyRaw, "银行流水_20260710.tsv", bankTSVForEvidence)
	writeRawFile(t, legacyArchived, "订单流水_20260711.tsv", "工行订单号\t结算日期\t订单金额\t商户实收金额\nA1\t2026-07-11\t100.00\t99.75\n")

	// 历史批次：有文件名、无指纹。
	if _, err := db.Pool().Exec(`INSERT INTO import_batches
		(id, source_type, file_name, row_count, success_count, skip_count, balance_warning, imported_at, raw_sha256)
		VALUES
		('b-legacy', 'order_flow', '订单流水_20260711.tsv', 1, 1, 0, '', '2026-07-11T10:00:00+08:00', ''),
		('b-missing', 'bank_flow', '银行流水_文件已丢失.tsv', 3, 3, 0, '', '2026-07-12T10:00:00+08:00', '')`); err != nil {
		t.Fatal(err)
	}

	report, err := svc.MigrateLegacy(ctx)
	if err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	if report.MovedInbox != 1 || report.MovedArchive != 1 {
		t.Fatalf("迁移统计 = %+v，want 投放区 1 个 + 旧归档 1 个", report)
	}
	if _, err := os.Stat(filepath.Join(root, "raw", "inbox", "银行流水_20260710.tsv")); err != nil {
		t.Errorf("投放区文件未迁移：%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "raw", "archive", "legacy", "订单流水_20260711.tsv")); err != nil {
		t.Errorf("旧归档文件未迁移：%v", err)
	}

	// 旧归档里的文件补登记（投放区文件待导入时才登记），历史批次回填指纹。
	if n := countRawFiles(t, db); n != 1 {
		t.Fatalf("补登记条数 = %d，want 1（仅归档文件）", n)
	}
	var linked string
	if err := db.Pool().QueryRow(`SELECT raw_sha256 FROM import_batches WHERE id='b-legacy'`).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if linked == "" {
		t.Fatal("历史批次未回填指纹")
	}

	// 幂等：再跑一次不重复登记、不重复移动。
	again, err := svc.MigrateLegacy(ctx)
	if err != nil {
		t.Fatalf("二次迁移失败：%v", err)
	}
	if again.MovedInbox != 0 || again.MovedArchive != 0 || again.Registered != 0 {
		t.Fatalf("二次迁移有副作用：%+v", again)
	}
	if n := countRawFiles(t, db); n != 1 {
		t.Fatalf("二次迁移后条数 = %d，want 1", n)
	}
	if !strings.Contains(again.Message, "缺原始文件") {
		t.Errorf("迁移提示缺少「缺原始文件」口径：%q", again.Message)
	}
	var missing string
	if err := db.Pool().QueryRow(`SELECT raw_sha256 FROM import_batches WHERE id='b-missing'`).Scan(&missing); err != nil {
		t.Fatal(err)
	}
	if missing != "" {
		t.Errorf("无对应文件的历史批次被强行补了指纹：%q", missing)
	}
}
