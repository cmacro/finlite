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

// VoucherService 凭证：新增 / 列表 / 详情 / 审核 / 反审核。
// 约束：借贷必平；仅 reviewed 汇总可生成；一汇总一凭证（业务文档 §6.5）。
type VoucherService struct {
	DB *store.DB
}

func NewVoucherService(db *store.DB) *VoucherService { return &VoucherService{DB: db} }

// nextVoucherNo 按日期生成凭证号：记-YYYYMMDD-NNN。
func nextVoucherNo(tx *sql.Tx, date string) (string, error) {
	prefix := "记-" + strings.ReplaceAll(date, "-", "") + "-"
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM vouchers WHERE voucher_no LIKE ?`, prefix+"%").Scan(&n); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%03d", prefix, n+1), nil
}

// checkBalanced 借贷配平检查，中文差额报错。
func checkBalanced(entries []VoucherEntry) error {
	debit := decimal.MustParse("0")
	credit := decimal.MustParse("0")
	for _, e := range entries {
		d, err := decimal.Parse(e.Debit)
		if err != nil {
			return fmt.Errorf("借方金额不合法：%s", e.Debit)
		}
		c, err := decimal.Parse(e.Credit)
		if err != nil {
			return fmt.Errorf("贷方金额不合法：%s", e.Credit)
		}
		debit = debit.Add(d)
		credit = credit.Add(c)
	}
	if !debit.Equal(credit) {
		return fmt.Errorf("借贷不平：借 %s ≠ 贷 %s", decimal.Format(debit), decimal.Format(credit))
	}
	return nil
}

// Create 由已审核汇总生成凭证（一汇总一凭证）。
func (s *VoucherService) Create(ctx context.Context, req CreateVoucherReq) (Voucher, error) {
	if strings.TrimSpace(req.SummaryID) == "" {
		return Voucher{}, fmt.Errorf("汇总不能为空")
	}
	if len(req.Entries) == 0 {
		return Voucher{}, fmt.Errorf("分录不能为空")
	}
	if err := checkBalanced(req.Entries); err != nil {
		return Voucher{}, err
	}
	now := time.Now().Format(time.RFC3339)
	var out Voucher
	err := s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var status, sumDate, reason string
		var existed string
		err := tx.QueryRow(`SELECT status, date, COALESCE(reason,'') FROM data_summaries WHERE id=?`,
			req.SummaryID).Scan(&status, &sumDate, &reason)
		if err == sql.ErrNoRows {
			return fmt.Errorf("汇总不存在：%s", req.SummaryID)
		}
		if err != nil {
			return fmt.Errorf("读取汇总失败：%v", err)
		}
		if status != "reviewed" {
			return fmt.Errorf("只有已审核汇总可生成凭证，当前状态：%s", status)
		}
		err = tx.QueryRow(`SELECT id FROM vouchers WHERE source_summary_id=?`, req.SummaryID).Scan(&existed)
		if err == nil {
			return fmt.Errorf("该汇总已生成凭证，不可重复生成")
		}
		if err != sql.ErrNoRows {
			return fmt.Errorf("检查重复凭证失败：%v", err)
		}
		no, err := nextVoucherNo(tx, sumDate)
		if err != nil {
			return fmt.Errorf("生成凭证号失败：%v", err)
		}
		id := uuid.NewString()
		if _, err := tx.Exec(`INSERT INTO vouchers
			(id, voucher_date, voucher_no, summary, status, source_summary_id, created_at)
			VALUES (?, ?, ?, ?, 'draft', ?, ?)`,
			id, sumDate, no, reason, req.SummaryID, now); err != nil {
			return fmt.Errorf("新增凭证失败：%v", err)
		}
		for i, e := range req.Entries {
			if _, err := tx.Exec(`INSERT INTO voucher_entries
				(id, voucher_id, account_code, account_name, debit, credit, remark, sort_no)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				uuid.NewString(), id, e.AccountCode, e.AccountName,
				decOrZero(e.Debit), decOrZero(e.Credit), e.Remark, i+1); err != nil {
				return fmt.Errorf("写入分录失败：%v", err)
			}
		}
		if _, err := tx.Exec(`UPDATE data_summaries SET status='vouchered', voucher_id=?, updated_at=? WHERE id=?`,
			id, now, req.SummaryID); err != nil {
			return fmt.Errorf("回写汇总状态失败：%v", err)
		}
		newJSON, _ := json.Marshal(req.Entries)
		if err := store.WriteAudit(tx, "voucher", id, "create", "", string(newJSON), req.Operator, ""); err != nil {
			return fmt.Errorf("写审计日志失败：%v", err)
		}
		out = Voucher{ID: id, VoucherDate: sumDate, VoucherNo: no, Summary: reason, Status: "draft", SourceSummaryID: req.SummaryID}
		return nil
	})
	if err != nil {
		return Voucher{}, err
	}
	return out, nil
}

