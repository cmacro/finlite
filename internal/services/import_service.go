package services

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"finlite/internal/decimal"
	"finlite/internal/store"

	"github.com/google/uuid"
	sdec "github.com/shopspring/decimal"
)

// ImportService 解析 TSV/CSV/XLSX、去重、余额校验、写 batch+errors。
// 业务依据：业务文档 §4 / §6.3 / 附录 A。原始表只读，错行靠重导。
type ImportService struct {
	DB *store.DB
}

func NewImportService(db *store.DB) *ImportService { return &ImportService{DB: db} }

func readTSVRows(filePath string) (header []string, rows [][]string, err error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, nil, fmt.Errorf("打不开文件：%v", err)
	}
	header, rows, err = parseTabularText(string(data))
	if err != nil {
		return nil, nil, fmt.Errorf("%s：%v", filepath.Base(filePath), err)
	}
	return header, rows, nil
}

// detectDelimiter 按首行（表头）挑分隔符：制表符优先，其次逗号。
// 业务依据：附录 A「TSV/CSV 文本文件按首行检测分隔符」。
func detectDelimiter(content string) rune {
	first := content
	if i := strings.IndexByte(content, '\n'); i >= 0 {
		first = content[:i]
	}
	tabs := strings.Count(first, "\t")
	commas := strings.Count(first, ",")
	if tabs > 0 && tabs >= commas {
		return '\t'
	}
	if commas > 0 {
		return ','
	}
	return '\t'
}

// parseTabularText 解析 TSV/CSV 文本内容。前端用 <input type=file> 选文件并读内容后传入，
// Service 不依赖 wails 原生对话框，避免向 services 包引入 wails 依赖。
func parseTabularText(content string) (header []string, rows [][]string, err error) {
	r := csv.NewReader(strings.NewReader(content))
	r.Comma = detectDelimiter(content)
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	all, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("解析表格失败：%v", err)
	}
	// 去 BOM
	if len(all) > 0 && len(all[0]) > 0 {
		all[0][0] = strings.TrimPrefix(all[0][0], "\uFEFF")
	}
	if len(all) == 0 {
		return nil, nil, fmt.Errorf("空文件")
	}
	// 丢弃完全空白的尾行/尾列（Excel 另存 CSV 常见）
	all = trimBlankRows(all)
	if len(all) == 0 {
		return nil, nil, fmt.Errorf("空文件")
	}
	return all[0], all[1:], nil
}

func trimBlankRows(rows [][]string) [][]string {
	out := rows[:0]
	for _, r := range rows {
		blank := true
		for _, c := range r {
			if strings.TrimSpace(c) != "" {
				blank = false
				break
			}
		}
		if !blank {
			out = append(out, r)
		}
	}
	return out
}

// parseXLSXText 读取 XLSX 首个工作表，按 TSV 口径返回表头+数据行。
// 业务依据：附录 A「XLSX 取首个工作表，首行即表头」。
func parseXLSXText(content []byte) (header []string, rows [][]string, err error) {
	sheet, err := readFirstXLSXSheet(content)
	if err != nil {
		return nil, nil, fmt.Errorf("解析 XLSX 失败：%v", err)
	}
	if len(sheet) == 0 {
		return nil, nil, fmt.Errorf("空工作表")
	}
	sheet = trimBlankRows(sheet)
	if len(sheet) == 0 {
		return nil, nil, fmt.Errorf("空工作表")
	}
	return sheet[0], sheet[1:], nil
}

