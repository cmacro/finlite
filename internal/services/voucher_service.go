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
	sdec "github.com/shopspring/decimal"
)

// VoucherService 凭证：新增 / 列表 / 详情 / 审核 / 反审核。
// 约束：借贷必平；仅 reviewed 汇总可生成；一汇总一凭证（业务文档 §6.5）。
type VoucherService struct {
	DB *store.DB
}

func NewVoucherService(db *store.DB) *VoucherService { return &VoucherService{DB: db} }

// BuildEntries 按业务文档 §6.5 五类模板，由已审核汇总生成凭证分录（借贷自动配平）。
// 返回的分录仅作草稿：前端可在保存前调整科目/金额，保存时仍走 Create 的配平校验。
func (s *VoucherService) BuildEntries(ctx context.Context, summaryID string) ([]VoucherEntry, error) {
	sm, err := s.getSummary(ctx, summaryID)
	if err != nil {
		return nil, err
	}
	if sm.Status != "reviewed" {
		return nil, fmt.Errorf("只有已审核汇总可生成凭证，当前状态：%s", sm.Status)
	}
	entries, err := templateEntries(sm)
	if err != nil {
		return nil, err
	}
	if err := checkBalanced(entries); err != nil {
		return nil, fmt.Errorf("模板生成的分录不平，请检查汇总金额：%v", err)
	}
	return entries, nil
}

// templateEntries 五类模板（业务文档 §6.5）。
// 收付方向由 一级(收入/支出) 与 项目 决定，金额取自汇总的收入/支出/手续费列。
func templateEntries(sm Summary) ([]VoucherEntry, error) {
	income, err := decimal.Parse(sm.IncomeAmount)
	if err != nil {
		return nil, fmt.Errorf("实际收入金额不合法：%s", sm.IncomeAmount)
	}
	expense, err := decimal.Parse(sm.ExpenseAmount)
	if err != nil {
		return nil, fmt.Errorf("支出金额不合法：%s", sm.ExpenseAmount)
	}
	fee, err := decimal.Parse(sm.FeeActual)
	if err != nil {
		return nil, fmt.Errorf("手续费金额不合法：%s", sm.FeeActual)
	}
	remark := summarizeReason(sm)
	entry := func(code, name, debit, credit string) VoucherEntry {
		return VoucherEntry{AccountCode: code, AccountName: name, Debit: debit, Credit: credit, Remark: remark}
	}
	zero := sdec.Zero.String()

	switch {
	// [POS收入] 借 银行存款-POS清算户(实收) + 借 财务费用-手续费 贷 主营业务收入-服务项目(订单金额)
	case sm.Category == "收入":
		gross := income.Add(fee)
		return []VoucherEntry{
			entry("1002", "银行存款-POS清算户", income.String(), zero),
			entry("6603", "财务费用-手续费", fee.String(), zero),
			entry("6001", "主营业务收入-"+serviceName(sm.Project), zero, gross.String()),
		}, nil

	// [产品成本] 借 主营业务成本-产品成本 贷 银行存款
	case sm.Category == "支出" && sm.Project == "产品成本":
		return []VoucherEntry{
			entry("6401", "主营业务成本-产品成本", expense.String(), zero),
			entry("1002", "银行存款", zero, expense.String()),
		}, nil

	// [护理/人力报销] 借 管理费用-职工福利费/护理成本 贷 银行存款
	case sm.Category == "支出" && sm.Project == "护理成本":
		return []VoucherEntry{
			entry("6602", "管理费用-职工福利费", expense.String(), zero),
			entry("1002", "银行存款", zero, expense.String()),
		}, nil

	// [手续费] 借 财务费用-手续费 贷 银行存款
	case sm.Category == "支出" && sm.Project == "手续费":
		return []VoucherEntry{
			entry("6603", "财务费用-手续费", expense.String(), zero),
			entry("1002", "银行存款", zero, expense.String()),
		}, nil

	// [房租水电] 借 管理费用-水电费/房租 贷 银行存款
	case sm.Category == "支出" && (sm.Project == "水电费" || sm.Project == "房租"):
		return []VoucherEntry{
			entry("6602", "管理费用-"+sm.Project, expense.String(), zero),
			entry("1002", "银行存款", zero, expense.String()),
		}, nil

	// 销售费用类（如广告投放/活动费）：借 销售费用 贷 银行存款
	case sm.Category == "支出" && sm.Project == "销售费用":
		return []VoucherEntry{
			entry("6601", "销售费用", expense.String(), zero),
			entry("1002", "银行存款", zero, expense.String()),
		}, nil

	default:
		return nil, fmt.Errorf("暂不支持的汇总类型，无法匹配 §6.5 凭证模板：一级=%s 项目=%s", sm.Category, sm.Project)
	}
}

// serviceName 把汇总项目名落到主营业务收入的辅助核算名（§6.6）。
func serviceName(project string) string {
	switch project {
	case "营业收入", "":
		return "营业收入"
	default:
		return project
	}
}

// summarizeReason 凭证摘要：事由优先，附收据号便于反查。
func summarizeReason(sm Summary) string {
	base := strings.TrimSpace(sm.Reason)
	if base == "" {
		base = strings.TrimSpace(sm.Project)
	}
	if no := strings.TrimSpace(sm.ReceiptNo); no != "" {
		return base + "（收据 " + no + "）"
	}
	return base
}

