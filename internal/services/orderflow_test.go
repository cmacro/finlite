package services

import (
	"context"
	"testing"
)

// orderTSV 订单流水测试数据（脱敏示例值）：覆盖费率正确 / 费率差异 / 实收差异 三种状态。
const orderTSV = "商户名称\t工行订单号\t第三方订单号\t交易时间\t结算日期\t订单金额\t手续费金额\t商户实收金额\t支付方式\t借贷记标识\n" +
	"示例商户\t240709001\t4200002456202607081122334455\t2026-07-08 10:12:33\t2026-07-09\t12000.00\t30.00\t11970.00\t微信\t贷记卡\n" +
	"示例商户\t240709002\t4200002456202607086677889900\t2026-07-08 13:48:02\t2026-07-09\t1000.00\t2.50\t997.50\t微信\t借记卡\n" +
	"示例商户\t240710001\t4200002456202607095544332211\t2026-07-09 09:03:18\t2026-07-10\t3200.00\t7.55\t3192.45\t微信\t借记卡\n" +
	"示例商户\t240710002\t4200002456202607091122334455\t2026-07-09 12:37:41\t2026-07-10\t2400.00\t6.00\t2390.00\t微信\t贷记卡\n"

// TestOrderFlowListGroupedBySettleDate 列表按结算日期（T+1）归集，并给出小计与费率标记。
func TestOrderFlowListGroupedBySettleDate(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	if _, err := NewImportService(db).ImportFile(ctx, "订单流水.tsv", orderTSV); err != nil {
		t.Fatalf("导入失败：%v", err)
	}

	page, err := NewOrderFlowService(db).List(ctx, OrderFlowFilter{})
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	if page.Total != 4 {
		t.Fatalf("总行数 = %d, want 4", page.Total)
	}
	if len(page.Groups) != 2 {
		t.Fatalf("结算日期分组数 = %d, want 2", len(page.Groups))
	}
	if page.Groups[0].SettleDate != "2026-07-09" || page.Groups[1].SettleDate != "2026-07-10" {
		t.Fatalf("分组顺序 = %s/%s, want 2026-07-09/2026-07-10",
			page.Groups[0].SettleDate, page.Groups[1].SettleDate)
	}
	if got := page.Groups[0].Count; got != 2 {
		t.Errorf("07-09 笔数 = %d, want 2", got)
	}
	if got := page.Groups[0].OrderTotal; got != "13000.00" {
		t.Errorf("07-09 订单金额小计 = %s, want 13000.00", got)
	}
	if got := page.Groups[0].FeeTotal; got != "32.50" {
		t.Errorf("07-09 手续费小计 = %s, want 32.50", got)
	}
	if got := page.Groups[0].NetTotal; got != "12967.50" {
		t.Errorf("07-09 实收小计 = %s, want 12967.50", got)
	}
	if got := page.Groups[0].AnomalyCount; got != 0 {
		t.Errorf("07-09 异常筆数 = %d, want 0", got)
	}

	// 07-10：3200.00 × 0.25% = 8.00 ≠ 7.55（费率差异）；2400.00 − 6.00 = 2394.00 ≠ 2390.00（实收差异）
	if got := page.Groups[1].AnomalyCount; got != 2 {
		t.Errorf("07-10 异常笔数 = %d, want 2", got)
	}
	if page.FeeDiffCount != 1 || page.NetDiffCount != 1 || page.ParseErrCount != 0 {
		t.Errorf("异常分类 = fee %d / net %d / parse %d, want 1/1/0",
			page.FeeDiffCount, page.NetDiffCount, page.ParseErrCount)
	}
	if page.AnomalyCount != 2 {
		t.Errorf("异常合计 = %d, want 2", page.AnomalyCount)
	}

	// 全量合计：订单金额 18600.00 − 手续费 46.05 − 实收 18549.95 = 4.00（实收差异行所致）
	if page.OrderTotal != "18600.00" || page.FeeTotal != "46.05" || page.NetTotal != "18549.95" {
		t.Errorf("全量合计 = %s / %s / %s, want 18600.00 / 46.05 / 18549.95",
			page.OrderTotal, page.FeeTotal, page.NetTotal)
	}
	if page.ReconDiff != "4.00" {
		t.Errorf("勾稽差额 = %s, want 4.00", page.ReconDiff)
	}

	// 费率与金额恒等式字段由 decimal 计算，前端只展示。
	first := page.Groups[0].Items[0]
	if first.FeeFlag != "" {
		t.Errorf("07-09 首行费率标记 = %q, want 空", first.FeeFlag)
	}
	if first.FeeRate != "0.25%" {
		t.Errorf("费率展示 = %q, want 0.25%%", first.FeeRate)
	}
	if first.ExpectedFee != "30.00" || first.ExpectedNet != "11970.00" {
		t.Errorf("预期手续费/应到实收 = %s / %s, want 30.00 / 11970.00",
			first.ExpectedFee, first.ExpectedNet)
	}
	if first.FileName != "订单流水.tsv" || first.SourceRowNo != 2 {
		t.Errorf("来源文件/行号 = %q / %d, want 订单流水.tsv / 2", first.FileName, first.SourceRowNo)
	}
}

// TestOrderFlowListOnlyFeeAdj 仅看费率差异：只返回有标记的行，且统计随之收敛。
func TestOrderFlowListOnlyFeeAdj(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	if _, err := NewImportService(db).ImportFile(ctx, "订单流水.tsv", orderTSV); err != nil {
		t.Fatalf("导入失败：%v", err)
	}

	page, err := NewOrderFlowService(db).List(ctx, OrderFlowFilter{OnlyFeeAdj: "1"})
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	if page.Total != 2 {
		t.Fatalf("异常行数 = %d, want 2", page.Total)
	}
	if len(page.Groups) != 1 || page.Groups[0].SettleDate != "2026-07-10" {
		t.Fatalf("分组 = %+v, want 仅 2026-07-10", page.Groups)
	}
	if page.Groups[0].Count != 2 {
		t.Errorf("07-10 笔数 = %d, want 2", page.Groups[0].Count)
	}
}

// TestOrderFlowListFilters 结算日期与关键字筛选（关键字同时命中工行/第三方订单号）。
func TestOrderFlowListFilters(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	if _, err := NewImportService(db).ImportFile(ctx, "订单流水.tsv", orderTSV); err != nil {
		t.Fatalf("导入失败：%v", err)
	}
	svc := NewOrderFlowService(db)

	page, err := svc.List(ctx, OrderFlowFilter{SettleDate: "2026-07-10"})
	if err != nil {
		t.Fatalf("按结算日期查询失败：%v", err)
	}
	if page.Total != 2 || len(page.Groups) != 1 {
		t.Errorf("按结算日期筛选 = %d 行 / %d 组, want 2 / 1", page.Total, len(page.Groups))
	}

	page, err = svc.List(ctx, OrderFlowFilter{Keyword: "420000245620260708"})
	if err != nil {
		t.Fatalf("按关键字查询失败：%v", err)
	}
	if page.Total != 2 {
		t.Errorf("按第三方订单号筛选 = %d 行, want 2", page.Total)
	}

	page, err = svc.List(ctx, OrderFlowFilter{Keyword: "240710"})
	if err != nil {
		t.Fatalf("按工行订单号查询失败：%v", err)
	}
	if page.Total != 2 {
		t.Errorf("按工行订单号筛选 = %d 行, want 2", page.Total)
	}
}
