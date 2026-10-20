// Package services 按领域划分的 Wails Service。
// 方法约定：context.Context 首参（阻塞/DB 操作），返回 (T, error)，
// 错误文案面向财务的简体中文。金额一律 decimal 字符串，禁止 float64。
package services

// ---------- 导入 ----------

type ImportResult struct {
	BatchID        string `json:"batchId"`
	SourceType     string `json:"sourceType"` // bank_flow / order_flow（见 import_batches.source_type）
	FileName       string `json:"fileName"`
	RowCount       int    `json:"rowCount"`
	SuccessCount   int    `json:"successCount"`
	SkipCount      int    `json:"skipCount"`
	BalanceWarning string `json:"balanceWarning"`
	ImportedAt     string `json:"importedAt"` // RFC3339，导入中心批次历史展示用
}

type ImportError struct {
	ID        string `json:"id"`
	BatchID   string `json:"batchId"`
	RowNo     int    `json:"rowNo"`
	FieldName string `json:"fieldName"`
	Message   string `json:"message"`
}

// ColumnMapping 导入前预览：识别到的关键列、缺失列与样本行（§7 F1「预览列映射」）。
type ColumnMapping struct {
	SourceType string     `json:"sourceType"`
	FileName   string     `json:"fileName"`
	Header     []string   `json:"header"`
	Matched    []string   `json:"matched"`
	Missing    []string   `json:"missing"`
	SampleRows [][]string `json:"sampleRows"`
	TotalRows  int        `json:"totalRows"`
}

// ---------- 银行流水 ----------

type BankFlow struct {
	ID               string `json:"id"`
	BatchID          string `json:"batchId"`
	FileName         string `json:"fileName"`
	RowNo            int    `json:"rowNo"`
	TxnTime          string `json:"txnTime"`
	Counterparty     string `json:"counterparty"`
	Summary          string `json:"summary"`
	Remark           string `json:"remark"`
	InAmount         string `json:"inAmount"`
	OutAmount        string `json:"outAmount"`
	Balance          string `json:"balance"`
	BalanceStatus    string `json:"balanceStatus"`
	BalanceConfirmed string `json:"balanceConfirmedAt"`
	DisplayStatus    string `json:"displayStatus"`
	CreatedAt        string `json:"createdAt"`
}

