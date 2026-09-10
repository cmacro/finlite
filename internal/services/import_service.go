package services

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"finlite/internal/decimal"
	"finlite/internal/store"

	"github.com/google/uuid"
	sdec "github.com/shopspring/decimal"
)

// ImportService 解析 TSV/CSV、去重、余额校验、写 batch+errors。
// 业务依据：业务文档 §4 / §6.3 / 附录 A。原始表只读，错行靠重导。
type ImportService struct {
	DB *store.DB
}

func NewImportService(db *store.DB) *ImportService { return &ImportService{DB: db} }

func readTSVRows(filePath string) (header []string, rows [][]string, err error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("打不开文件：%v", err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma = '\t'
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	all, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("解析 TSV 失败：%v", err)
	}
	// 去 BOM
	if len(all) > 0 && len(all[0]) > 0 {
		all[0][0] = strings.TrimPrefix(all[0][0], "\uFEFF")
	}
	if len(all) == 0 {
		return nil, nil, fmt.Errorf("空文件：%s", filepath.Base(filePath))
	}
	return all[0], all[1:], nil
}

func colIdx(header []string, names ...string) int {
	for i, h := range header {
		h = strings.TrimSpace(h)
		for _, n := range names {
			if h == n {
				return i
			}
		}
	}
	return -1
}

func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

// isSummaryRateRow 汇总文件第 2 行为费率声明行（首列为空且含 0.005），导入时跳过。
func isSummaryRateRow(row []string) bool {
	joined := strings.Join(row, " ")
	return strings.TrimSpace(cell(row, 0)) == "" &&
		(strings.Contains(joined, "0.005") || strings.Contains(joined, "0.006"))
}

