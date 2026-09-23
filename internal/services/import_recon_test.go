package services

import (
	"context"
	"path/filepath"
	"testing"

	"finlite/internal/store"
)

func newTestDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "finlite.db"))
	if err != nil {
		t.Fatalf("打开测试库失败：%v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestBankBalanceCheckOrderIndependent 银行余额校验与导出顺序无关：
// 文件倒序、同一秒多行乱序时，仍应识别出真实连续的对账单（业务文档 §6.3）。
func TestBankBalanceCheckOrderIndependent(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	// 倒序 + 同秒乱序：真实顺序为 700000.00 → -2432.51 → +11970.00 → -4800.00
	content := "凭证号\t对方账号\t交易时间\t对方单位\t用途\t摘要\t附言\t转入金额\t转出金额\t余额\n" +
		"000000000\tx\t2026-07-10 13:39:14\tB公司\t货款\t货款\t\t\t4,800.00\t704,737.49\n" +
		"000000000\tx\t2026-07-10 02:45:36\tPOS\t\t1001****7575\t\t11,970.00\t\t709,537.49\n" +
		"000000000\tx\t2026-07-01 09:00:00\tA公司\t货款\t货款\t\t\t2,432.51\t697,567.49\n"

	res, err := NewImportService(db).ImportFile(ctx, "银行流水.tsv", content)
	if err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	if res.SuccessCount != 3 {
		t.Fatalf("成功行数 = %d, want 3", res.SuccessCount)
	}
	if res.BalanceWarning != "" {
		t.Errorf("整批余额警告 = %q, want 空（逐行连续）", res.BalanceWarning)
	}
	var mismatched int
	if err := db.Pool().QueryRow(`SELECT COUNT(*) FROM bank_flows WHERE balance_status<>'ok'`).Scan(&mismatched); err != nil {
		t.Fatal(err)
	}
	if mismatched != 0 {
		t.Errorf("mismatch 行数 = %d, want 0", mismatched)
	}
}

// TestBankBalanceCheckDetectsRealBreak 真实断点仍须标不平（不能因重排而漏报）。
func TestBankBalanceCheckDetectsRealBreak(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	// 第 2 行余额与递推不符
	content := "凭证号\t对方账号\t交易时间\t对方单位\t用途\t摘要\t附言\t转入金额\t转出金额\t余额\n" +
		"000000000\tx\t2026-07-01 09:00:00\tA公司\t货款\t货款\t\t\t2,432.51\t697,567.49\n" +
		"000000000\tx\t2026-07-02 02:45:36\tPOS\t\t1001****7575\t\t11,970.00\t\t709,000.00\n"

	res, err := NewImportService(db).ImportFile(ctx, "银行流水.tsv", content)
	if err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	if res.BalanceWarning == "" {
		t.Error("整批余额警告为空，want 不平警告")
	}
	var mismatched int
	if err := db.Pool().QueryRow(`SELECT COUNT(*) FROM bank_flows WHERE balance_status='mismatch'`).Scan(&mismatched); err != nil {
		t.Fatal(err)
	}
	if mismatched == 0 {
		t.Error("未标记任何 mismatch 行，want 至少 1 行")
	}
	errs, err := NewImportService(db).ListErrors(ctx, res.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	var hasDiscontinuity bool
	for _, e := range errs {
		if e.FieldName == "balance_discontinuity" {
			hasDiscontinuity = true
		}
	}
	if !hasDiscontinuity {
		t.Error("import_errors 缺少 balance_discontinuity 记录")
	}
}

// TestSummaryStateMachine 汇总状态机：reviewed→draft 可驳回，vouchered 不可驳回。
func TestSummaryStateMachine(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	sum := NewSummaryService(db)
	s, err := sum.Save(ctx, SaveSummaryReq{
		Date: "2026-07-10", Category: "支出", Project: "手续费", Reason: "跨行汇款手续费",
		ReceiptNo: "0000301", ExpenseAmount: "9.00", Operator: "tester",
	})
	if err != nil {
		t.Fatalf("保存失败：%v", err)
	}
	// draft 不可直接驳回
	if _, err := sum.Audit(ctx, s.ID, false, "tester", ""); err == nil {
		t.Error("draft 汇总被成功驳回，want 拒绝")
	}
	if _, err := sum.Audit(ctx, s.ID, true, "tester", ""); err != nil {
		t.Fatalf("审核失败：%v", err)
	}
	if _, err := sum.Audit(ctx, s.ID, false, "tester", ""); err != nil {
		t.Fatalf("已审核汇总驳回失败：%v", err)
	}
	full, err := sum.Save(ctx, SaveSummaryReq{
		ID: s.ID, Date: "2026-07-10", Category: "支出", Project: "手续费", Reason: "跨行汇款手续费",
		ReceiptNo: "0000301", ExpenseAmount: "9.00", Operator: "tester",
	})
	if err != nil {
		t.Fatalf("驳回后再保存失败：%v", err)
	}
	if full.Status != "draft" {
		t.Fatalf("驳回后状态 = %q, want draft", full.Status)
	}
}

// TestReceiptNoUnique 收据号唯一：空号可重复，非空号不得重复（F5）。
func TestReceiptNoUnique(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	sum := NewSummaryService(db)
	mk := func(receipt string) error {
		_, err := sum.Save(ctx, SaveSummaryReq{
			Date: "2026-07-10", Category: "收入", Project: "营业收入",
			ReceiptNo: receipt, IncomeAmount: "100.00", Operator: "tester",
		})
		return err
	}
	if err := mk(""); err != nil {
		t.Fatalf("空收据号首条失败：%v", err)
	}
	if err := mk(""); err != nil {
		t.Fatalf("空收据号第二条应允许，却失败：%v", err)
	}
	if err := mk("0000502"); err != nil {
		t.Fatalf("首次使用 0000502 失败：%v", err)
	}
	if err := mk("0000502"); err == nil {
		t.Error("重复收据号被接受，want 拒绝")
	}
}

// TestVoucherTemplateBalanced 五类凭证模板各自借贷相等（§6.5）。
func TestVoucherTemplateBalanced(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	sum := NewSummaryService(db)
	vs := NewVoucherService(db)
	cases := []SaveSummaryReq{
		{Category: "收入", Project: "产康", Reason: "王浩然产康充值", IncomeAmount: "11970.00", FeeActual: "30.00"},
		{Category: "支出", Project: "产品成本", Reason: "货款", ExpenseAmount: "4800.00"},
		{Category: "支出", Project: "护理成本", Reason: "报销", ExpenseAmount: "717.00"},
		{Category: "支出", Project: "手续费", Reason: "跨行汇款手续费", ExpenseAmount: "9.00"},
		{Category: "支出", Project: "水电费", Reason: "2026.6房费", ExpenseAmount: "4552.00"},
	}
	for i, c := range cases {
		c.Date = "2026-07-10"
		c.ReceiptNo = "00008" + string(rune('0'+i))
		c.Operator = "tester"
		s, err := sum.Save(ctx, c)
		if err != nil {
			t.Fatalf("%s 保存失败：%v", c.Project, err)
		}
		if _, err := sum.Audit(ctx, s.ID, true, "tester", ""); err != nil {
			t.Fatal(err)
		}
		entries, err := vs.BuildEntries(ctx, s.ID)
		if err != nil {
			t.Fatalf("%s 模板生成失败：%v", c.Project, err)
		}
		// Create 内含配平校验：不平会报错
		if _, err := vs.Create(ctx, CreateVoucherReq{SummaryID: s.ID, Entries: entries, Operator: "tester"}); err != nil {
			t.Fatalf("%s 凭证生成失败（借贷不平）：%v", c.Project, err)
		}
		if len(entries) < 2 {
			t.Errorf("%s 分录数 = %d, want >= 2", c.Project, len(entries))
		}
	}
}

// TestVoucherTemplateRequiresReviewed 未审核汇总不可生成凭证。
func TestVoucherTemplateRequiresReviewed(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	sum := NewSummaryService(db)
	s, err := sum.Save(ctx, SaveSummaryReq{
		Date: "2026-07-10", Category: "支出", Project: "手续费", Reason: "手续费",
		ExpenseAmount: "9.00", Operator: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewVoucherService(db).BuildEntries(ctx, s.ID); err == nil {
		t.Error("draft 汇总生成了凭证模板，want 拒绝")
	}
}

// TestBalanceSheetCarriesOpening 余额表期初应承接期间之前的发生额，期末 = 期初 + 借 - 贷（F7）。
func TestBalanceSheetCarriesOpening(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	sum := NewSummaryService(db)
	vs := NewVoucherService(db)
	mk := func(req SaveSummaryReq) {
		t.Helper()
		s, err := sum.Save(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sum.Audit(ctx, s.ID, true, "tester", ""); err != nil {
			t.Fatal(err)
		}
		entries, err := vs.BuildEntries(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
		v, err := vs.Create(ctx, CreateVoucherReq{SummaryID: s.ID, Entries: entries, Operator: "tester"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := vs.Audit(ctx, v.ID, "tester"); err != nil {
			t.Fatal(err)
		}
	}
	mk(SaveSummaryReq{Date: "2026-07-10", Category: "收入", Project: "产康", Reason: "r",
		ReceiptNo: "0000601", IncomeAmount: "11970.00", FeeActual: "30.00", Operator: "tester"})
	mk(SaveSummaryReq{Date: "2026-07-13", Category: "支出", Project: "水电费", Reason: "r",
		ReceiptNo: "0000602", ExpenseAmount: "4552.00", Operator: "tester"})

	rs := NewReportService(db)
	// 自 07-11 起：期初应含 07-10 的 11970.00（银行存款）
	rows, err := rs.BalanceSheet(ctx, "2026-07-11", "")
	if err != nil {
		t.Fatal(err)
	}
	var bank *BalanceRow
	for i := range rows {
		if rows[i].AccountCode == "1002" {
			bank = &rows[i]
		}
	}
	if bank == nil {
		t.Fatal("余额表缺少 1002 银行存款")
	}
	if bank.Begin != "11970.00" {
		t.Errorf("1002 期初 = %s, want 11970.00", bank.Begin)
	}
	if bank.Debit != "0.00" || bank.Credit != "4552.00" {
		t.Errorf("1002 本期 借=%s 贷=%s, want 0.00 / 4552.00", bank.Debit, bank.Credit)
	}
	if bank.End != "7418.00" {
		t.Errorf("1002 期末 = %s, want 7418.00（期初 11970 - 贷 4552）", bank.End)
	}
}

// TestReconIncludesBankOnlyDates 银行侧独有日期必须进待处理队列（§6.4）。
func TestReconIncludesBankOnlyDates(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	imp := NewImportService(db)
	bank := "凭证号\t对方账号\t交易时间\t对方单位\t用途\t摘要\t附言\t转入金额\t转出金额\t余额\n" +
		"000000000\tx\t2026-07-01 02:49:15\t待清算商户银行卡POS净收暂收销账户\t\t1001****7575\t\t7,441.35\t\t762,863.23\n"
	if _, err := imp.ImportFile(ctx, "银行流水.tsv", bank); err != nil {
		t.Fatal(err)
	}
	// 订单侧只有 07-02 结算，与银行 07-01 对不上
	order := "工行订单号\t交易时间\t结算日期\t订单金额\t手续费金额\t商户实收金额\t支付方式\t借贷记标识\n" +
		"NO1\t2026-07-01 10:00:00\t2026-07-02\t100.00\t0.25\t99.75\t微信\t借记卡\n"
	if _, err := imp.ImportFile(ctx, "订单流水.tsv", order); err != nil {
		t.Fatal(err)
	}
	diffs, err := NewReconService(db).ListDiffs(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	var hasBankOnly bool
	for _, d := range diffs {
		if d.Date == "2026-07-01" {
			hasBankOnly = true
		}
	}
	if !hasBankOnly {
		t.Errorf("待处理队列未包含银行独有日期 2026-07-01（diff 数=%d）", len(diffs))
	}
}

// TestCSVAndXLSXParsing CSV 逗号分隔与 XLSX 首个工作表都应解析出正确表头。
func TestCSVAndXLSXParsing(t *testing.T) {
	csvContent := "交易时间,转入金额,转出金额,余额\n2026-07-10 02:45:36,11970.00,,657872.62\n"
	h, rows, err := ParseAnyFile("银行流水.csv", csvContent)
	if err != nil {
		t.Fatalf("CSV 解析失败：%v", err)
	}
	if len(h) != 4 || h[0] != "交易时间" || len(rows) != 1 {
		t.Errorf("CSV 解析结果：表头=%v 行数=%d", h, len(rows))
	}

	xlsx := buildTestXLSX(t)
	h2, rows2, err := ParseAnyFile("银行流水.xlsx", xlsx)
	if err != nil {
		t.Fatalf("XLSX 解析失败：%v", err)
	}
	if len(h2) != 3 || h2[0] != "交易时间" || len(rows2) != 1 {
		t.Errorf("XLSX 解析结果：表头=%v 行数=%d", h2, len(rows2))
	}
	if rows2[0][0] != "2026-07-10 02:45:36" {
		t.Errorf("XLSX 时间单元格 = %q", rows2[0][0])
	}
}