// List 凭证分页。
func (s *VoucherService) List(ctx context.Context, f VoucherFilter) (VoucherPage, error) {
	page, size, offset := pageArgs(f.Page, f.PageSize)
	var where []string
	var args []any
	if f.DateFrom != "" {
		where = append(where, "voucher_date>=?")
		args = append(args, f.DateFrom)
	}
	if f.DateTo != "" {
		where = append(where, "voucher_date<=?")
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
		`SELECT COUNT(*) FROM vouchers`+w, args...).Scan(&total); err != nil {
		return VoucherPage{}, fmt.Errorf("统计凭证失败：%v", err)
	}
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT id, voucher_date, voucher_no, summary, status,
			COALESCE(source_summary_id,''), created_at
		 FROM vouchers`+w+` ORDER BY voucher_date DESC, voucher_no DESC LIMIT ? OFFSET ?`,
		append(args, size, offset)...)
	if err != nil {
		return VoucherPage{}, fmt.Errorf("查询凭证失败：%v", err)
	}
	defer rows.Close()
	var items []Voucher
	for rows.Next() {
		var v Voucher
		if err := rows.Scan(&v.ID, &v.VoucherDate, &v.VoucherNo, &v.Summary,
			&v.Status, &v.SourceSummaryID, &v.CreatedAt); err != nil {
			return VoucherPage{}, fmt.Errorf("读取凭证失败：%v", err)
		}
		items = append(items, v)
	}
	if items == nil {
		items = []Voucher{}
	}
	return VoucherPage{Items: items, Total: total, Page: page, PageSize: size}, rows.Err()
}

// Get 凭证详情（含分录）。
func (s *VoucherService) Get(ctx context.Context, id string) (Voucher, error) {
	var v Voucher
	err := s.DB.Pool().QueryRowContext(ctx,
		`SELECT id, voucher_date, voucher_no, summary, status,
			COALESCE(source_summary_id,''), created_at FROM vouchers WHERE id=?`, id).
		Scan(&v.ID, &v.VoucherDate, &v.VoucherNo, &v.Summary, &v.Status, &v.SourceSummaryID, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return Voucher{}, fmt.Errorf("凭证不存在：%s", id)
	}
	if err != nil {
		return Voucher{}, fmt.Errorf("读取凭证失败：%v", err)
	}
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT id, voucher_id, account_code, account_name, debit, credit,
			COALESCE(remark,''), sort_no FROM voucher_entries WHERE voucher_id=? ORDER BY sort_no`, id)
	if err != nil {
		return Voucher{}, fmt.Errorf("查询分录失败：%v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e VoucherEntry
		if err := rows.Scan(&e.ID, &e.VoucherID, &e.AccountCode, &e.AccountName,
			&e.Debit, &e.Credit, &e.Remark, &e.SortNo); err != nil {
			return Voucher{}, fmt.Errorf("读取分录失败：%v", err)
		}
		v.Entries = append(v.Entries, e)
	}
	return v, rows.Err()
}

// Audit 审核凭证（draft → audited）。
func (s *VoucherService) Audit(ctx context.Context, id string, operator string) (Voucher, error) {
	return s.setStatus(ctx, id, "draft", "audited", "audit", operator)
}

// UnAudit 反审核（audited → draft，留日志）。
func (s *VoucherService) UnAudit(ctx context.Context, id string, operator string) (Voucher, error) {
	return s.setStatus(ctx, id, "audited", "draft", "unaudit", operator)
}

func (s *VoucherService) setStatus(ctx context.Context, id, from, to, action, operator string) (Voucher, error) {
	var out Voucher
	err := s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		var cur string
		if err := tx.QueryRow(`SELECT status FROM vouchers WHERE id=?`, id).Scan(&cur); err == sql.ErrNoRows {
			return fmt.Errorf("凭证不存在：%s", id)
		} else if err != nil {
			return fmt.Errorf("读取凭证失败：%v", err)
		}
		if cur != from {
			return fmt.Errorf("凭证状态不允许该操作，当前：%s", cur)
		}
		if _, err := tx.Exec(`UPDATE vouchers SET status=? WHERE id=?`, to, id); err != nil {
			return fmt.Errorf("更新凭证状态失败：%v", err)
		}
		return store.WriteAudit(tx, "voucher", id, action,
			`{"status":"`+cur+`"}`, `{"status":"`+to+`"}`, operator, "")
	})
	if err != nil {
		return Voucher{}, err
	}
	out.ID, out.Status = id, to
	return out, nil
}