// ImportBankFile 导入银行流水 TSV。去重键 (txn_time,in,out,counterparty)。
func (s *ImportService) ImportBankFile(ctx context.Context, filePath string) (ImportResult, error) {
	header, rows, err := readTSVRows(filePath)
	if err != nil {
		return ImportResult{}, err
	}
	iTxn := colIdx(header, "交易时间")
	iCounter := colIdx(header, "对方单位")
	iSummary := colIdx(header, "用途", "摘要", "用途/摘要")
	iRemark := colIdx(header, "附言")
	iIn := colIdx(header, "转入金额")
	iOut := colIdx(header, "转出金额")
	iBal := colIdx(header, "余额")
	if iTxn < 0 || iIn < 0 || iOut < 0 || iBal < 0 {
		return ImportResult{}, fmt.Errorf("银行流水表头不匹配：需要 交易时间/转入金额/转出金额/余额 列")
	}

	batchID := uuid.NewString()
	fileName := filepath.Base(filePath)
	now := time.Now().Format(time.RFC3339)
	res := ImportResult{BatchID: batchID, FileName: fileName, RowCount: len(rows)}

	var prevBal *sdec.Decimal
	var firstBal, lastBal *sdec.Decimal
	var sumIn, sumOut = sdec.Zero, sdec.Zero
	var balanceWarn strings.Builder

	err = s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		for idx, row := range rows {
			rowNo := idx + 2 // 含表头行号
			txnTime := cell(row, iTxn)
			counter := cell(row, iCounter)
			summary := cell(row, iSummary)
			remark := cell(row, iRemark)
			inAmt, err1 := decimal.Parse(cell(row, iIn))
			outAmt, err2 := decimal.Parse(cell(row, iOut))
			bal, err3 := decimal.Parse(cell(row, iBal))
			if err1 != nil || err2 != nil || err3 != nil {
				insertErr(tx, batchID, rowNo, "amount",
					fmt.Sprintf("金额解析失败：转入[%s] 转出[%s] 余额[%s]", cell(row, iIn), cell(row, iOut), cell(row, iBal)))
				continue
			}
			if txnTime == "" {
				insertErr(tx, batchID, rowNo, "txn_time", "交易时间为空，已跳过")
				continue
			}
			// 去重
			var dup int
			_ = tx.QueryRow(`SELECT COUNT(*) FROM bank_flows
				WHERE txn_time=? AND in_amount=? AND out_amount=? AND counterparty=?`,
				txnTime, inAmt.String(), outAmt.String(), counter).Scan(&dup)
			if dup > 0 {
				res.SkipCount++
				continue
			}
			// 逐行余额校验：balance == prev + in - out（严格相等，无容差）
			status := "skip"
			if prevBal != nil {
				expect := prevBal.Add(inAmt).Sub(outAmt)
				if expect.Equal(bal) {
					status = "ok"
				} else {
					status = "mismatch"
					insertErr(tx, batchID, rowNo, "balance_discontinuity",
						fmt.Sprintf("余额不连续：上行 %s +转入 %s -转出 %s ≠ 本行 %s",
							decimal.Format(*prevBal), decimal.Format(inAmt), decimal.Format(outAmt), decimal.Format(bal)))
				}
			} else {
				status = "ok"
				b := bal
				firstBal = &b
			}
			lb := bal
			lastBal = &lb
			pb := bal
			prevBal = &pb
			sumIn = sumIn.Add(inAmt)
			sumOut = sumOut.Add(outAmt)

			id := uuid.NewString()
			if _, err := tx.Exec(`INSERT INTO bank_flows
				(id, batch_id, file_name, row_no, txn_time, counterparty, summary, remark,
				 in_amount, out_amount, balance, balance_status, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, batchID, fileName, rowNo, txnTime, counter, summary, remark,
				inAmt.String(), outAmt.String(), bal.String(), status, now,
			); err != nil {
				return fmt.Errorf("写入银行流水失败（第 %d 行）：%v", rowNo, err)
			}
			res.SuccessCount++
		}
		// 整批校验：last - first == Σin - Σout
		if firstBal != nil && lastBal != nil {
			if !lastBal.Sub(*firstBal).Equal(sumIn.Sub(sumOut)) {
				balanceWarn.WriteString(fmt.Sprintf("整批不平：末余额 %s - 首余额 %s ≠ Σ转入 %s - Σ转出 %s",
					decimal.Format(*lastBal), decimal.Format(*firstBal),
					decimal.Format(sumIn), decimal.Format(sumOut)))
			}
		}
		res.BalanceWarning = balanceWarn.String()
		_, err := tx.Exec(`INSERT INTO import_batches
			(id, source_type, file_name, row_count, success_count, skip_count, balance_warning, imported_at)
			VALUES (?, 'bank_flow', ?, ?, ?, ?, ?, ?)`,
			batchID, fileName, res.RowCount, res.SuccessCount, res.SkipCount,
			nullStr(res.BalanceWarning), now)
		return err
	})
	if err != nil {
		return ImportResult{}, err
	}
	return res, nil
}

// ImportOrderFile 导入订单流水 TSV。去重键 icbc_order_no UNIQUE。
func (s *ImportService) ImportOrderFile(ctx context.Context, filePath string) (ImportResult, error) {
	header, rows, err := readTSVRows(filePath)
	if err != nil {
		return ImportResult{}, err
	}
	iOrder := colIdx(header, "工行订单号", "订单号")
	iThird := colIdx(header, "第三方订单号")
	iTxn := colIdx(header, "交易时间")
	iSettle := colIdx(header, "结算日期")
	iAmt := colIdx(header, "订单金额")
	iFee := colIdx(header, "手续费金额", "手续费")
	iNet := colIdx(header, "商户实收金额", "实收金额")
	iPay := colIdx(header, "支付方式")
	iFlag := colIdx(header, "借贷记", "卡种", "借贷记标识")
	if iOrder < 0 || iSettle < 0 || iAmt < 0 || iNet < 0 {
		return ImportResult{}, fmt.Errorf("订单流水表头不匹配：需要 工行订单号/结算日期/订单金额/实收金额 列")
	}

	batchID := uuid.NewString()
	fileName := filepath.Base(filePath)
	now := time.Now().Format(time.RFC3339)
	res := ImportResult{BatchID: batchID, FileName: fileName, RowCount: len(rows)}

	err = s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		for idx, row := range rows {
			rowNo := idx + 2
			orderNo := cell(row, iOrder)
			if orderNo == "" {
				insertErr(tx, batchID, rowNo, "icbc_order_no", "工行订单号为空，已跳过")
				continue
			}
			settle := cell(row, iSettle)
			if settle == "" {
				// 结算日期为空则用交易日期+1（T+1）并记待确认
				if t := cell(row, iTxn); len(t) >= 10 {
					if tm, e := time.Parse("2006-01-02 15:04:05", t); e == nil {
						settle = tm.AddDate(0, 0, 1).Format("2006-01-02")
					} else if tm, e := time.Parse("2006-01-02", t[:10]); e == nil {
						settle = tm.AddDate(0, 0, 1).Format("2006-01-02")
					}
				}
				if settle == "" {
					insertErr(tx, batchID, rowNo, "settle_date", "结算日期为空且无法按T+1推导，已跳过")
					continue
				}
			}
			amt, e1 := decimal.Parse(cell(row, iAmt))
			fee, _ := decimal.Parse(cell(row, iFee))
			net, e2 := decimal.Parse(cell(row, iNet))
			if e1 != nil || e2 != nil {
				insertErr(tx, batchID, rowNo, "amount", "金额解析失败，已跳过")
				continue
			}
			raw, _ := json.Marshal(map[string]string{"file": fileName, "row": fmt.Sprint(rowNo)})
			if _, err := tx.Exec(`INSERT OR IGNORE INTO order_flows
				(id, batch_id, icbc_order_no, third_order_no, txn_time, settle_date,
				 order_amount, fee_amount, net_amount, pay_method, debit_credit_flag, raw_json, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				uuid.NewString(), batchID, orderNo, cell(row, iThird), cell(row, iTxn), settle,
				amt.String(), fee.String(), net.String(), cell(row, iPay), cell(row, iFlag),
				string(raw), now,
			); err != nil {
				return fmt.Errorf("写入订单流水失败（第 %d 行）：%v", rowNo, err)
			}
			res.SuccessCount++
		}
		_, err := tx.Exec(`INSERT INTO import_batches
			(id, source_type, file_name, row_count, success_count, skip_count, balance_warning, imported_at)
			VALUES (?, 'order_flow', ?, ?, ?, ?, '', ?)`,
			batchID, fileName, res.RowCount, res.SuccessCount, res.SkipCount, now)
		return err
	})
	if err != nil {
		return ImportResult{}, err
	}
	// INSERT OR IGNORE 的重复行计入 skip
	res.SkipCount = res.RowCount - res.SuccessCount
	return res, nil
}

