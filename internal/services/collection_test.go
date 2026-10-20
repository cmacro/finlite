package services

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
)

// TestCollectionLifecycle 店面收款状态机：草稿 → 确认 → 生成汇总（source='collection'）。
// 已汇总记录不可再编辑 / 作废；一收款一汇总（重复生成直接拒绝）。
func TestCollectionLifecycle(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewCollectionService(db)

	receipt := base64.StdEncoding.EncodeToString([]byte("fake-png-bytes"))
	saved, err := svc.Save(ctx, SaveCollectionReq{
		Date: "2026-07-10", Store: "望京旗舰店", Customer: "客户A",
		Project: "会员年卡续费", PayMethod: "cash", Account: "cash",
		Amount: "1,280.00", ReceiptNo: "SR-20260710-01", Operator: "local",
		Attachments: []CollectionAttachmentInput{{FileName: "收款单据.png", DataBase64: receipt}},
	})
	if err != nil {
		t.Fatalf("保存草稿失败：%v", err)
	}
	if !strings.HasPrefix(saved.ID, "SC260710") {
		t.Errorf("收款单号 = %q, want 前缀 SC260710", saved.ID)
	}
	if saved.Status != collectionDraft {
		t.Errorf("状态 = %q, want draft", saved.Status)
	}
	if saved.AttachmentCount != 1 {
		t.Errorf("附件数 = %d, want 1", saved.AttachmentCount)
	}

	page, err := svc.List(ctx, CollectionFilter{DateFrom: "2026-07-01", DateTo: "2026-07-31"})
	if err != nil {
		t.Fatalf("列表失败：%v", err)
	}
	if page.Stats.DraftCount != 1 || page.Stats.DraftAmount != "1280.00" {
		t.Errorf("草稿统计 = %+v, want 1 笔 / 1280.00", page.Stats)
	}
	if page.TotalAmount != "1280.00" {
		t.Errorf("筛选合计 = %q, want 1280.00", page.TotalAmount)
	}

	// 重复收据号必须拒绝
	if _, err := svc.Save(ctx, SaveCollectionReq{
		Date: "2026-07-11", Store: "望京旗舰店", Customer: "客户B",
		Project: "健康管理套餐", PayMethod: "cash", Account: "cash",
		Amount: "100.00", ReceiptNo: "SR-20260710-01", Operator: "local",
	}); err == nil || !strings.Contains(err.Error(), "收据号已存在") {
		t.Errorf("重复收据号错误 = %v, want 收据号已存在", err)
	}

	if _, err := svc.Confirm(ctx, saved.ID, "local"); err != nil {
		t.Fatalf("确认收款失败：%v", err)
	}
	ref, err := svc.GenerateSummary(ctx, saved.ID, "local")
	if err != nil {
		t.Fatalf("生成汇总失败：%v", err)
	}
	var source, status, income string
	if err := db.Pool().QueryRow(`SELECT source, status, income_amount FROM data_summaries WHERE id=?`,
		ref.SummaryID).Scan(&source, &status, &income); err != nil {
		t.Fatalf("读取汇总失败：%v", err)
	}
	if source != "collection" || status != "draft" || income != "1280.00" {
		t.Errorf("汇总 = source:%q status:%q income:%q, want collection/draft/1280.00", source, status, income)
	}

	if _, err := svc.GenerateSummary(ctx, saved.ID, "local"); err == nil {
		t.Error("重复生成汇总应失败")
	}
	if _, err := svc.Save(ctx, SaveCollectionReq{ID: saved.ID, Date: "2026-07-10",
		Store: "望京旗舰店", Customer: "客户A", Project: "会员年卡续费",
		PayMethod: "cash", Account: "cash", Amount: "1280.00", Operator: "local"}); err == nil {
		t.Error("已汇总记录编辑应失败")
	}
	if _, err := svc.Void(ctx, saved.ID, "客户换卡", "local"); err == nil {
		t.Error("已汇总记录作废应失败")
	}

	detail, err := svc.Get(ctx, saved.ID)
	if err != nil {
		t.Fatalf("读取详情失败：%v", err)
	}
	if len(detail.Attachments) != 1 || len(detail.AuditLogs) == 0 {
		t.Errorf("详情附件 = %d, 操作记录 = %d, want ≥1/≥1", len(detail.Attachments), len(detail.AuditLogs))
	}

	// 店面收款汇总不参与订单 POS 三方勾稽：同日无订单/银行 POS 时应判为已匹配，
	// 否则现金收款会把当日汇总收入抬高、误报差异。
	recon, err := NewReconService(db).ReconcileByDate(ctx, "2026-07-10")
	if err != nil {
		t.Fatalf("三方勾稽失败：%v", err)
	}
	if !recon.Matched || recon.SummaryActual != "0.00" {
		t.Errorf("三方勾稽 = %+v, want 已匹配且汇总实际收入 0.00", recon)
	}
}

