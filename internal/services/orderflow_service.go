package services

import (
	"context"
	"fmt"
	"strings"

	"finlite/internal/decimal"
	"finlite/internal/store"

	sdec "github.com/shopspring/decimal"
)

// OrderFlowService 订单流水：列表 / 费率复算。
type OrderFlowService struct {
	DB *store.DB
}

func NewOrderFlowService(db *store.DB) *OrderFlowService { return &OrderFlowService{DB: db} }

// List 订单流水分页；feeFlag 标出手改/费率异常（fee_diff 黄标），±0.01 外标异。
func (s *OrderFlowService) List(ctx context.Context, f OrderFlowFilter) (OrderFlowPage, error) {
	page, size, offset := pageArgs(f.Page, f.PageSize)
	var where []string
	var args []any
	if f.SettleDate != "" {
		where = append(where, "settle_date=?")
		args = append(args, f.SettleDate)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		where = append(where, "(icbc_order_no LIKE ? OR third_order_no LIKE ?)")
		like := "%" + kw + "%"
		args = append(args, like, like)
	}
	w := ""
	if len(where) > 0 {
		w = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.DB.Pool().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM order_flows`+w, args...).Scan(&total); err != nil {
		return OrderFlowPage{}, fmt.Errorf("统计订单流水失败：%v", err)
	}
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT id, batch_id, icbc_order_no, COALESCE(third_order_no,''), txn_time, settle_date,
			order_amount, fee_amount, net_amount,
			COALESCE(pay_method,''), COALESCE(debit_credit_flag,''), created_at
		 FROM order_flows`+w+` ORDER BY settle_date, txn_time LIMIT ? OFFSET ?`,
		append(args, size, offset)...)
	if err != nil {
		return OrderFlowPage{}, fmt.Errorf("查询订单流水失败：%v", err)
	}
	defer rows.Close()
	var items []OrderFlow
	for rows.Next() {
		var o OrderFlow
		if err := rows.Scan(&o.ID, &o.BatchID, &o.IcbcOrderNo, &o.ThirdOrderNo,
			&o.TxnTime, &o.SettleDate, &o.OrderAmount, &o.FeeAmount, &o.NetAmount,
			&o.PayMethod, &o.DebitCreditFlag, &o.CreatedAt); err != nil {
			return OrderFlowPage{}, fmt.Errorf("读取订单流水失败：%v", err)
		}
		o.FeeFlag = feeFlagOf(o)
		if f.OnlyFeeAdj == "1" && o.FeeFlag == "" {
			continue
		}
		items = append(items, o)
	}
	if items == nil {
		items = []OrderFlow{}
	}
	return OrderFlowPage{Items: items, Total: total, Page: page, PageSize: size}, rows.Err()
}

// feeFlagOf 复算手续费：expected = order × rate，差 > 0.01 标 fee_diff。
func feeFlagOf(o OrderFlow) string {
	amt, err1 := decimal.Parse(o.OrderAmount)
	fee, err2 := decimal.Parse(o.FeeAmount)
	if err1 != nil || err2 != nil {
		return "fee_parse_err"
	}
	method := o.PayMethod
	if method == "" {
		method = o.DebitCreditFlag
	}
	expect := decimal.FeeByRate(amt, decimal.FeeRateForPayMethod(method))
	if !decimal.WithinTolerance(fee, expect, sdec.NewFromFloat(0.01)) {
		return "fee_diff"
	}
	// 恒等式抽查：订单金额 - 手续费 = 实收（允许 ±0.01）
	net, err3 := decimal.Parse(o.NetAmount)
	if err3 == nil && !decimal.WithinTolerance(amt.Sub(fee), net, sdec.NewFromFloat(0.01)) {
		return "net_diff"
	}
	return ""
}
