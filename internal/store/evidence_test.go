package store

import (
	"archive/zip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleTSV = "交易时间\t转入金额\t转出金额\t余额\n2026-07-10 02:45:36\t11970.00\t\t709537.49\n"

// newEvidenceDB 建库并返回（账套根目录 = 库文件所在目录，便于断言归档路径）。
func newEvidenceDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "finlite.db"))
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestRegisterRawFileArchivesAndDedupes 原始文件：归档到账套内、相对路径入库、同内容只存一份。
func TestRegisterRawFileArchivesAndDedupes(t *testing.T) {
	ctx := context.Background()
	db := newEvidenceDB(t)
	l := db.Ledger()

	first, created, err := db.RegisterRawFile(ctx, RegisterRawReq{
		OrigName: "银行流水_20260710.tsv", SourceType: "bank_flow", Data: []byte(sampleTSV), RowCount: 1,
	})
	if err != nil {
		t.Fatalf("登记原始文件失败：%v", err)
	}
	if !created {
		t.Fatal("首次登记 created=false，want true")
	}
	if len(first.SHA256) != 64 {
		t.Fatalf("sha256 长度 = %d，want 64", len(first.SHA256))
	}
	if strings.HasPrefix(first.RelPath, "/") || strings.Contains(first.RelPath, l.Root) {
		t.Fatalf("rel_path 必须是相对账套目录的路径：%q", first.RelPath)
	}
	if !strings.HasPrefix(first.RelPath, "raw/archive/") {
		t.Fatalf("rel_path 前缀 = %q，want raw/archive/", first.RelPath)
	}
	abs, err := l.Abs(first.RelPath)
	if err != nil {
		t.Fatalf("rel_path 无法还原：%v", err)
	}
	if got, err := os.ReadFile(abs); err != nil || string(got) != sampleTSV {
		t.Fatalf("归档文件内容不符：%v", err)
	}

	// 同内容再导入（改了文件名也一样）：命中清单，不再落盘。
	again, created, err := db.RegisterRawFile(ctx, RegisterRawReq{
		OrigName: "另一个名字.tsv", SourceType: "bank_flow", Data: []byte(sampleTSV),
	})
	if err != nil {
		t.Fatalf("二次登记失败：%v", err)
	}
	if created {
		t.Fatal("同内容二次登记 created=true，want false（应去重）")
	}
	if again.SHA256 != first.SHA256 || again.RelPath != first.RelPath {
		t.Fatalf("二次登记返回 %+v，want 复用 %+v", again, first)
	}
	files, err := db.ListRawFiles(ctx)
	if err != nil {
		t.Fatalf("读取清单失败：%v", err)
	}
	if len(files) != 1 {
		t.Fatalf("清单条数 = %d，want 1", len(files))
	}
}

