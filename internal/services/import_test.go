package services

import (
	"strings"
	"testing"
)

// TestDetectSourceType 文件名自动识别类型规则。
// 业务依据：导入中心 F1 —— 按文件名自动匹配 银行流水/订单流水/数据汇总。
func TestDetectSourceType(t *testing.T) {
	cases := []struct {
		name     string
		fileName string
		want     string
		wantErr  bool
	}{
		{"银行关键字", "银行流水.tsv", "bank_flow", false},
		{"银行英文", "bank_flow_2026-07.tsv", "bank_flow", false},
		{"订单关键字", "订单流水.tsv", "order_flow", false},
		{"订单英文", "order_flow_2026-07.tsv", "order_flow", false},
		{"POS 子集", "pos流水.tsv", "order_flow", false},
		{"汇总提示错误", "数据汇总.tsv", "", true},
		{"未知类型", "对账单.tsv", "", true},
		{"空文件名", "", "", true},
	}
	for _, c := range cases {
		got, err := DetectSourceType(c.fileName)
		if (err != nil) != c.wantErr {
			t.Errorf("DetectSourceType(%q) err=%v, wantErr=%v", c.fileName, err, c.wantErr)
			continue
		}
		if got != c.want {
			t.Errorf("DetectSourceType(%q) = %q, want %q", c.fileName, got, c.want)
		}
	}
}

// TestParseTabularText BOM 去除与表头/数据行拆分。
func TestParseTabularText(t *testing.T) {
	content := "\uFEFF交易时间\t转入金额\t余额\n2026-07-10 02:45:36\t11,970.00\t657,872.62\n"
	header, rows, err := parseTabularText(content)
	if err != nil {
		t.Fatalf("parseTabularText: %v", err)
	}
	if len(header) != 3 || strings.TrimPrefix(header[0], "\uFEFF") != "交易时间" {
		t.Errorf("表头未去 BOM 或列数不对：%v", header)
	}
	if len(rows) != 1 {
		t.Fatalf("数据行数 = %d, want 1", len(rows))
	}
	if rows[0][1] != "11,970.00" {
		t.Errorf("金额列 = %q, want 11,970.00", rows[0][1])
	}
}
