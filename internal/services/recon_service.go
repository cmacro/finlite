package services

import (
	"context"
	"fmt"
	"strings"

	"finlite/internal/decimal"
	"finlite/internal/store"

	sdec "github.com/shopspring/decimal"
)

// ReconService 对账中心：按结算日三方勾稽。
// Σ订单实收 == 银行POS转入 == Σ汇总实际收入（业务文档 §6.4）。
// 差异进待处理队列，永不阻塞其他日期。
type ReconService struct {
	DB *store.DB
}

func NewReconService(db *store.DB) *ReconService { return &ReconService{DB: db} }

// isPOSRow 附言为示例商户号或对方含 POS净收 即判 POS 清算（已脱敏比对）。
func isPOSRow(remark, counterparty string) bool {
	if strings.Contains(counterparty, "POS净收") {
		return true
	}
	r := strings.TrimSpace(remark)
	return r != "" && r != "000000000"
}

func sumDec(vals []string) sdec.Decimal {
	sum := sdec.Zero
	for _, v := range vals {
		if d, err := decimal.Parse(v); err == nil {
			sum = sum.Add(d)
		}
	}
	return sum
}

// ReconcileByDate 对单个结算日做三方勾稽。
func (s *ReconService) ReconcileByDate(ctx context.Context, date string) (ReconResult, error) {
	if strings.TrimSpace(date) == "" {
		return ReconResult{}, fmt.Errorf("对账日期不能为空")
	}
	// 1) 订单：settle_date = D 的 Σnet
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT net_amount FROM order_flows WHERE settle_date=?`, date)
	if err != nil {
		return ReconResult{}, fmt.Errorf("汇总订单实收失败：%v", err)
	}
	var orderVals []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return ReconResult{}, fmt.Errorf("读取订单实收失败：%v", err)
		}
		orderVals = append(orderVals, v)
	}
	rows.Close()
	orderNet := sumDec(orderVals)

	// 2) 银行：txn_time 日期 = D 且 POS 行的 Σin
	brows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT in_amount, remark, COALESCE(counterparty,'') FROM bank_flows WHERE substr(txn_time,1,10)=?`, date)
	if err != nil {
		return ReconResult{}, fmt.Errorf("汇总银行POS失败：%v", err)
	}
	bankPOS := sdec.Zero
	for brows.Next() {
		var inAmt, remark, counter string
		if err := brows.Scan(&inAmt, &remark, &counter); err != nil {
			brows.Close()
			return ReconResult{}, fmt.Errorf("读取银行流水失败：%v", err)
		}
		if !isPOSRow(remark, counter) {
			continue
		}
		if d, err := decimal.Parse(inAmt); err == nil {
			bankPOS = bankPOS.Add(d)
		}
	}
	brows.Close()

	// 3) 汇总：date = D 的收入行 Σincome（实际收入口径）。
	//    店面收款（source='collection'）经 F9 通道对账单独核对，不参与订单 POS 三方勾稽，
	//    否则现金/个人码收款会凭空拉高当日汇总收入、把本该匹配的日期误判为差异。
	srows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT income_amount FROM data_summaries WHERE date=? AND category='收入' AND source<>'collection'`, date)
	if err != nil {
		return ReconResult{}, fmt.Errorf("汇总数据汇总失败：%v", err)
	}
	var sumVals []string
	for srows.Next() {
		var v string
		if err := srows.Scan(&v); err != nil {
			srows.Close()
			return ReconResult{}, fmt.Errorf("读取数据汇总失败：%v", err)
		}
		sumVals = append(sumVals, v)
	}
	srows.Close()
	summaryActual := sumDec(sumVals)

	matched := orderNet.Equal(bankPOS) && (len(sumVals) == 0 || bankPOS.Equal(summaryActual))
	diff := orderNet.Sub(bankPOS)
	return ReconResult{
		Date:          date,
		OrderNet:      decimal.Format(orderNet),
		BankPOSIn:     decimal.Format(bankPOS),
		SummaryActual: decimal.Format(summaryActual),
		Matched:       matched,
		Diff:          decimal.Format(diff),
	}, nil
}

// ListDiffs 列出日期区间内未匹配的日期（待处理队列）。
// 日期集合取「订单结算日 ∪ 银行 POS 入账日」：银行侧独有日期（订单已导完但银行多出
// 一笔 POS 入账，或订单缺口）同样必须进队列，否则差异被静默吞掉（业务文档 §6.4）。
func (s *ReconService) ListDiffs(ctx context.Context, dateFrom, dateTo string) ([]ReconDiff, error) {
	q := `SELECT settle_date FROM order_flows WHERE 1=1`
	var args []any
	if dateFrom != "" {
		q += ` AND settle_date>=?`
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		q += ` AND settle_date<=?`
		args = append(args, dateTo)
	}
	q += ` UNION SELECT substr(txn_time,1,10) FROM bank_flows WHERE 1=1`
	if dateFrom != "" {
		q += ` AND substr(txn_time,1,10)>=?`
		args = append(args, dateFrom)
	}
	if dateTo != "" {
		q += ` AND substr(txn_time,1,10)<=?`
		args = append(args, dateTo)
	}
	q += ` ORDER BY 1`
	rows, err := s.DB.Pool().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("查询对账日期失败：%v", err)
	}
	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			rows.Close()
			return nil, fmt.Errorf("读取对账日期失败：%v", err)
		}
		if strings.TrimSpace(d) != "" {
			dates = append(dates, d)
		}
	}
	rows.Close()
	var out []ReconDiff
	for _, d := range dates {
		r, err := s.ReconcileByDate(ctx, d)
		if err != nil {
			return nil, err
		}
		if r.Matched {
			continue
		}
		out = append(out, buildDiff(r))
	}
	if out == nil {
		out = []ReconDiff{}
	}
	return out, nil
}

// buildDiff 按三方勾稽结果定位差异字段，供待处理队列展示。
func buildDiff(r ReconResult) ReconDiff {
	orderNet, _ := decimal.Parse(r.OrderNet)
	bankPOS, _ := decimal.Parse(r.BankPOSIn)
	summary, _ := decimal.Parse(r.SummaryActual)
	switch {
	case !orderNet.Equal(bankPOS):
		return ReconDiff{
			Date:   r.Date,
			Field:  "订单实收 vs 银行POS",
			Expect: r.OrderNet,
			Actual: r.BankPOSIn,
			Detail: "Σ订单实收与银行POS转入不一致，请定位原行（文件+行号）核对补贴/立减",
		}
	default:
		return ReconDiff{
			Date:   r.Date,
			Field:  "银行POS vs 汇总实际收入",
			Expect: r.BankPOSIn,
			Actual: r.SummaryActual,
			Detail: fmt.Sprintf("银行POS与汇总实际收入不一致（差 %s），请核对数据汇总页当日收入行",
				decimal.Format(bankPOS.Sub(summary))),
		}
	}
}