// DetectSourceType 按文件名关键字识别导入类型。
// 规则：含「银行」/ bank → bank_flow；含「订单」/ order / pos → order_flow；
// 含「汇总」/ summary → 数据汇总不在导入中心受理。
func DetectSourceType(fileName string) (string, error) {
	n := strings.ToLower(fileName)
	switch {
	case strings.Contains(n, "银行") || strings.Contains(n, "bank"):
		return "bank_flow", nil
	case strings.Contains(n, "订单") || strings.Contains(n, "order") || strings.Contains(n, "pos"):
		return "order_flow", nil
	case strings.Contains(n, "汇总") || strings.Contains(n, "summary"):
		return "", fmt.Errorf("数据汇总请走「数据汇总」页维护，导入中心不受理：%s", fileName)
	default:
		return "", fmt.Errorf("无法根据文件名识别类型（文件名需含「银行」「订单」或「汇总」关键字）：%s", fileName)
	}
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

// ParseAnyFile 解析文本/二进制内容为表头+数据行，供映射预览与导入共用。
func ParseAnyFile(fileName, content string) (header []string, rows [][]string, err error) {
	if isXLSXFileName(fileName) {
		return parseXLSXText([]byte(content))
	}
	return parseTabularText(content)
}

func isXLSXFileName(fileName string) bool {
	return strings.EqualFold(filepath.Ext(fileName), ".xlsx")
}

// bankColumnSpec 银行流水关键列（供预览展示识别结果）。
var bankColumnSpec = []string{"交易时间", "对方单位", "用途/摘要", "附言", "转入金额", "转出金额", "余额"}

// orderColumnSpec 订单流水关键列。
var orderColumnSpec = []string{"工行订单号", "交易时间", "结算日期", "订单金额", "手续费金额", "商户实收金额", "支付方式", "借贷记标识"}

// PreviewColumns 列映射预览：识别到的关键列、缺失列与前若干行样本。
// 业务依据：§7 F1「预览列映射」。
func (s *ImportService) PreviewColumns(ctx context.Context, fileName, content string) (ColumnMapping, error) {
	sourceType, err := DetectSourceType(fileName)
	if err != nil {
		return ColumnMapping{}, err
	}
	header, rows, err := ParseAnyFile(fileName, content)
	if err != nil {
		return ColumnMapping{}, err
	}
	spec := columnSpec(sourceType)
	var matched, missing []string
	for _, name := range spec {
		if colIdx(header, strings.Split(name, "/")...) >= 0 {
			matched = append(matched, name)
		} else {
			missing = append(missing, name)
		}
	}
	sample := rows
	if len(sample) > 5 {
		sample = sample[:5]
	}
	return ColumnMapping{
		SourceType: sourceType,
		FileName:   fileName,
		Header:     header,
		Matched:    matched,
		Missing:    missing,
		SampleRows: sample,
		TotalRows:  len(rows),
	}, nil
}

// ImportBankFile 导入银行流水文件。去重键 (txn_time,in,out,counterparty)。
func (s *ImportService) ImportBankFile(ctx context.Context, filePath string) (ImportResult, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ImportResult{}, fmt.Errorf("打不开文件：%v", err)
	}
	name := filepath.Base(filePath)
	header, rows, err := ParseAnyFile(name, string(data))
	if err != nil {
		return ImportResult{}, fmt.Errorf("%s：%v", name, err)
	}
	return s.importBankRows(ctx, name, header, rows)
}

// bankRow 解析后的银行流水行（未写库）。
type bankRow struct {
	rowNo        int
	txnTime      string
	counterparty string
	summary      string
	remark       string
	inAmount     sdec.Decimal
	outAmount    sdec.Decimal
	balance      sdec.Decimal
	rawJSON      string
	status       string // ok / mismatch，由 checkBankBalance 写入
}

