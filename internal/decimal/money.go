package decimal

import (
	"strings"

	"github.com/shopspring/decimal"
)

// 金额统一封装：shopspring/decimal 端到端，SQLite 存 TEXT，展示保留 2 位。
// 业务依据：docs/business/00-overview-health-finance-flow.md §6.2 / §8。

// Parse 解析用户输入 / TSV 单元格金额。
// 去千分位逗号、去首尾空格、去 ￥¥元 符号；空串按 0 处理。
func Parse(s string) (decimal.Decimal, error) {
	t := strings.TrimSpace(s)
	t = strings.ReplaceAll(t, ",", "")
	t = strings.TrimPrefix(t, "￥")
	t = strings.TrimPrefix(t, "¥")
	t = strings.TrimSuffix(t, "元")
	t = strings.TrimSpace(t)
	if t == "" {
		return decimal.Zero, nil
	}
	return decimal.NewFromString(t)
}

// MustParse 在种子数据等确定合法场景使用；非法则 panic。
func MustParse(s string) decimal.Decimal {
	d, err := Parse(s)
	if err != nil {
		panic("decimal.MustParse: " + s + ": " + err.Error())
	}
	return d
}

// Format 展示用：保留 2 位小数。
func Format(d decimal.Decimal) string {
	return d.StringFixed(2)
}

// Add/Sub/Cmp 纯透传，保证 Service 层不直接碰 float64。
func Add(a, b decimal.Decimal) decimal.Decimal { return a.Add(b) }
func Sub(a, b decimal.Decimal) decimal.Decimal { return a.Sub(b) }
func Equal(a, b decimal.Decimal) bool          { return a.Equal(b) }
func Cmp(a, b decimal.Decimal) int             { return a.Cmp(b) }

// FeeByRate 按费率预填手续费：orderAmount × rate，四舍五入保留 2 位。
// rate 为小数形式，如借记卡 0.005、贷记卡 0.006、微信/支付宝 0.0025。
func FeeByRate(orderAmount, rate decimal.Decimal) decimal.Decimal {
	return orderAmount.Mul(rate).Round(2)
}

// FeeRateForPayMethod 支付方式到费率的映射（与 fee_rates 表种子一致）。
// payMethod 取值：debit_card / credit_card / wechat / alipay。
// 汇总表借记/贷记列常为空（都走微信/支付宝通道），费率判断以订单流水为准。
func FeeRateForPayMethod(payMethod string) decimal.Decimal {
	switch strings.ToLower(strings.TrimSpace(payMethod)) {
	case "debit_card", "借记卡":
		return decimal.NewFromFloat(0.005)
	case "credit_card", "贷记卡":
		return decimal.NewFromFloat(0.006)
	case "wechat", "微信", "alipay", "支付宝":
		return decimal.NewFromFloat(0.0025)
	default:
		return decimal.NewFromFloat(0.0025)
	}
}

// WithinTolerance 判断 |a-b| <= tol，用于手续费 ±0.01 标异等场景。
func WithinTolerance(a, b, tol decimal.Decimal) bool {
	diff := a.Sub(b).Abs()
	return diff.LessThanOrEqual(tol)
}

// RatePercent 费率转展示用百分数：0.0025 → "0.25%"。
func RatePercent(rate decimal.Decimal) string {
	return rate.Mul(decimal.NewFromInt(100)).String() + "%"
}
