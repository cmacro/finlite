package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"finlite/internal/decimal"
	"finlite/internal/store"

	sdec "github.com/shopspring/decimal"
)

// OrderFlowService 订单流水：列表 / 费率复算。
// 原始表只读，不提供任何写接口；费率与金额恒等式校验在 Go decimal 中完成。
type OrderFlowService struct {
	DB *store.DB
}

func NewOrderFlowService(db *store.DB) *OrderFlowService { return &OrderFlowService{DB: db} }

// feeTolerance 手续费/实收恒等式允许的舍入误差 ±0.01（业务文档 §6.2）。
var feeTolerance = sdec.New(1, -2)

// List 订单流水：按结算日期（T+1 收入归属日）归集，一次性返回全部明细（不分页）。
// feeFlag 标出费率异常：fee_diff 费率差异（黄）、net_diff 实收差异（红）、fee_parse_err 解析失败（红）。
func (s *OrderFlowService) List(ctx context.Context, f OrderFlowFilter) (OrderFlowPage, error) {
	var where []string
	var args []any
	if d := strings.TrimSpace(f.SettleDate); d != "" {
		where = append(where, "o.settle_date=?")
		args = append(args, d)
	}
	if kw := strings.TrimSpace(f.Keyword); kw != "" {
		where = append(where, "(o.icbc_order_no LIKE ? OR COALESCE(o.third_order_no,'') LIKE ?)")
		like := "%" + kw + "%"
		args = append(args, like, like)
	}
	w := ""
	if len(where) > 0 {
		w = " WHERE " + strings.Join(where, " AND ")
	}

	rows, err := s.DB.Pool().QueryContext(ctx, `
		SELECT o.id, o.batch_id, o.icbc_order_no, COALESCE(o.third_order_no,''), o.txn_time, o.settle_date,
			o.order_amount, o.fee_amount, o.net_amount,
			COALESCE(o.pay_method,''), COALESCE(o.debit_credit_flag,''), o.created_at,
			COALESCE(o.raw_json,'{}'),
			COALESCE(b.file_name,''), COALESCE(b.imported_at,'')
		FROM order_flows o
		LEFT JOIN import_batches b ON b.id = o.batch_id`+w+`
		ORDER BY o.settle_date, o.txn_time, o.icbc_order_no`, args...)
	if err != nil {
		return OrderFlowPage{}, fmt.Errorf("查询订单流水失败：%v", err)
	}
	defer rows.Close()

	onlyAdj := strings.TrimSpace(f.OnlyFeeAdj) == "1"

	var (
		out      = OrderFlowPage{Groups: []OrderFlowGroup{}}
		orderSum = sdec.Zero
		feeSum   = sdec.Zero
		netSum   = sdec.Zero
	)
	for rows.Next() {
		var o OrderFlow
		if err := rows.Scan(&o.ID, &o.BatchID, &o.IcbcOrderNo, &o.ThirdOrderNo,
			&o.TxnTime, &o.SettleDate, &o.OrderAmount, &o.FeeAmount, &o.NetAmount,
			&o.PayMethod, &o.DebitCreditFlag, &o.CreatedAt, &o.RawJSON,
			&o.FileName, &o.ImportedAt); err != nil {
			return OrderFlowPage{}, fmt.Errorf("读取订单流水失败：%v", err)
		}
		if o.FileName == "" || o.SourceRowNo == 0 {
			if file, rowNo := sourceOf(o.RawJSON); file != "" || rowNo > 0 {
				o.SourceRowNo = rowNo
				if o.FileName == "" {
					o.FileName = file
				}
			}
		}
		o = analyzeOrderFlow(o)
		if onlyAdj && o.FeeFlag == "" {
			continue
		}

		out.Total++
		switch o.FeeFlag {
		case "fee_diff":
			out.FeeDiffCount++
			out.AnomalyCount++
		case "net_diff":
			out.NetDiffCount++
			out.AnomalyCount++
		case "fee_parse_err":
			out.ParseErrCount++
			out.AnomalyCount++
		}

		amt, errA := decimal.Parse(o.OrderAmount)
		fee, errF := decimal.Parse(o.FeeAmount)
		net, errN := decimal.Parse(o.NetAmount)
		if errA != nil {
			amt = sdec.Zero
		}
		if errF != nil {
			fee = sdec.Zero
		}
		if errN != nil {
			net = sdec.Zero
		}
		orderSum = orderSum.Add(amt)
		if o.FeeFlag != "fee_parse_err" {
			feeSum = feeSum.Add(fee)
		}
		netSum = netSum.Add(net)

		n := len(out.Groups)
		if n == 0 || out.Groups[n-1].SettleDate != o.SettleDate {
			out.Groups = append(out.Groups, OrderFlowGroup{
				SettleDate: o.SettleDate,
				Items:      []OrderFlow{},
			})
			n++
		}
		g := &out.Groups[n-1]
		g.Items = append(g.Items, o)
		g.Count++
		g.OrderTotal = decimal.Format(decimal.MustParse(g.OrderTotal).Add(amt))
		g.NetTotal = decimal.Format(decimal.MustParse(g.NetTotal).Add(net))
		if o.FeeFlag == "fee_parse_err" {
			// 解析失败的手续费不计入小计（页面提示"解析失败未计入手续费小计"）。
			g.ParseErrCount++
		} else {
			g.FeeTotal = decimal.Format(decimal.MustParse(g.FeeTotal).Add(fee))
		}
		if o.FeeFlag != "" {
			g.AnomalyCount++
		}
	}
	if err := rows.Err(); err != nil {
		return OrderFlowPage{}, fmt.Errorf("读取订单流水失败：%v", err)
	}

	out.OrderTotal = decimal.Format(orderSum)
	out.FeeTotal = decimal.Format(feeSum)
	out.NetTotal = decimal.Format(netSum)
	out.ReconDiff = decimal.Format(orderSum.Sub(feeSum).Sub(netSum))
	return out, nil
}

