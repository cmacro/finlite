package services

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"finlite/internal/decimal"
	"finlite/internal/store"

	sdec "github.com/shopspring/decimal"
)

// BankFlowService 银行流水：列表 / 批次汇总 / 确认 / 撤销确认 / 导出。
// 原始表只读，不提供 Update；确认只写 balance_confirmed_at（可撤销）。
type BankFlowService struct {
	DB *store.DB
}

func NewBankFlowService(db *store.DB) *BankFlowService { return &BankFlowService{DB: db} }

// pageArgs 其它 Service 的分页参数归一化（银行流水页本身不再分页）。
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

// flowWhere 把筛选条件拼成 SQL（列别名 r，指向带窗口函数的 base CTE）。
func flowWhere(f BankFlowFilter) (string, []any) {
	var where []string
	var args []any
	if f.BatchID != "" {
		where = append(where, "r.batch_id=?")
		args = append(args, f.BatchID)
	}
	if f.DateFrom != "" {
		where = append(where, "substr(r.txn_time,1,10)>=?")
		args = append(args, f.DateFrom)
	}
	if f.DateTo != "" {
		where = append(where, "substr(r.txn_time,1,10)<=?")
		args = append(args, f.DateTo)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		where = append(where, "(r.counterparty LIKE ? OR r.summary LIKE ? OR r.remark LIKE ?)")
		like := "%" + kw + "%"
		args = append(args, like, like, like)
	}
	if len(where) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

// queryFlowRows 按筛选取全部明细（不分页），再按导入时的余额衔接顺序补齐上行余额。
func (s *BankFlowService) queryFlowRows(ctx context.Context, f BankFlowFilter) ([]BankFlow, error) {
	w, args := flowWhere(f)
	rows, err := s.DB.Pool().QueryContext(ctx, `
		SELECT r.id, r.batch_id, r.file_name, r.row_no, r.txn_time,
			COALESCE(r.counterparty,''), COALESCE(r.summary,''), COALESCE(r.remark,''),
			r.in_amount, r.out_amount, r.balance, r.balance_status,
			COALESCE(r.balance_confirmed_at,''), r.created_at,
			COALESCE(r.raw_json,'{}'), COALESCE(r.voucher_id,''), COALESCE(v.voucher_no,'')
		FROM bank_flows r
		LEFT JOIN vouchers v ON v.id = r.voucher_id`+w+`
		ORDER BY r.txn_time, r.row_no`, args...)
	if err != nil {
		return nil, fmt.Errorf("查询银行流水失败：%v", err)
	}
	defer rows.Close()

	items := []BankFlow{}
	for rows.Next() {
		var b BankFlow
		if err := rows.Scan(&b.ID, &b.BatchID, &b.FileName, &b.RowNo, &b.TxnTime,
			&b.Counterparty, &b.Summary, &b.Remark, &b.InAmount, &b.OutAmount,
			&b.Balance, &b.BalanceStatus, &b.BalanceConfirmed, &b.CreatedAt,
			&b.RawJSON, &b.VoucherID, &b.VoucherNo); err != nil {
			return nil, fmt.Errorf("读取银行流水失败：%v", err)
		}
		enrichBankFlow(&b)
		items = append(items, b)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.attachBalanceChain(ctx, items); err != nil {
		return nil, err
	}
	return items, nil
}

// attachBalanceChain 用与导入完全一致的余额衔接算法（orderBankRows）重算批次内顺序，
// 补齐每行「上行余额」。同一交易秒内的多行在导入时会按余额可衔接关系重排，
// 若按 (txn_time,row_no) 直接取上一行，会与已入库的 balance_status 矛盾。
// 日期/关键字筛选不影响该重算：按批次取全量行，保证首行之外都有真实上行。
func (s *BankFlowService) attachBalanceChain(ctx context.Context, items []BankFlow) error {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var batchIDs []string
	for _, b := range items {
		if !seen[b.BatchID] {
			seen[b.BatchID] = true
			batchIDs = append(batchIDs, b.BatchID)
		}
	}
	args := make([]any, len(batchIDs))
	for i, id := range batchIDs {
		args[i] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batchIDs)), ",")
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT id, batch_id, row_no, txn_time, in_amount, out_amount, balance
		 FROM bank_flows WHERE batch_id IN (`+placeholders+`)
		 ORDER BY batch_id, txn_time, row_no`, args...)
	if err != nil {
		return fmt.Errorf("读取银行流水余额衔接失败：%v", err)
	}
	defer rows.Close()

	groups := map[string][]bankRow{}
	for rows.Next() {
		var b bankRow
		var batchID string
		var inAmt, outAmt, bal string
		if err := rows.Scan(&b.id, &batchID, &b.rowNo, &b.txnTime, &inAmt, &outAmt, &bal); err != nil {
			return fmt.Errorf("读取银行流水余额衔接失败：%v", err)
		}
		in, err1 := decimal.Parse(inAmt)
		out, err2 := decimal.Parse(outAmt)
		balance, err3 := decimal.Parse(bal)
		if err1 != nil || err2 != nil || err3 != nil {
			return fmt.Errorf("银行流水金额解析失败（第 %d 行）", b.rowNo)
		}
		b.inAmount, b.outAmount, b.balance = in, out, balance
		groups[batchID] = append(groups[batchID], b)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	type chain struct {
		prev string
		seq  int
	}
	derived := make(map[string]chain, len(items))
	for _, group := range groups {
		ordered := orderBankRows(group)
		var prev sdec.Decimal
		hasPrev := false
		for i := range ordered {
			r := &ordered[i]
			if hasPrev {
				derived[r.id] = chain{prev: decimal.Format(prev), seq: i}
			} else {
				derived[r.id] = chain{seq: i}
			}
			prev = r.balance
			hasPrev = true
		}
	}

	for i := range items {
		d := derived[items[i].ID]
		items[i].PrevBalance = d.prev
		enrichBankFlow(&items[i])
	}
	// 同一秒重排后的顺序才是真实衔接顺序：行序按 (交易时间, 批内顺序) 展示。
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].TxnTime != items[j].TxnTime {
			return items[i].TxnTime < items[j].TxnTime
		}
		di, dj := derived[items[i].ID].seq, derived[items[j].ID].seq
		if di != dj {
			return di < dj
		}
		return items[i].BatchID < items[j].BatchID
	})
	return nil
}

// enrichBankFlow 补齐展示派生字段（decimal 计算全部在此完成，前端只格式化）。
//   - 对方账号：从 raw_json 取（导入时保留的原始列）；
//   - 应到余额 / 差额：上行余额 + 转入 - 转出，严格相等，无容差；
//   - POS 清算：对方单位为待清算商户银行卡 POS 净收暂收销账户。
func enrichBankFlow(b *BankFlow) {
	var raw struct {
		CounterpartyAccount string `json:"counterpartyAccount"`
	}
	if b.RawJSON != "" {
		_ = json.Unmarshal([]byte(b.RawJSON), &raw)
	}
	b.CounterpartyAccount = strings.TrimSpace(raw.CounterpartyAccount)
	b.IsPOS = strings.Contains(strings.ToUpper(b.Counterparty), "POS")

	// displayStatus：只有「原本不平、已被人工确认」的行显示「已确认」，
	// 其余行沿用导入时校验结果（mismatch / ok）；首行 skip 由前端按无上行余额判定。
	if b.BalanceConfirmed != "" && b.BalanceStatus == "mismatch" {
		b.DisplayStatus = "confirmed"
	} else {
		b.DisplayStatus = b.BalanceStatus
	}

	if b.PrevBalance == "" {
		return
	}
	prev, err1 := decimal.Parse(b.PrevBalance)
	in, err2 := decimal.Parse(b.InAmount)
	out, err3 := decimal.Parse(b.OutAmount)
	bal, err4 := decimal.Parse(b.Balance)
	if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
		return
	}
	expected := prev.Add(in).Sub(out)
	b.ExpectedBalance = decimal.Format(expected)
	b.BalanceDiff = signedMoney(bal.Sub(expected))
}

// signedMoney 差额展示：正数带 +，负数带 -，零为 0.00。
func signedMoney(d sdec.Decimal) string {
	if d.IsZero() {
		return "0.00"
	}
	if d.IsNegative() {
		return decimal.Format(d)
	}
	return "+" + decimal.Format(d)
}

// List 银行流水分页列表（UI 不分页，一次返回全部明细 + 汇总统计）。
func (s *BankFlowService) List(ctx context.Context, f BankFlowFilter) (BankFlowPage, error) {
	items, err := s.queryFlowRows(ctx, f)
	if err != nil {
		return BankFlowPage{}, err
	}
	page := BankFlowPage{Items: items, Total: len(items)}
	totalIn := sdec.Zero
	totalOut := sdec.Zero
	for _, b := range items {
		if in, err := decimal.Parse(b.InAmount); err == nil {
			totalIn = totalIn.Add(in)
			if in.IsPositive() {
				page.InCount++
			}
		}
		if out, err := decimal.Parse(b.OutAmount); err == nil {
			totalOut = totalOut.Add(out)
			if out.IsPositive() {
				page.OutCount++
			}
		}
		switch bankRowState(b) {
		case "mismatch":
			page.MismatchCount++
		case "confirmed":
			page.ConfirmedCount++
		case "skip":
			page.SkipCount++
		}
	}
	page.TotalIn = decimal.Format(totalIn)
	page.TotalOut = decimal.Format(totalOut)
	return page, nil
}

// bankRowState 行展示状态：已确认 > 不平 > 无上行（跳过） > 正常。
// 只统计「原本不平且已人工确认」的行为 confirmed（enrichBankFlow 已计算 DisplayStatus）。
func bankRowState(b BankFlow) string {
	switch b.DisplayStatus {
	case "confirmed":
		return "confirmed"
	case "mismatch":
		return "mismatch"
	}
	if b.PrevBalance == "" {
		return "skip"
	}
	return "ok"
}

// Batches 银行流水批次列表（含每批余额不平行数 / 已确认行数），批次信息栏与筛选用。
func (s *BankFlowService) Batches(ctx context.Context) ([]BankFlowBatch, error) {
	rows, err := s.DB.Pool().QueryContext(ctx, `
		SELECT r.id, r.file_name, r.row_count, r.success_count, r.skip_count,
			COALESCE(r.imported_at,''),
			COALESCE(SUM(CASE WHEN f.balance_status='mismatch' AND f.balance_confirmed_at IS NULL THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN f.balance_status='mismatch' AND f.balance_confirmed_at IS NOT NULL THEN 1 ELSE 0 END), 0),
			COALESCE(MAX(f.balance_confirmed_at),'')
		FROM import_batches r
		LEFT JOIN bank_flows f ON f.batch_id = r.id
		WHERE r.source_type='bank_flow'
		GROUP BY r.id
		ORDER BY r.imported_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("查询银行流水批次失败：%v", err)
	}
	defer rows.Close()
	out := []BankFlowBatch{}
	for rows.Next() {
		var b BankFlowBatch
		if err := rows.Scan(&b.BatchID, &b.FileName, &b.RowCount, &b.SuccessCount,
			&b.SkipCount, &b.ImportedAt, &b.MismatchCount, &b.ConfirmedCount,
			&b.ConfirmedAt); err != nil {
			return nil, fmt.Errorf("读取银行流水批次失败：%v", err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ConfirmBatch 按批次人工确认余额（唯一消红路径），可撤销。
func (s *BankFlowService) ConfirmBatch(ctx context.Context, batchID string, operator string) (int64, error) {
	if strings.TrimSpace(batchID) == "" {
		return 0, fmt.Errorf("批次不能为空")
	}
	now := time.Now().Format(time.RFC3339)
	res, err := s.DB.Pool().ExecContext(ctx,
		`UPDATE bank_flows SET balance_confirmed_at=? WHERE batch_id=? AND balance_confirmed_at IS NULL`,
		now, batchID)
	if err != nil {
		return 0, fmt.Errorf("确认余额失败：%v", err)
	}
	n, _ := res.RowsAffected()
	_ = operator
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

// ExportTSV 按当前筛选导出 TSV（目标路径由前端保存对话框选择）。
// 金额保持数据库中的 decimal 字符串，不在导出时做任何二次计算。
func (s *BankFlowService) ExportTSV(ctx context.Context, path string, f BankFlowFilter) (int, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return 0, fmt.Errorf("请选择导出文件路径")
	}
	items, err := s.queryFlowRows(ctx, f)
	if err != nil {
		return 0, err
	}
	file, err := os.Create(path)
	if err != nil {
		return 0, fmt.Errorf("创建导出文件失败：%v", err)
	}
	defer file.Close()
	w := bufio.NewWriter(file)
	header := []string{"交易时间", "对方账号", "对方单位", "摘要/用途", "附言", "转入", "转出", "余额", "校验状态"}
	if _, err := w.WriteString(strings.Join(header, "\t") + "\n"); err != nil {
		return 0, fmt.Errorf("写入导出文件失败：%v", err)
	}
	for _, b := range items {
		row := []string{
			b.TxnTime, b.CounterpartyAccount, b.Counterparty, b.Summary, b.Remark,
			b.InAmount, b.OutAmount, b.Balance, bankStatusText(bankRowState(b)),
		}
		for i := range row {
			row[i] = strings.ReplaceAll(row[i], "\t", " ")
		}
		if _, err := w.WriteString(strings.Join(row, "\t") + "\n"); err != nil {
			return 0, fmt.Errorf("写入导出文件失败：%v", err)
		}
	}
	if err := w.Flush(); err != nil {
		return 0, fmt.Errorf("写入导出文件失败：%v", err)
	}
	return len(items), nil
}

func bankStatusText(state string) string {
	switch state {
	case "ok":
		return "正常"
	case "mismatch":
		return "不平"
	case "skip":
		return "跳过"
	case "confirmed":
		return "已确认"
	default:
		return state
	}
}