type BankFlowFilter struct {
	BatchID  string `json:"batchId"`
	Keyword  string `json:"keyword"`
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type BankFlowPage struct {
	Items    []BankFlow `json:"items"`
	Total    int        `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
}

// ---------- 默认路径导入（导入中心「默认路径导入」） ----------

// RawFile 默认原始数据目录中的一个文件（扫描结果）。
type RawFile struct {
	Name           string   `json:"name"`
	SourceType     string   `json:"sourceType"` // bank_flow / order_flow；空=未识别
	Size           int64    `json:"size"`       // 字节
	ModTime        string   `json:"modTime"`    // RFC3339
	State          string   `json:"state"`      // new / done / bad
	MissingColumns []string `json:"missingColumns"`
	ImportedAt     string   `json:"importedAt"` // 已导入批次的导入时间（RFC3339，仅 done）
	Error          string   `json:"error"`
}

// RawScan 默认路径扫描结果。DirExists=false 时前端仅提示，不报错。
type RawScan struct {
	Dir       string    `json:"dir"`
	DirExists bool      `json:"dirExists"`
	Files     []RawFile `json:"files"`
	ScannedAt string    `json:"scannedAt"` // RFC3339
}

// ImportRawReq 默认路径导入请求；Names 只能是默认目录内的文件名（防路径穿越）。
type ImportRawReq struct {
	Names   []string `json:"names"`
	Archive bool     `json:"archive"` // 导入成功后移到 archived/
}

// ImportRawResult 单个默认路径文件的导入结果。
type ImportRawResult struct {
	FileName   string       `json:"fileName"`
	Status     string       `json:"status"` // imported / failed
	Result     ImportResult `json:"result"`
	ArchivedTo string       `json:"archivedTo"`
	Error      string       `json:"error"`
}

// ---------- 订单流水 ----------

type OrderFlow struct {
	ID              string `json:"id"`
	BatchID         string `json:"batchId"`
	IcbcOrderNo     string `json:"icbcOrderNo"`
	ThirdOrderNo    string `json:"thirdOrderNo"`
	TxnTime         string `json:"txnTime"`
	SettleDate      string `json:"settleDate"`
	OrderAmount     string `json:"orderAmount"`
	FeeAmount       string `json:"feeAmount"`
	NetAmount       string `json:"netAmount"`
	PayMethod       string `json:"payMethod"`
	DebitCreditFlag string `json:"debitCreditFlag"`
	FeeFlag         string `json:"feeFlag"`
	CreatedAt       string `json:"createdAt"`
}

type OrderFlowFilter struct {
	SettleDate string `json:"settleDate"`
	Keyword    string `json:"keyword"`
	OnlyFeeAdj string `json:"onlyFeeAdj"`
	Page       int    `json:"page"`
	PageSize   int    `json:"pageSize"`
}

type OrderFlowPage struct {
	Items    []OrderFlow `json:"items"`
	Total    int         `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
}

// ---------- 对账 ----------

type ReconResult struct {
	Date          string `json:"date"`
	OrderNet      string `json:"orderNet"`
	BankPOSIn     string `json:"bankPOSIn"`
	SummaryActual string `json:"summaryActual"`
	Matched       bool   `json:"matched"`
	Diff          string `json:"diff"`
}

type ReconDiff struct {
	Date   string `json:"date"`
	Field  string `json:"field"`
	Expect string `json:"expect"`
	Actual string `json:"actual"`
	Detail string `json:"detail"`
}

// ---------- 数据汇总 ----------

type Summary struct {
	ID                string `json:"id"`
	Date              string `json:"date"`
	ReceiptNo         string `json:"receiptNo"`
	Category          string `json:"category"`
	Project           string `json:"project"`
	Reason            string `json:"reason"`
	IncomeAmount      string `json:"incomeAmount"`
	ExpenseAmount     string `json:"expenseAmount"`
	FeeAuto           string `json:"feeAuto"`
	FeeActual         string `json:"feeActual"`
	FeeManualAdjusted int    `json:"feeManualAdjusted"`
	Balance           string `json:"balance"`
	Remark            string `json:"remark"`
	Source            string `json:"source"`
	Status            string `json:"status"`
	VoucherID         string `json:"voucherId"`
	CreatedAt         string `json:"createdAt"`
	UpdatedAt         string `json:"updatedAt"`
}

type SummaryFilter struct {
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
	Status   string `json:"status"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type SummaryPage struct {
	Items    []Summary `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
}

type SaveSummaryReq struct {
	ID            string `json:"id"`
	Date          string `json:"date"`
	ReceiptNo     string `json:"receiptNo"`
	Category      string `json:"category"`
	Project       string `json:"project"`
	Reason        string `json:"reason"`
	IncomeAmount  string `json:"incomeAmount"`
	ExpenseAmount string `json:"expenseAmount"`
	FeeActual     string `json:"feeActual"`
	Balance       string `json:"balance"`
	Remark        string `json:"remark"`
	Operator      string `json:"operator"`
}

// ---------- 凭证 ----------

type VoucherEntry struct {
	ID          string `json:"id"`
	VoucherID   string `json:"voucherId"`
	AccountCode string `json:"accountCode"`
	AccountName string `json:"accountName"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
	Remark      string `json:"remark"`
	SortNo      int    `json:"sortNo"`
}

type Voucher struct {
	ID              string         `json:"id"`
	VoucherDate     string         `json:"voucherDate"`
	VoucherNo       string         `json:"voucherNo"`
	Summary         string         `json:"summaryText"`
	Status          string         `json:"status"`
	SourceSummaryID string         `json:"sourceSummaryId"`
	Entries         []VoucherEntry `json:"entries"`
	CreatedAt       string         `json:"createdAt"`
}

type VoucherFilter struct {
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
	Status   string `json:"status"`
	Page     int    `json:"page"`
	PageSize int    `json:"pageSize"`
}

type VoucherPage struct {
	Items    []Voucher `json:"items"`
	Total    int       `json:"total"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
}

type CreateVoucherReq struct {
	SummaryID string         `json:"summaryId"`
	Entries   []VoucherEntry `json:"entries"`
	Operator  string         `json:"operator"`
}

// ---------- 基础资料 / 报表 ----------

type Account struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	ParentCode  string `json:"parentCode"`
	AccountType string `json:"accountType"`
	IsActive    int    `json:"isActive"`
}

type FeeRate struct {
	PayMethod string `json:"payMethod"`
	Rate      string `json:"rate"`
	UpdatedAt string `json:"updatedAt"`
}

type BalanceRow struct {
	AccountCode string `json:"accountCode"`
	AccountName string `json:"accountName"`
	Begin       string `json:"begin"`
	Debit       string `json:"debit"`
	Credit      string `json:"credit"`
	End         string `json:"end"`
}

type DailyRow struct {
	Date    string `json:"date"`
	Income  string `json:"income"`
	Expense string `json:"expense"`
}

type AuditLog struct {
	ID         string `json:"id"`
	EntityType string `json:"entityType"`
	EntityID   string `json:"entityId"`
	Action     string `json:"action"`
	OldValues  string `json:"oldValues"`
	NewValues  string `json:"newValues"`
	Operator   string `json:"operator"`
	OperatedAt string `json:"operatedAt"`
	Remark     string `json:"remark"`
}
