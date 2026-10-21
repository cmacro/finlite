package services

import (
	"context"
	"encoding/base64"
	"os"
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

// TestCollectionAttachmentExternalized 附件内容外置：库内只存 sha256/rel_path，
// 文件落在账套 attachments/ 目录，读取与删除都走文件（不再有 BLOB）。
func TestCollectionAttachmentExternalized(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewCollectionService(db)

	png := []byte("fake-png-bytes-for-evidence")
	saved, err := svc.Save(ctx, SaveCollectionReq{
		Date: "2026-07-10", Store: "望京旗舰店", Customer: "客户A",
		Project: "会员年卡续费", PayMethod: "cash", Account: "cash",
		Amount: "1280.00", ReceiptNo: "SR-20260710-09", Operator: "local",
		Attachments: []CollectionAttachmentInput{{
			FileName: "收款单据.png", DataBase64: base64.StdEncoding.EncodeToString(png),
		}},
	})
	if err != nil {
		t.Fatalf("保存失败：%v", err)
	}

	// 表结构：content 列已移除，只留指纹与相对路径。
	cols := map[string]bool{}
	rows, err := db.Pool().Query(`PRAGMA table_info(collection_attachments)`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var (
			cid       int
			name, typ string
			notNull   int
			dflt      any
			pk        int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	rows.Close()
	if cols["content"] {
		t.Fatal("collection_attachments 仍含 content 列，附件仍存在库里")
	}
	if !cols["sha256"] || !cols["rel_path"] {
		t.Fatalf("附件表缺少指纹列：%v", cols)
	}

	// 行上有指纹，文件存在且在账套目录内。
	var sha, rel string
	if err := db.Pool().QueryRow(`SELECT sha256, rel_path FROM collection_attachments WHERE collection_id=?`,
		saved.ID).Scan(&sha, &rel); err != nil {
		t.Fatalf("读取附件指纹失败：%v", err)
	}
	if len(sha) != 64 || !strings.HasPrefix(rel, "attachments/") {
		t.Fatalf("附件指纹/路径异常：sha=%q rel=%q", sha, rel)
	}
	abs, err := db.Ledger().Abs(rel)
	if err != nil {
		t.Fatalf("附件路径越界：%v", err)
	}
	if got, err := os.ReadFile(abs); err != nil || string(got) != string(png) {
		t.Fatalf("附件文件内容不符：%v", err)
	}

	// 读取仍返回原始字节（前端预览接口不变）。
	detail, err := svc.Get(ctx, saved.ID)
	if err != nil || len(detail.Attachments) != 1 {
		t.Fatalf("详情附件异常：%v, %d", err, len(detail.Attachments))
	}
	data, err := svc.GetAttachment(ctx, detail.Attachments[0].ID)
	if err != nil {
		t.Fatalf("读取附件失败：%v", err)
	}
	if decoded, derr := base64.StdEncoding.DecodeString(data.DataBase64); derr != nil || string(decoded) != string(png) {
		t.Fatalf("附件内容回读不一致：%v", derr)
	}

	// 编辑时去掉附件 → 行删除、文件清理。
	if _, err := svc.Save(ctx, SaveCollectionReq{
		ID: saved.ID, Date: "2026-07-10", Store: "望京旗舰店", Customer: "客户A",
		Project: "会员年卡续费", PayMethod: "cash", Account: "cash",
		Amount: "1280.00", ReceiptNo: "SR-20260710-09", Operator: "local",
	}); err != nil {
		t.Fatalf("编辑保存失败：%v", err)
	}
	var n int
	if err := db.Pool().QueryRow(`SELECT COUNT(*) FROM collection_attachments WHERE collection_id=?`,
		saved.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("附件行未删除：%d", n)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Errorf("附件文件未清理：%v", err)
	}
}

// TestCollectionAttachmentSharedFile 同一张照片被两条记录引用时只存一份；
// 删掉其中一条不删文件，全部删除后才清理。
func TestCollectionAttachmentSharedFile(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewCollectionService(db)

	jpg := []byte("same-jpeg-bytes")
	b64 := base64.StdEncoding.EncodeToString(jpg)
	newReq := func(receipt string) SaveCollectionReq {
		return SaveCollectionReq{
			Date: "2026-07-11", Store: "望京旗舰店", Customer: "客户B",
			Project: "健康管理套餐", PayMethod: "wechat", Account: "personal",
			Amount: "199.00", ReceiptNo: receipt, Operator: "local",
			Attachments: []CollectionAttachmentInput{{FileName: "凭据.jpg", DataBase64: b64}},
		}
	}
	a, err := svc.Save(ctx, newReq("SR-20260711-01"))
	if err != nil {
		t.Fatalf("保存第一条失败：%v", err)
	}
	b, err := svc.Save(ctx, newReq("SR-20260711-02"))
	if err != nil {
		t.Fatalf("保存第二条失败：%v", err)
	}

	paths := map[string]int{}
	rows, err := db.Pool().Query(`SELECT rel_path FROM collection_attachments`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		paths[p]++
	}
	rows.Close()
	if len(paths) != 1 {
		t.Fatalf("同一内容落出多个文件：%v", paths)
	}
	var rel string
	for p := range paths {
		rel = p
	}
	abs, _ := db.Ledger().Abs(rel)

	// 删掉第一条记录的附件：文件仍被第二条引用，必须保留。
	if _, err := svc.Save(ctx, SaveCollectionReq{
		ID: a.ID, Date: "2026-07-11", Store: "望京旗舰店", Customer: "客户B",
		Project: "健康管理套餐", PayMethod: "wechat", Account: "personal",
		Amount: "199.00", ReceiptNo: "SR-20260711-01", Operator: "local",
	}); err != nil {
		t.Fatalf("编辑第一条失败：%v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("共享附件被提前删除：%v", err)
	}

	// 再删掉第二条：引用归零，文件清理。
	if _, err := svc.Save(ctx, SaveCollectionReq{
		ID: b.ID, Date: "2026-07-11", Store: "望京旗舰店", Customer: "客户B",
		Project: "健康管理套餐", PayMethod: "wechat", Account: "personal",
		Amount: "199.00", ReceiptNo: "SR-20260711-02", Operator: "local",
	}); err != nil {
		t.Fatalf("编辑第二条失败：%v", err)
	}
	if _, err := os.Stat(abs); !os.IsNotExist(err) {
		t.Errorf("引用归零后附件文件仍在：%v", err)
	}
}
