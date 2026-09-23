package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"finlite/internal/decimal"
	"finlite/internal/store"

	"github.com/google/uuid"
)

// SummaryService 数据汇总：CRUD / 自动预填 / 审核流。
// 状态机：draft → reviewed → vouchered；可驳回 reviewed → draft。
type SummaryService struct {
	DB *store.DB
}

func NewSummaryService(db *store.DB) *SummaryService { return &SummaryService{DB: db} }

// List 汇总分页。
func (s *SummaryService) List(ctx context.Context, f SummaryFilter) (SummaryPage, error) {
	page, size, offset := pageArgs(f.Page, f.PageSize)
	var where []string
	var args []any
	if f.DateFrom != "" {
		where = append(where, "date>=?")
		args = append(args, f.DateFrom)
	}
	if f.DateTo != "" {
		where = append(where, "date<=?")
		args = append(args, f.DateTo)
	}
	if f.Status != "" {
		where = append(where, "status=?")
		args = append(args, f.Status)
	}
	w := ""
	if len(where) > 0 {
		w = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.DB.Pool().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM data_summaries`+w, args...).Scan(&total); err != nil {
		return SummaryPage{}, fmt.Errorf("统计数据汇总失败：%v", err)
	}
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT id, date, COALESCE(receipt_no,''), category, project, COALESCE(reason,''),
			income_amount, expense_amount, fee_auto, fee_actual, fee_manual_adjusted,
			balance, COALESCE(remark,''), source, status, COALESCE(voucher_id,''),
			created_at, updated_at
		 FROM data_summaries`+w+` ORDER BY date, created_at LIMIT ? OFFSET ?`,
		append(args, size, offset)...)
	if err != nil {
		return SummaryPage{}, fmt.Errorf("查询数据汇总失败：%v", err)
	}
	defer rows.Close()
	var items []Summary
	for rows.Next() {
		var sm Summary
		if err := rows.Scan(&sm.ID, &sm.Date, &sm.ReceiptNo, &sm.Category, &sm.Project,
			&sm.Reason, &sm.IncomeAmount, &sm.ExpenseAmount, &sm.FeeAuto, &sm.FeeActual,
			&sm.FeeManualAdjusted, &sm.Balance, &sm.Remark, &sm.Source, &sm.Status,
			&sm.VoucherID, &sm.CreatedAt, &sm.UpdatedAt); err != nil {
			return SummaryPage{}, fmt.Errorf("读取数据汇总失败：%v", err)
		}
		items = append(items, sm)
	}
	if items == nil {
		items = []Summary{}
	}
	return SummaryPage{Items: items, Total: total, Page: page, PageSize: size}, rows.Err()
}

// Save 新增或更新汇总草稿（仅 draft 可改）。手工改 fee 置 fee_manual_adjusted=1。
func (s *SummaryService) Save(ctx context.Context, req SaveSummaryReq) (Summary, error) {
	if strings.TrimSpace(req.Date) == "" {
		return Summary{}, fmt.Errorf("日期不能为空")
	}
	if req.Category != "收入" && req.Category != "支出" {
		return Summary{}, fmt.Errorf("一级分类只能是 收入/支出")
	}
	if _, err := decimal.Parse(req.IncomeAmount); err != nil {
		return Summary{}, fmt.Errorf("实际收入金额不合法：%s", req.IncomeAmount)
	}
	if _, err := decimal.Parse(req.ExpenseAmount); err != nil {
		return Summary{}, fmt.Errorf("支出金额不合法：%s", req.ExpenseAmount)
	}
	now := time.Now().Format(time.RFC3339)
	var out Summary
	err := s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		if err := checkReceiptNoUnique(tx, req.ReceiptNo, req.ID); err != nil {
			return err
		}
		if strings.TrimSpace(req.ID) == "" {
			id := uuid.NewString()
			feeAuto := req.FeeActual
			if _, err := tx.Exec(`INSERT INTO data_summaries
				(id, date, receipt_no, category, project, reason, income_amount, expense_amount,
				 fee_auto, fee_actual, fee_manual_adjusted, balance, remark, source, status, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, 'manual', 'draft', ?, ?)`,
				id, req.Date, req.ReceiptNo, req.Category, req.Project, req.Reason,
				decOrZero(req.IncomeAmount), decOrZero(req.ExpenseAmount),
				decOrZero(feeAuto), decOrZero(req.FeeActual),
				decOrZero(req.Balance), req.Remark, now, now); err != nil {
				return fmt.Errorf("新增汇总失败：%v", err)
			}
			newJSON, _ := json.Marshal(req)
			if err := store.WriteAudit(tx, "data_summary", id, "create", "", string(newJSON), req.Operator, ""); err != nil {
				return fmt.Errorf("写审计日志失败：%v", err)
			}
			out = Summary{ID: id, Date: req.Date, Status: "draft"}
			return nil
		}
		// 更新：仅 draft 可改
		var status, oldJSON string
		old := Summary{}
		err := tx.QueryRow(`SELECT id, date, COALESCE(receipt_no,''), category, project, COALESCE(reason,''),
			income_amount, expense_amount, fee_auto, fee_actual, fee_manual_adjusted,
			balance, COALESCE(remark,''), source, status, COALESCE(voucher_id,''),
			created_at, updated_at FROM data_summaries WHERE id=?`, req.ID).
			Scan(&old.ID, &old.Date, &old.ReceiptNo, &old.Category, &old.Project, &old.Reason,
				&old.IncomeAmount, &old.ExpenseAmount, &old.FeeAuto, &old.FeeActual,
				&old.FeeManualAdjusted, &old.Balance, &old.Remark, &old.Source, &status,
				&old.VoucherID, &old.CreatedAt, &old.UpdatedAt)
		if err == sql.ErrNoRows {
			return fmt.Errorf("汇总不存在：%s", req.ID)
		}
		if err != nil {
			return fmt.Errorf("读取汇总失败：%v", err)
		}
		if status != "draft" {
			return fmt.Errorf("只有草稿可修改，当前状态：%s", status)
		}
		oldJSONBytes, _ := json.Marshal(old)
		oldJSON = string(oldJSONBytes)
		manual := 0
		if req.FeeActual != old.FeeAuto && req.FeeActual != old.FeeActual {
			manual = 1
		}
		if _, err := tx.Exec(`UPDATE data_summaries SET date=?, receipt_no=?, category=?, project=?,
			reason=?, income_amount=?, expense_amount=?, fee_actual=?, fee_manual_adjusted=?,
			balance=?, remark=?, updated_at=? WHERE id=?`,
			req.Date, req.ReceiptNo, req.Category, req.Project, req.Reason,
			decOrZero(req.IncomeAmount), decOrZero(req.ExpenseAmount), decOrZero(req.FeeActual),
			manual, decOrZero(req.Balance), req.Remark, now, req.ID); err != nil {
			return fmt.Errorf("更新汇总失败：%v", err)
		}
		newJSON, _ := json.Marshal(req)
		if err := store.WriteAudit(tx, "data_summary", req.ID, "update", oldJSON, string(newJSON), req.Operator, ""); err != nil {
			return fmt.Errorf("写审计日志失败：%v", err)
		}
		out = Summary{ID: req.ID, Date: req.Date, Status: "draft"}
		return nil
	})
	if err != nil {
		return Summary{}, err
	}
	return out, nil
}

