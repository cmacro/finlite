package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"finlite/internal/store"
)

// BankFlowService 银行流水：列表 / 批次确认 / 撤销确认。
// 原始表只读，不提供 Update；确认只写 balance_confirmed_at（可撤销）。
type BankFlowService struct {
	DB *store.DB
}

func NewBankFlowService(db *store.DB) *BankFlowService { return &BankFlowService{DB: db} }

func pageArgs(fPage, fSize int) (page, size, offset int) {
	page, size = fPage, fSize
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	return page, size, (page - 1) * size
}

// List 银行流水分页列表，displayStatus 供 UI 三色：mismatch红 / ok绿 / skip灰。
func (s *BankFlowService) List(ctx context.Context, f BankFlowFilter) (BankFlowPage, error) {
	page, size, offset := pageArgs(f.Page, f.PageSize)
	var where []string
	var args []any
	if f.BatchID != "" {
		where = append(where, "batch_id=?")
		args = append(args, f.BatchID)
	}
	if f.DateFrom != "" {
		where = append(where, "substr(txn_time,1,10)>=?")
		args = append(args, f.DateFrom)
	}
	if f.DateTo != "" {
		where = append(where, "substr(txn_time,1,10)<=?")
		args = append(args, f.DateTo)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		where = append(where, "(counterparty LIKE ? OR summary LIKE ? OR remark LIKE ?)")
		like := "%" + kw + "%"
		args = append(args, like, like, like)
	}
	w := ""
	if len(where) > 0 {
		w = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.DB.Pool().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM bank_flows`+w, args...).Scan(&total); err != nil {
		return BankFlowPage{}, fmt.Errorf("统计银行流水失败：%v", err)
	}
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT id, batch_id, file_name, row_no, txn_time,
			COALESCE(counterparty,''), COALESCE(summary,''), COALESCE(remark,''),
			in_amount, out_amount, balance, balance_status,
			COALESCE(balance_confirmed_at,''), created_at
		 FROM bank_flows`+w+` ORDER BY txn_time, row_no LIMIT ? OFFSET ?`,
		append(args, size, offset)...)
	if err != nil {
		return BankFlowPage{}, fmt.Errorf("查询银行流水失败：%v", err)
	}
	defer rows.Close()
	var items []BankFlow
	for rows.Next() {
		var b BankFlow
		if err := rows.Scan(&b.ID, &b.BatchID, &b.FileName, &b.RowNo, &b.TxnTime,
			&b.Counterparty, &b.Summary, &b.Remark, &b.InAmount, &b.OutAmount,
			&b.Balance, &b.BalanceStatus, &b.BalanceConfirmed, &b.CreatedAt); err != nil {
			return BankFlowPage{}, fmt.Errorf("读取银行流水失败：%v", err)
		}
		if b.BalanceConfirmed != "" {
			b.DisplayStatus = "confirmed"
		} else {
			b.DisplayStatus = b.BalanceStatus
		}
		items = append(items, b)
	}
	if items == nil {
		items = []BankFlow{}
	}
	return BankFlowPage{Items: items, Total: total, Page: page, PageSize: size}, rows.Err()
}

// ConfirmBatch 按批次人工确认余额（唯一消红路径），可撤销。
func (s *BankFlowService) ConfirmBatch(ctx context.Context, batchID string, operator string) (int64, error) {
	if strings.TrimSpace(batchID) == "" {
		return 0, fmt.Errorf("批次不能为空")
	}
	now := time.Now().Format(time.RFC3339)
	// 直接执行（单行更新，原子性由 SQLite 保证）
	res, err := s.DB.Pool().ExecContext(ctx,
		`UPDATE bank_flows SET balance_confirmed_at=? WHERE batch_id=? AND balance_confirmed_at IS NULL`,
		now, batchID)
	if err != nil {
		return 0, fmt.Errorf("确认余额失败：%v", err)
	}
	n, _ := res.RowsAffected()
	_ = operator
	_ = ctx
	return n, nil
}

// UnconfirmBatch 撤销批次确认，恢复红底。
func (s *BankFlowService) UnconfirmBatch(ctx context.Context, batchID string) (int64, error) {
	if strings.TrimSpace(batchID) == "" {
		return 0, fmt.Errorf("批次不能为空")
	}
	res, err := s.DB.Pool().ExecContext(ctx,
		`UPDATE bank_flows SET balance_confirmed_at=NULL WHERE batch_id=? AND balance_confirmed_at IS NOT NULL`,
		batchID)
	if err != nil {
		return 0, fmt.Errorf("撤销确认失败：%v", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
