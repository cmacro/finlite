package decimal

import (
	"testing"

	sdec "github.com/shopspring/decimal"
)

func TestParseStripsFormats(t *testing.T) {
	d, err := Parse("12,000.00")
	if err != nil {
		t.Fatal(err)
	}
	if !d.Equal(sdec.NewFromFloat(12000)) {
		t.Fatalf("parse = %s", d)
	}
	if got := Format(d); got != "12000.00" {
		t.Fatalf("format = %s", got)
	}
	if _, err := Parse("0.00"); err != nil {
		t.Fatal(err)
	}
	empty, err := Parse("")
	if err != nil || !empty.IsZero() {
		t.Fatalf("empty = %s, %v", empty, err)
	}
}

func TestFeeByRateWechat(t *testing.T) {
	// 样本：12000 → 30.00（微信 0.25%）
	fee := FeeByRate(MustParse("12000"), FeeRateForPayMethod("wechat"))
	if Format(fee) != "30.00" {
		t.Fatalf("fee = %s", Format(fee))
	}
}