// Audit 审核 / 驳回汇总。
// 状态机（业务文档 §5）：draft → reviewed；reviewed → draft（驳回）。
// vouchered 为终态：已生成凭证的汇总不可驳回，否则会绕过「一汇总一凭证」。
func (s *SummaryService) Audit(ctx context.Context, id string, approve bool, operator, comment string) (Summary, error) {
	if strings.TrimSpace(id) == "" {
		return Summary{}, fmt.Errorf("汇总不能为空")
	}
	now := time.Now().Format(time.RFC3339)
	target := "reviewed"
	action := "review"
	from := "draft"
	if !approve {
		target = "draft"
		action = "reject"
		from = "reviewed"
	}
	var out Summary
	err := s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var cur string
		if err := tx.QueryRow(`SELECT status FROM data_summaries WHERE id=?`, id).Scan(&cur); err == sql.ErrNoRows {
			return fmt.Errorf("汇总不存在：%s", id)
		} else if err != nil {
			return fmt.Errorf("读取汇总失败：%v", err)
		}
		if cur != from {
			if cur == "vouchered" {
				return fmt.Errorf("已生成凭证的汇总不可驳回或重新审核，请先反审核并作废对应凭证")
			}
			return fmt.Errorf("汇总状态不允许该操作，当前：%s（需为 %s）", cur, from)
		}
		if _, err := tx.Exec(`UPDATE data_summaries SET status=?, updated_at=? WHERE id=?`, target, now, id); err != nil {
			return fmt.Errorf("审核汇总失败：%v", err)
		}
		if err := store.WriteAudit(tx, "data_summary", id, action,
			`{"status":"`+cur+`"}`, `{"status":"`+target+`"}`, operator, comment); err != nil {
			return fmt.Errorf("写审计日志失败：%v", err)
		}
		out = Summary{ID: id, Status: target}
		return nil
	})
	if err != nil {
		return Summary{}, err
	}
	return out, nil
}

// GenerateFromRecon 由对账结果预填汇总草稿（source='auto'，事由留空待人工补）。
func (s *SummaryService) GenerateFromRecon(ctx context.Context, date string, operator string) (Summary, error) {
	recon := NewReconService(s.DB)
	r, err := recon.ReconcileByDate(ctx, date)
	if err != nil {
		return Summary{}, err
	}
	now := time.Now().Format(time.RFC3339)
	id := uuid.NewString()
	err = s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO data_summaries
			(id, date, category, project, reason, income_amount, expense_amount,
			 fee_auto, fee_actual, fee_manual_adjusted, balance, remark, source, status, created_at, updated_at)
			VALUES (?, ?, '收入', '营业收入', '', ?, '0', '0', '0', 0, ?, '对账自动预填', 'auto', 'draft', ?, ?)`,
			id, date, r.BankPOSIn, r.BankPOSIn, now, now); err != nil {
			return fmt.Errorf("预填汇总失败：%v", err)
		}
		return store.WriteAudit(tx, "data_summary", id, "generate",
			"", `{"date":"`+date+`","orderNet":"`+r.OrderNet+`"}`, operator, "对账自动预填")
	})
	if err != nil {
		return Summary{}, err
	}
	return Summary{ID: id, Date: date, Status: "draft", Source: "auto"}, nil
}

// checkReceiptNoUnique 收据号唯一（业务文档 §7 F5）：空号不占用，排除自身后不得重复。
// 数据库侧另有部分唯一索引兜底（upgrade/01-receipt-no-unique.sql），此处提供中文报错。
func checkReceiptNoUnique(tx *sql.Tx, receiptNo, selfID string) error {
	no := strings.TrimSpace(receiptNo)
	if no == "" {
		return nil
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM data_summaries
		WHERE receipt_no=? AND id<>?`, no, selfID).Scan(&n); err != nil {
		return fmt.Errorf("校验收据号失败：%v", err)
	}
	if n > 0 {
		return fmt.Errorf("收据号已存在：%s", no)
	}
	return nil
}

func decOrZero(s string) string {
	if d, err := decimal.Parse(s); err == nil {
		return d.String()
	}
	return "0"
}