// analyzeOrderFlow 复算费率并填充展示字段：
// expected = order × rate（借记卡 0.5% / 贷记卡 0.6% / 微信·支付宝 0.25%），差额超 ±0.01 标 fee_diff；
// 订单金额 − 手续费 ≠ 实收（超 ±0.01）标 net_diff；金额无法解析标 fee_parse_err。
func analyzeOrderFlow(o OrderFlow) OrderFlow {
	method := o.PayMethod
	if strings.TrimSpace(method) == "" {
		method = o.DebitCreditFlag
	}
	rate := decimal.FeeRateForPayMethod(method)
	o.FeeRate = decimal.RatePercent(rate)

	amt, errA := decimal.Parse(o.OrderAmount)
	fee, errF := decimal.Parse(o.FeeAmount)
	net, errN := decimal.Parse(o.NetAmount)
	if errA != nil || errF != nil {
		o.FeeFlag = "fee_parse_err"
		return o
	}

	expect := decimal.FeeByRate(amt, rate)
	o.ExpectedFee = decimal.Format(expect)
	o.ExpectedNet = decimal.Format(amt.Sub(fee))
	o.FeeDiff = decimal.Format(fee.Sub(expect))
	if !decimal.WithinTolerance(fee, expect, feeTolerance) {
		o.FeeFlag = "fee_diff"
	}
	if errN == nil {
		o.NetDiff = decimal.Format(amt.Sub(fee).Sub(net))
		if o.FeeFlag == "" && !decimal.WithinTolerance(amt.Sub(fee), net, feeTolerance) {
			o.FeeFlag = "net_diff"
		}
	}
	return o
}

// sourceOf 读取导入时写入 raw_json 的来源文件与行号。
// 导入侧 "row" 写成字符串，这里同时兼容数字，避免历史数据丢来源信息。
func sourceOf(raw string) (fileName string, rowNo int) {
	if strings.TrimSpace(raw) == "" {
		return "", 0
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return "", 0
	}
	if f, ok := meta["file"].(string); ok {
		fileName = f
	}
	switch v := meta["row"].(type) {
	case string:
		rowNo, _ = strconv.Atoi(v)
	case float64:
		rowNo = int(v)
	}
	return fileName, rowNo
}