// getSummary 读取汇总（模板生成用，含金额与状态）。
func (s *VoucherService) getSummary(ctx context.Context, id string) (Summary, error) {
	var sm Summary
	err := s.DB.Pool().QueryRowContext(ctx,
		`SELECT id, date, COALESCE(receipt_no,''), category, project, COALESCE(reason,''),
			income_amount, expense_amount, fee_auto, fee_actual, fee_manual_adjusted,
			balance, COALESCE(remark,''), source, status, COALESCE(voucher_id,''),
			created_at, updated_at FROM data_summaries WHERE id=?`, id).
		Scan(&sm.ID, &sm.Date, &sm.ReceiptNo, &sm.Category, &sm.Project, &sm.Reason,
			&sm.IncomeAmount, &sm.ExpenseAmount, &sm.FeeAuto, &sm.FeeActual,
			&sm.FeeManualAdjusted, &sm.Balance, &sm.Remark, &sm.Source, &sm.Status,
			&sm.VoucherID, &sm.CreatedAt, &sm.UpdatedAt)
	if err == sql.ErrNoRows {
		return Summary{}, fmt.Errorf("汇总不存在：%s", id)
	}
	if err != nil {
		return Summary{}, fmt.Errorf("读取汇总失败：%v", err)
	}
	return sm, nil
}

// backfillSourceVoucherID 按汇总的收支口径把来源行关联到凭证（§10 反查链）。
//
//   - 收入类：当日 POS 清算入账的银行行 + 结算日 = 当日+1 的订单行（POS 入账 T+1，§6.1）；
//   - 支出类：当日转出金额等于汇总支出额的银行行。
//
// 只填 voucher_id 为空的，不覆盖已有归属；金额一律按 decimal 字符串比较。
func backfillSourceVoucherID(tx *sql.Tx, voucherID string, sm Summary) error {
	if sm.Category == "收入" {
		_, err := tx.Exec(`UPDATE bank_flows SET voucher_id=?
			WHERE voucher_id IS NULL AND substr(txn_time,1,10)=?
			  AND (counterparty LIKE '%POS净收%' OR (remark<>'' AND remark<>'000000000'))`,
			voucherID, sm.Date)
		if err != nil {
			return fmt.Errorf("回写银行流水凭证关联失败：%v", err)
		}
		next, err := nextDay(sm.Date)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE order_flows SET voucher_id=?
			WHERE voucher_id IS NULL AND settle_date=?`, voucherID, next)
		if err != nil {
			return fmt.Errorf("回写订单流水凭证关联失败：%v", err)
		}
		return nil
	}
	_, err := tx.Exec(`UPDATE bank_flows SET voucher_id=?
		WHERE voucher_id IS NULL AND substr(txn_time,1,10)=? AND out_amount=?`,
		voucherID, sm.Date, decOrZero(sm.ExpenseAmount))
	if err != nil {
		return fmt.Errorf("回写银行流水凭证关联失败：%v", err)
	}
	return nil
}

// nextDay 返回 YYYY-MM-DD 的次日，用于收入类订单的 T+1 结算日匹配。
func nextDay(date string) (string, error) {
	tm, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(date), time.Local)
	if err != nil {
		return "", fmt.Errorf("汇总日期格式不合法（需 YYYY-MM-DD）：%s", date)
	}
	return tm.AddDate(0, 0, 1).Format("2006-01-02"), nil
}

// fetchSummaryTx 事务内读取汇总（含金额与状态）。
func fetchSummaryTx(tx *sql.Tx, id string) (Summary, error) {
	var sm Summary
	err := tx.QueryRow(`SELECT id, date, COALESCE(receipt_no,''), category, project, COALESCE(reason,''),
		income_amount, expense_amount, fee_auto, fee_actual, fee_manual_adjusted,
		balance, COALESCE(remark,''), source, status, COALESCE(voucher_id,''),
		created_at, updated_at FROM data_summaries WHERE id=?`, id).
		Scan(&sm.ID, &sm.Date, &sm.ReceiptNo, &sm.Category, &sm.Project, &sm.Reason,
			&sm.IncomeAmount, &sm.ExpenseAmount, &sm.FeeAuto, &sm.FeeActual,
			&sm.FeeManualAdjusted, &sm.Balance, &sm.Remark, &sm.Source, &sm.Status,
			&sm.VoucherID, &sm.CreatedAt, &sm.UpdatedAt)
	if err != nil {
		return Summary{}, fmt.Errorf("读取汇总失败：%v", err)
	}
	return sm, nil
}

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
		sm, err := fetchSummaryTx(tx, req.SummaryID)
		if err != nil {
			return err
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
		// §10 反查链：凭证 → 汇总 → 银行/订单行 → 文件+行号。
		// 按汇总的收支口径精确关联，避免把当日无关行（其他支出/手续费）一并挂上：
		//  收入类：当日 POS 清算入账行 + 结算日=次日的订单行（POS 入账 T+1）
		//  支出类：当日转出金额与汇总支出相等的银行行
		if err := backfillSourceVoucherID(tx, id, sm); err != nil {
			return err
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