// TestVerifyRawFilesDetectsTamper 完整性校验：改动/删除/未登记都要报出来。
func TestVerifyRawFilesDetectsTamper(t *testing.T) {
	ctx := context.Background()
	db := newEvidenceDB(t)
	l := db.Ledger()

	okFile, _, err := db.RegisterRawFile(ctx, RegisterRawReq{OrigName: "a.tsv", SourceType: "bank_flow", Data: []byte(sampleTSV)})
	if err != nil {
		t.Fatal(err)
	}
	badFile, _, err := db.RegisterRawFile(ctx, RegisterRawReq{OrigName: "b.tsv", SourceType: "bank_flow", Data: []byte(sampleTSV + "2026-07-11 09:00:00\t1.00\t\t709538.49\n")})
	if err != nil {
		t.Fatal(err)
	}
	goneFile, _, err := db.RegisterRawFile(ctx, RegisterRawReq{OrigName: "c.tsv", SourceType: "order_flow", Data: []byte("工行订单号\t结算日期\nA1\t2026-07-11\n")})
	if err != nil {
		t.Fatal(err)
	}

	// 篡改 b、删除 c、往归档目录塞一个未登记文件。
	badAbs, _ := l.Abs(badFile.RelPath)
	if err := os.WriteFile(badAbs, []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	goneAbs, _ := l.Abs(goneFile.RelPath)
	if err := os.Remove(goneAbs); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(l.Root, "raw", "archive", "manual-copy.tsv")
	if err := os.WriteFile(stray, []byte("manual"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := db.VerifyRawFiles(ctx)
	if err != nil {
		t.Fatalf("校验失败：%v", err)
	}
	if report.Checked != 3 || report.OK != 1 || report.Modified != 1 || report.Missing != 1 {
		t.Fatalf("统计 = checked %d / ok %d / modified %d / missing %d，want 3/1/1/1",
			report.Checked, report.OK, report.Modified, report.Missing)
	}
	if report.Unregistered != 1 {
		t.Fatalf("未登记文件 = %d，want 1", report.Unregistered)
	}
	kinds := map[string]bool{}
	for _, it := range report.Issues {
		kinds[it.Kind] = true
	}
	for _, want := range []string{"modified", "missing", "unregistered"} {
		if !kinds[want] {
			t.Errorf("校验结果缺少 %s：%+v", want, report.Issues)
		}
	}
	_ = okFile
}

// TestExportBundleSnapshotAndManifest 交接包：库快照 + 原始文件 + manifest.tsv，指纹可复算。
func TestExportBundleSnapshotAndManifest(t *testing.T) {
	ctx := context.Background()
	db := newEvidenceDB(t)
	raw, _, err := db.RegisterRawFile(ctx, RegisterRawReq{OrigName: "银行流水.tsv", SourceType: "bank_flow", Data: []byte(sampleTSV)})
	if err != nil {
		t.Fatal(err)
	}

	res, err := db.ExportBundle(ctx)
	if err != nil {
		t.Fatalf("导出交接包失败：%v", err)
	}
	if res.RawFiles != 1 {
		t.Fatalf("原始文件数 = %d，want 1", res.RawFiles)
	}
	if res.SHA256 == "" || res.SizeBytes == 0 {
		t.Fatalf("交接包结果不完整：%+v", res)
	}

	zr, err := zip.OpenReader(res.Path)
	if err != nil {
		t.Fatalf("打开交接包失败：%v", err)
	}
	defer zr.Close()
	names := map[string]bool{}
	var manifest string
	for _, f := range zr.File {
		names[f.Name] = true
		if strings.HasSuffix(f.Name, "manifest.tsv") {
			rc, rerr := f.Open()
			if rerr != nil {
				t.Fatal(rerr)
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			manifest = string(data)
		}
	}
	code := db.Ledger().Code
	for _, want := range []string{
		code + "/finlite.db",
		code + "/" + raw.RelPath,
		code + "/manifest.tsv",
	} {
		if !names[want] {
			t.Errorf("交接包缺少 %s（实际：%v）", want, names)
		}
	}
	if !strings.Contains(manifest, raw.SHA256) {
		t.Errorf("manifest 未包含原始文件指纹：%q", manifest)
	}
}

// TestOpenLedgerIdentityGuard 账套身份：目录名 / ledger.json / 库内三方互校，禁止串档。
func TestOpenLedgerIdentityGuard(t *testing.T) {
	dataRoot := t.TempDir()
	l1, err := ResolveLedger(dataRoot, "ledger-1001")
	if err != nil {
		t.Fatal(err)
	}
	db, err := OpenLedger(l1)
	if err != nil {
		t.Fatalf("打开账套失败：%v", err)
	}
	var code string
	if err := db.Pool().QueryRow(`SELECT code FROM ledger_profile WHERE id=1`).Scan(&code); err != nil {
		t.Fatalf("读取账套身份失败：%v", err)
	}
	if code != "ledger-1001" {
		t.Fatalf("账套码 = %q，want ledger-1001", code)
	}
	for _, p := range []string{l1.DBPath(), l1.IdentityPath(), l1.RawInboxDir(), l1.BackupDir()} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("账套目录缺失 %s：%v", p, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// 把 A 账套的库拷到 B 账套目录：必须拒绝打开。
	l2, err := ResolveLedger(dataRoot, "ledger-1002")
	if err != nil {
		t.Fatal(err)
	}
	if err := l2.Ensure(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(l1.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l2.DBPath(), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLedger(l2); err == nil {
		t.Fatal("跨账套拷贝的库被打开，want 拒绝")
	} else if !strings.Contains(err.Error(), "账套目录不匹配") {
		t.Fatalf("错误文案不可操作：%v", err)
	}

	// 非法账套码拒绝。
	if _, err := ResolveLedger(dataRoot, "../etc"); err == nil {
		t.Fatal("非法账套码被接受，want 拒绝")
	}
}

// TestLedgerPathsStayInsideRoot 相对路径越界必须拒绝（防手改库读到账套外文件）。
func TestLedgerPathsStayInsideRoot(t *testing.T) {
	l := Ledger{Code: "ledger-1001", Root: filepath.Join(t.TempDir(), "ledger-1001")}
	for _, bad := range []string{"", "..", "../x.tsv", "/etc/passwd", "raw/../../x"} {
		if _, err := l.Abs(bad); err == nil {
			t.Errorf("Abs(%q) 未拒绝越界路径", bad)
		}
	}
	good, err := l.Abs("raw/archive/2026/07/a.tsv")
	if err != nil {
		t.Fatalf("合法相对路径被拒绝：%v", err)
	}
	if rel, err := l.Rel(good); err != nil || rel != "raw/archive/2026/07/a.tsv" {
		t.Fatalf("Rel 往返不一致：%q, %v", rel, err)
	}
}

// TestAdoptLegacyLayout 旧版布局（exe 同目录 finlite.db / raw / backups）一次性收进账套目录。
func TestAdoptLegacyLayout(t *testing.T) {
	dataRoot := t.TempDir()
	// 旧版：库文件与 raw/、backups/ 都在 data root 下。
	if err := os.WriteFile(filepath.Join(dataRoot, "finlite.db"), []byte("old-db"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataRoot, "raw", "archived"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "raw", "archived", "银行流水.tsv"), []byte(sampleTSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataRoot, "backups"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataRoot, "backups", "finlite_20260101_000000.db"), []byte("old-backup"), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := ResolveLedger(dataRoot, DefaultLedgerCode)
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := l.AdoptLegacy(dataRoot)
	if err != nil {
		t.Fatalf("迁移失败：%v", err)
	}
	if len(adopted) != 3 {
		t.Fatalf("迁移项 = %v，want finlite.db + raw + backups", adopted)
	}
	for _, p := range []string{
		l.DBPath(),
		filepath.Join(l.Root, "raw", "archived", "银行流水.tsv"),
		filepath.Join(l.BackupDir(), "finlite_20260101_000000.db"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("账套内缺少 %s：%v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "finlite.db")); !os.IsNotExist(err) {
		t.Error("旧库文件未移走")
	}

	// 幂等：再跑一次不搬任何东西。
	again, err := l.AdoptLegacy(dataRoot)
	if err != nil || len(again) != 0 {
		t.Fatalf("二次迁移 = %v, %v，want 空", again, err)
	}
}

// TestPreMigrateAttachmentsExternalizesBlobs 旧库把附件存 BLOB：开库前置步骤写盘并回填指纹，
// v6 脚本重建表去掉 content 列，附件内容不丢。
func TestPreMigrateAttachmentsExternalizesBlobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "finlite.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("建库失败：%v", err)
	}
	// 退回 v5 形态：附件内容存 BLOB。
	for _, stmt := range []string{
		`DROP TABLE collection_attachments`,
		`CREATE TABLE collection_attachments (
			id TEXT NOT NULL PRIMARY KEY, collection_id TEXT NOT NULL, file_name TEXT NOT NULL,
			ext TEXT NOT NULL, size_bytes INTEGER NOT NULL DEFAULT 0, content BLOB,
			sort_no INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL)`,
		`CREATE INDEX idx_collection_attachments_collection ON collection_attachments (collection_id)`,
	} {
		if _, err := db.Pool().Exec(stmt); err != nil {
			t.Fatalf("回退失败（%s）：%v", stmt, err)
		}
	}
	blob := []byte("legacy-blob-bytes")
	if _, err := db.Pool().Exec(`INSERT INTO collection_attachments
		(id, collection_id, file_name, ext, size_bytes, content, sort_no, created_at)
		VALUES ('a1', 'SC260710001', '收款单据.png', 'png', ?, ?, 0, '2026-07-10T10:00:00+08:00')`,
		len(blob), blob); err != nil {
		t.Fatalf("写入旧版附件失败：%v", err)
	}
	if _, err := db.Pool().Exec(`PRAGMA user_version=5`); err != nil {
		t.Fatalf("回写 user_version 失败：%v", err)
	}
	_ = db.Close()

	upgraded, err := Open(path)
	if err != nil {
		t.Fatalf("升级失败：%v", err)
	}
	defer upgraded.Close()

	var sha, rel string
	if err := upgraded.Pool().QueryRow(`SELECT sha256, rel_path FROM collection_attachments WHERE id='a1'`).
		Scan(&sha, &rel); err != nil {
		t.Fatalf("读取回填结果失败：%v", err)
	}
	if len(sha) != 64 || !strings.HasPrefix(rel, "attachments/") {
		t.Fatalf("指纹/路径未回填：sha=%q rel=%q", sha, rel)
	}
	abs, err := upgraded.Ledger().Abs(rel)
	if err != nil {
		t.Fatalf("路径越界：%v", err)
	}
	if got, err := os.ReadFile(abs); err != nil || string(got) != string(blob) {
		t.Fatalf("外置文件内容不符：%v", err)
	}
	// content 列必须已被 v6 脚本移除。
	hasTable, hasContent, hasSHA, err := upgraded.attachmentColumns(context.Background())
	if err != nil || !hasTable || !hasSHA {
		t.Fatalf("表结构探测异常：table=%v content=%v sha=%v err=%v", hasTable, hasContent, hasSHA, err)
	}
	if hasContent {
		t.Error("content 列仍存在，BLOB 未移除")
	}
}
