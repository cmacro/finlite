package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSmokeImportSamplesWithEvidence 用 docs/raw 样本走完整导入链路，确认证据层不影响业务结果。
func TestSmokeImportSamplesWithEvidence(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	svc := NewImportService(db)

	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join("..", "..", "docs", "raw", name))
		if err != nil {
			t.Fatalf("读取样本 %s 失败：%v", name, err)
		}
		return string(b)
	}

	bank, err := svc.ImportFile(ctx, "银行流水.tsv", read("银行流水.tsv"))
	if err != nil {
		t.Fatalf("银行流水导入失败：%v", err)
	}
	order, err := svc.ImportFile(ctx, "订单流水.tsv", read("订单流水.tsv"))
	if err != nil {
		t.Fatalf("订单流水导入失败：%v", err)
	}
	t.Logf("银行：%d 行，成功 %d，跳过 %d，警告 %q，指纹 %s", bank.RowCount, bank.SuccessCount, bank.SkipCount, bank.BalanceWarning, bank.RawSHA256[:8])
	t.Logf("订单：%d 行，成功 %d，跳过 %d，指纹 %s", order.RowCount, order.SuccessCount, order.SkipCount, order.RawSHA256[:8])

	files, err := db.ListRawFiles(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		t.Logf("证据：%s → %s（%d 行，%d 字节，%s）", f.OrigName, f.RelPath, f.RowCount, f.SizeBytes, f.Status)
	}
	if len(files) != 2 {
		t.Fatalf("原始文件条数 = %d，want 2", len(files))
	}
	report, err := db.VerifyRawFiles(ctx)
	if err != nil || report.OK != 2 || len(report.Issues) != 0 {
		t.Fatalf("校验异常：%+v, %v", report, err)
	}
}