// TestCollectionReconChannelMatch 通道对账：对公转账按金额匹配银行转入，未匹配进待处理队列。
func TestCollectionReconChannelMatch(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewCollectionService(db)

	if _, err := db.Pool().Exec(`INSERT INTO bank_flows
		(id, batch_id, file_name, row_no, txn_time, counterparty, summary, in_amount, out_amount,
		 balance, balance_status, created_at)
		VALUES ('bf1', 'b1', '银行流水.tsv', 1, '2026-07-16 10:00:00', '客户A', '货款',
			'12000.00', '0', '0', 'ok', '2026-07-16T10:00:00Z')`); err != nil {
		t.Fatalf("准备银行流水失败：%v", err)
	}

	matched, err := svc.Save(ctx, SaveCollectionReq{
		Date: "2026-07-15", Store: "国贸店", Customer: "客户A",
		Project: "企业健康服务年费", PayMethod: "bank_transfer", Account: "corporate",
		Amount: "12000.00", Operator: "local", Submit: true,
	})
	if err != nil {
		t.Fatalf("保存对公收款失败：%v", err)
	}
	if matched.Status != collectionConfirmed {
		t.Errorf("提交后状态 = %q, want confirmed", matched.Status)
	}
	unmatched, err := svc.Save(ctx, SaveCollectionReq{
		Date: "2026-07-20", Store: "国贸店", Customer: "客户C",
		Project: "企业健康讲座", PayMethod: "bank_transfer", Account: "corporate",
		Amount: "9500.00", Operator: "local", Submit: true,
	})
	if err != nil {
		t.Fatalf("保存未匹配收款失败：%v", err)
	}

	r, err := svc.Recon(ctx, CollectionFilter{DateFrom: "2026-07-01", DateTo: "2026-07-31"})
	if err != nil {
		t.Fatalf("通道对账失败：%v", err)
	}
	if len(r.Channels) != 2 {
		t.Fatalf("通道数 = %d, want 2", len(r.Channels))
	}
	corp := r.Channels[1]
	if corp.RegisterTotal != "21500.00" || corp.ReconTotal != "12000.00" || corp.Diff != "9500.00" {
		t.Errorf("对公通道 = %+v, want 登记 21500.00 / 到账 12000.00 / 差异 9500.00", corp)
	}
	if corp.Status != "pending" {
		t.Errorf("对公通道状态 = %q, want pending", corp.Status)
	}
	if r.PendingCount != 1 {
		t.Errorf("待处理数 = %d, want 1（仅未匹配银行的收款）", r.PendingCount)
	}
	found := false
	for _, p := range r.Pending {
		if p.ID == unmatched.ID && p.Channel == "corporate" {
			found = true
		}
	}
	if !found {
		t.Errorf("待处理队列缺少未匹配收款 %s：%+v", unmatched.ID, r.Pending)
	}

	// 确认后生成汇总 → 非银/已汇总收款不再计入待处理。
	if _, err := svc.GenerateSummary(ctx, matched.ID, "local"); err != nil {
		t.Fatalf("生成汇总失败：%v", err)
	}
}