// importBankRows 银行流水导入核心（文件入口与 ImportFile 统一入口共用）。
// 余额校验：按 txn_time 升序重排后再逐行/整批校验（业务文档 §6.3）。
func (s *ImportService) importBankRows(ctx context.Context, fileName string, header []string, rows [][]string) (ImportResult, error) {
	iAcct := colIdx(header, "对方账号")
	iTxn := colIdx(header, "交易时间")
	iCounter := colIdx(header, "对方单位")
	iPurpose := colIdx(header, "用途")
	iDigest := colIdx(header, "摘要")
	iSummary := colIdx(header, "用途/摘要")
	iRemark := colIdx(header, "附言")
	iIn := colIdx(header, "转入金额")
	iOut := colIdx(header, "转出金额")
	iBal := colIdx(header, "余额")
	if iTxn < 0 || iIn < 0 || iOut < 0 || iBal < 0 {
		return ImportResult{}, fmt.Errorf("银行流水表头不匹配：需要 交易时间/转入金额/转出金额/余额 列")
	}

	batchID := uuid.NewString()
	now := time.Now().Format(time.RFC3339)
	res := ImportResult{BatchID: batchID, SourceType: "bank_flow", FileName: fileName, RowCount: len(rows), ImportedAt: now}

	// 1) 解析全部行（解析失败/缺时间的记错跳过）
	type parseErr struct {
		rowNo int
		field string
		msg   string
	}
	var parsed []bankRow
	var errs []parseErr
	for idx, row := range rows {
		rowNo := idx + 2 // 含表头行号
		txnTime := cell(row, iTxn)
		if txnTime == "" {
			errs = append(errs, parseErr{rowNo, "txn_time", "交易时间为空，已跳过"})
			res.SkipCount++
			continue
		}
		inAmt, err1 := decimal.Parse(cell(row, iIn))
		outAmt, err2 := decimal.Parse(cell(row, iOut))
		bal, err3 := decimal.Parse(cell(row, iBal))
		if err1 != nil || err2 != nil || err3 != nil {
			errs = append(errs, parseErr{rowNo, "amount",
				fmt.Sprintf("金额解析失败：转入[%s] 转出[%s] 余额[%s]", cell(row, iIn), cell(row, iOut), cell(row, iBal))})
			res.SkipCount++
			continue
		}
		// 用途 + 摘要 合并存入 summary（附录 A）；两者同时存在时以「用途/摘要」列优先。
		summary := cell(row, iSummary)
		if summary == "" {
			summary = joinNonEmpty(cell(row, iPurpose), cell(row, iDigest))
		}
		raw, _ := json.Marshal(map[string]string{
			"file":                fileName,
			"row":                 fmt.Sprint(rowNo),
			"counterpartyAccount": cell(row, iAcct),
		})
		parsed = append(parsed, bankRow{
			rowNo: rowNo, txnTime: txnTime, counterparty: cell(row, iCounter),
			summary: summary, remark: cell(row, iRemark),
			inAmount: inAmt, outAmount: outAmt, balance: bal, rawJSON: string(raw),
		})
	}

	// 2) 排序 + 校验：先按交易时间升序；同一秒内多行（样本 7-10 13:39:14 的
	// 4,800/717/149）导出顺序不确定，按「上行余额 + 转入 - 转出 == 本行余额」的
	// 可衔接关系重排，使真实连续的行不被误判为不平（业务文档 §6.3）。
	parsed = orderBankRows(parsed)
	balanceWarn := checkBankBalance(parsed)
	for i := range parsed {
		parsed[i].status = balanceWarn.statusByRow[parsed[i].rowNo]
	}

	// 3) 写库：去重 + 落盘
	err := s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		for _, e := range errs {
			insertErr(tx, batchID, e.rowNo, e.field, e.msg)
		}
		for _, r := range parsed {
			var dup int
			_ = tx.QueryRow(`SELECT COUNT(*) FROM bank_flows
				WHERE txn_time=? AND in_amount=? AND out_amount=? AND counterparty=?`,
				r.txnTime, r.inAmount.String(), r.outAmount.String(), r.counterparty).Scan(&dup)
			if dup > 0 {
				res.SkipCount++
				continue
			}
			id := uuid.NewString()
			if _, err := tx.Exec(`INSERT INTO bank_flows
				(id, batch_id, file_name, row_no, txn_time, counterparty, summary, remark,
				 in_amount, out_amount, balance, balance_status, raw_json, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				id, batchID, fileName, r.rowNo, r.txnTime, r.counterparty, r.summary, r.remark,
				r.inAmount.String(), r.outAmount.String(), r.balance.String(), r.status, r.rawJSON, now,
			); err != nil {
				return fmt.Errorf("写入银行流水失败（第 %d 行）：%v", r.rowNo, err)
			}
			res.SuccessCount++
		}
		for _, b := range balanceWarn.breaks {
			insertErr(tx, batchID, b.rowNo, "balance_discontinuity", b.msg)
		}
		res.BalanceWarning = balanceWarn.batch
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

// bankBalanceResult 余额校验结果（逐行状态 + 整批警告）。
type bankBalanceResult struct {
	statusByRow map[int]string
	breaks      []struct {
		rowNo int
		msg   string
	}
	batch string
}

// checkBankBalance 对已按 txn_time 升序排好的行做逐行与整批校验（业务文档 §6.3）。
// 逐行：本行余额 == 上行余额 + 转入 - 转出（首行直接 ok，无上行可比）。
// 整批：末余额 - 首余额 == 自第 2 行起的 Σ(转入 - 转出)（首行净额已被首余额吸收）。
func checkBankBalance(parsed []bankRow) bankBalanceResult {
	out := bankBalanceResult{statusByRow: map[int]string{}}
	if len(parsed) == 0 {
		return out
	}
	var prev *sdec.Decimal
	sumNet := sdec.Zero // 自第 2 行起
	for i := range parsed {
		r := &parsed[i]
		if prev == nil {
			out.statusByRow[r.rowNo] = "ok"
		} else {
			expect := prev.Add(r.inAmount).Sub(r.outAmount)
			if expect.Equal(r.balance) {
				out.statusByRow[r.rowNo] = "ok"
			} else {
				out.statusByRow[r.rowNo] = "mismatch"
				out.breaks = append(out.breaks, struct {
					rowNo int
					msg   string
				}{r.rowNo, fmt.Sprintf("余额不连续：上行 %s +转入 %s -转出 %s ≠ 本行 %s",
					decimal.Format(*prev), decimal.Format(r.inAmount), decimal.Format(r.outAmount), decimal.Format(r.balance))})
			}
			sumNet = sumNet.Add(r.inAmount).Sub(r.outAmount)
		}
		b := r.balance
		prev = &b
	}
	first, last := parsed[0].balance, parsed[len(parsed)-1].balance
	if !last.Sub(first).Equal(sumNet) {
		out.batch = fmt.Sprintf("整批不平：末余额 %s - 首余额 %s ≠ Σ(转入-转出，首行除外) %s",
			decimal.Format(last), decimal.Format(first), decimal.Format(sumNet))
	}
	return out
}

// orderBankRows 按交易时间升序排列，并在同一秒内按余额衔接关系消除导出顺序歧义。
// 同秒行的真实先后无法从文件判定，但余额递推唯一确定相邻关系：若
// 「A.余额 + B.转入 - B.转出 == B.余额」，则 B 必定紧跟 A。
// 实现：按 (txnTime) 分组，组内用「以组首行余额为起点、贪心衔接」重建顺序；
// 无法唯一重建时退回原始行序（此时多行必然互不衔接，属真实不平或跨秒断点）。
func orderBankRows(parsed []bankRow) []bankRow {
	sort.SliceStable(parsed, func(i, j int) bool {
		if parsed[i].txnTime != parsed[j].txnTime {
			return parsed[i].txnTime < parsed[j].txnTime
		}
		return parsed[i].rowNo < parsed[j].rowNo
	})
	out := make([]bankRow, 0, len(parsed))
	for i := 0; i < len(parsed); {
		j := i
		for j < len(parsed) && parsed[j].txnTime == parsed[i].txnTime {
			j++
		}
		group := parsed[i:j]
		out = append(out, chainByBalance(group)...)
		i = j
	}
	return out
}

// chainByBalance 组内按余额衔接贪心重排；无法重建时退回原序交由校验标不平。
// 起点的选取：组内「不是任何其他行后继」的那一行 —— 它真正的上行在组外（或本组首行）。
func chainByBalance(group []bankRow) []bankRow {
	if len(group) <= 1 {
		return group
	}
	head := -1
	for i := range group {
		isSuccessor := false
		for j := range group {
			if i == j {
				continue
			}
			if chainsTo(group[j], group[i]) {
				isSuccessor = true
				break
			}
		}
		if !isSuccessor {
			if head >= 0 {
				// 多个候选起点：无法唯一重建，退回原序
				return group
			}
			head = i
		}
	}
	if head < 0 {
		return group
	}
	remaining := make([]bankRow, 0, len(group)-1)
	remaining = append(remaining, group[:head]...)
	remaining = append(remaining, group[head+1:]...)
	out := []bankRow{group[head]}
	cur := group[head]
	for len(remaining) > 0 {
		next := -1
		for k := range remaining {
			if chainsTo(cur, remaining[k]) {
				next = k
				break
			}
		}
		if next < 0 {
			// 无衔接行：剩余行按原序追加，交由校验标不平
			return append(out, remaining...)
		}
		cur = remaining[next]
		out = append(out, cur)
		remaining = append(remaining[:next], remaining[next+1:]...)
	}
	return out
}

// chainsTo 判断 b 是否紧跟 a：a.余额 + b.转入 - b.转出 == b.余额。
func chainsTo(a, b bankRow) bool {
	return a.balance.Add(b.inAmount).Sub(b.outAmount).Equal(b.balance)
}

func joinNonEmpty(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, "/")
}

// ImportOrderFile 导入订单流水文件。去重键 icbc_order_no UNIQUE。
func (s *ImportService) ImportOrderFile(ctx context.Context, filePath string) (ImportResult, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ImportResult{}, fmt.Errorf("打不开文件：%v", err)
	}
	name := filepath.Base(filePath)
	header, rows, err := ParseAnyFile(name, string(data))
	if err != nil {
		return ImportResult{}, fmt.Errorf("%s：%v", name, err)
	}
	return s.importOrderRows(ctx, name, header, rows)
}

// importOrderRows 订单流水导入核心（文件入口与 ImportFile 统一入口共用）。
func (s *ImportService) importOrderRows(ctx context.Context, fileName string, header []string, rows [][]string) (ImportResult, error) {
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
	now := time.Now().Format(time.RFC3339)
	res := ImportResult{BatchID: batchID, SourceType: "order_flow", FileName: fileName, RowCount: len(rows), ImportedAt: now}

	err := s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		for idx, row := range rows {
			rowNo := idx + 2
			orderNo := cell(row, iOrder)
			if orderNo == "" {
				insertErr(tx, batchID, rowNo, "icbc_order_no", "工行订单号为空，已跳过")
				res.SkipCount++
				continue
			}
			settle := cell(row, iSettle)
			if settle == "" {
				// 结算日期为空则用交易日期+1（T+1）并记待确认
				settle = deriveSettleDate(cell(row, iTxn))
				if settle == "" {
					insertErr(tx, batchID, rowNo, "settle_date", "结算日期为空且无法按T+1推导，已跳过")
					res.SkipCount++
					continue
				}
				insertErr(tx, batchID, rowNo, "settle_date", "结算日期为空，已按交易日期+1（T+1）推导，请人工确认")
			}
			amt, e1 := decimal.Parse(cell(row, iAmt))
			fee, _ := decimal.Parse(cell(row, iFee))
			net, e2 := decimal.Parse(cell(row, iNet))
			if e1 != nil || e2 != nil {
				insertErr(tx, batchID, rowNo, "amount", "金额解析失败，已跳过")
				res.SkipCount++
				continue
			}
			raw, _ := json.Marshal(map[string]string{"file": fileName, "row": fmt.Sprint(rowNo)})
			sqlRes, err := tx.Exec(`INSERT OR IGNORE INTO order_flows
				(id, batch_id, icbc_order_no, third_order_no, txn_time, settle_date,
				 order_amount, fee_amount, net_amount, pay_method, debit_credit_flag, raw_json, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				uuid.NewString(), batchID, orderNo, cell(row, iThird), cell(row, iTxn), settle,
				amt.String(), fee.String(), net.String(), cell(row, iPay), cell(row, iFlag),
				string(raw), now,
			)
			if err != nil {
				return fmt.Errorf("写入订单流水失败（第 %d 行）：%v", rowNo, err)
			}
			if n, _ := sqlRes.RowsAffected(); n == 0 {
				res.SkipCount++
				continue
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
	return res, nil
}

// deriveSettleDate 结算日期缺省时按交易日期 +1 天（T+1）推导，返回 YYYY-MM-DD。
func deriveSettleDate(txnTime string) string {
	if len(txnTime) < 10 {
		return ""
	}
	if tm, err := time.Parse("2006-01-02 15:04:05", txnTime); err == nil {
		return tm.AddDate(0, 0, 1).Format("2006-01-02")
	}
	if tm, err := time.Parse("2006-01-02", txnTime[:10]); err == nil {
		return tm.AddDate(0, 0, 1).Format("2006-01-02")
	}
	return ""
}

// ImportFile 统一导入入口：前端选文件后传文件名+内容，按文件名自动识别类型
// （银行/订单，见 DetectSourceType），解析→去重→余额校验→入库。
func (s *ImportService) ImportFile(ctx context.Context, fileName, content string) (ImportResult, error) {
	if strings.TrimSpace(fileName) == "" {
		return ImportResult{}, fmt.Errorf("缺少文件名，无法识别文件类型")
	}
	sourceType, err := DetectSourceType(fileName)
	if err != nil {
		return ImportResult{}, err
	}
	header, rows, err := ParseAnyFile(fileName, content)
	if err != nil {
		return ImportResult{}, err
	}
	switch sourceType {
	case "bank_flow":
		return s.importBankRows(ctx, fileName, header, rows)
	case "order_flow":
		return s.importOrderRows(ctx, fileName, header, rows)
	default:
		return ImportResult{}, fmt.Errorf("不支持的导入类型：%s", sourceType)
	}
}

// ListBatches 导入批次列表（导入中心用）。
func (s *ImportService) ListBatches(ctx context.Context, sourceType string) ([]ImportResult, error) {
	q := `SELECT id, source_type, file_name, row_count, success_count, skip_count,
			COALESCE(balance_warning,''), COALESCE(imported_at,'') FROM import_batches`
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
		if err := rows.Scan(&r.BatchID, &r.SourceType, &r.FileName, &r.RowCount, &r.SuccessCount, &r.SkipCount, &r.BalanceWarning, &r.ImportedAt); err != nil {
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