// ListBatches 导入批次列表（导入中心用）。
func (s *ImportService) ListBatches(ctx context.Context, sourceType string) ([]ImportResult, error) {
	q := `SELECT id, file_name, row_count, success_count, skip_count,
			COALESCE(balance_warning,'') FROM import_batches`
	var args []any
	if sourceType != "" {
		q += ` WHERE source_type=?`
		args = append(args, sourceType)
	}
	q += ` ORDER BY imported_at DESC LIMIT 200`
	rows, err := s.DB.Pool().QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("查询导入批次失败：%v", err)
	}
	defer rows.Close()
	var out []ImportResult
	for rows.Next() {
		var r ImportResult
		if err := rows.Scan(&r.BatchID, &r.FileName, &r.RowCount, &r.SuccessCount, &r.SkipCount, &r.BalanceWarning); err != nil {
			return nil, fmt.Errorf("读取导入批次失败：%v", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListErrors 某批次的错误明细。
func (s *ImportService) ListErrors(ctx context.Context, batchID string) ([]ImportError, error) {
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT id, batch_id, COALESCE(row_no,0), COALESCE(field_name,''), message
		 FROM import_errors WHERE batch_id=? ORDER BY row_no`, batchID)
	if err != nil {
		return nil, fmt.Errorf("查询导入错误失败：%v", err)
	}
	defer rows.Close()
	var out []ImportError
	for rows.Next() {
		var e ImportError
		if err := rows.Scan(&e.ID, &e.BatchID, &e.RowNo, &e.FieldName, &e.Message); err != nil {
			return nil, fmt.Errorf("读取导入错误失败：%v", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func insertErr(tx *sql.Tx, batchID string, rowNo int, field, msg string) {
	_, _ = tx.Exec(`INSERT INTO import_errors (id, batch_id, row_no, field_name, message)
		VALUES (?, ?, ?, ?, ?)`, uuid.NewString(), batchID, rowNo, field, msg)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
