// Package services 按领域划分的 Wails Service。
// 方法约定：context.Context 首参（阻塞/DB 操作），返回 (T, error)，
// 错误文案面向财务的简体中文。金额一律 decimal 字符串，禁止 float64。
package services

// ---------- 导入 ----------

type ImportResult struct {
	BatchID        string `json:"batchId"`
	FileName       string `json:"fileName"`
	RowCount       int    `json:"rowCount"`
	SuccessCount   int    `json:"successCount"`
	SkipCount      int    `json:"skipCount"`
	BalanceWarning string `json:"balanceWarning"`
}

type ImportError struct {
	ID        string `json:"id"`
	BatchID   string `json:"batchId"`
	RowNo     int    `json:"rowNo"`
	FieldName string `json:"fieldName"`
	Message   string `json:"message"`
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
