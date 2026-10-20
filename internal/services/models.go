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
	ID                  string `json:"id"`
	BatchID             string `json:"batchId"`
	FileName            string `json:"fileName"`
	RowNo               int    `json:"rowNo"`
	TxnTime             string `json:"txnTime"`
	Counterparty        string `json:"counterparty"`
	CounterpartyAccount string `json:"counterpartyAccount"` // raw_json 中保留的「对方账号」
	Summary             string `json:"summary"`
	Remark              string `json:"remark"`
	InAmount            string `json:"inAmount"`
	OutAmount           string `json:"outAmount"`
	Balance             string `json:"balance"`
	BalanceStatus       string `json:"balanceStatus"`
	BalanceConfirmed    string `json:"balanceConfirmedAt"`
	DisplayStatus       string `json:"displayStatus"`
	PrevBalance         string `json:"prevBalance"`     // 同一批次上行余额；空=首行
	ExpectedBalance     string `json:"expectedBalance"` // 上行余额 + 转入 - 转出
	BalanceDiff         string `json:"balanceDiff"`     // 本行余额 - 应到余额（带符号）
	IsPOS               bool   `json:"isPos"`
	VoucherID           string `json:"voucherId"`
	VoucherNo           string `json:"voucherNo"`
	RawJSON             string `json:"rawJson"`
	CreatedAt           string `json:"createdAt"`
}

type BankFlowFilter struct {
	BatchID  string `json:"batchId"`
	Keyword  string `json:"keyword"`
	DateFrom string `json:"dateFrom"`
	DateTo   string `json:"dateTo"`
}

// BankFlowPage 一次性返回全部明细（DESIGN.md §7 禁止分页），附带筛选结果的汇总统计。
type BankFlowPage struct {
	Items          []BankFlow `json:"items"`
	Total          int        `json:"total"`
	TotalIn        string     `json:"totalIn"`
	TotalOut       string     `json:"totalOut"`
	InCount        int        `json:"inCount"`
	OutCount       int        `json:"outCount"`
	MismatchCount  int        `json:"mismatchCount"`
	ConfirmedCount int        `json:"confirmedCount"`
	SkipCount      int        `json:"skipCount"`
}

// BankFlowBatch 批次信息栏用：导入统计 + 余额不平 / 已确认行数。
type BankFlowBatch struct {
	BatchID        string `json:"batchId"`
	FileName       string `json:"fileName"`
	RowCount       int    `json:"rowCount"`
	SuccessCount   int    `json:"successCount"`
	SkipCount      int    `json:"skipCount"`
	ImportedAt     string `json:"importedAt"`
	MismatchCount  int    `json:"mismatchCount"`
	ConfirmedCount int    `json:"confirmedCount"`
	ConfirmedAt    string `json:"confirmedAt"`
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

// OrderFlow 订单流水单行（原始表只读）。
// 费率复算字段（FeeRate / ExpectedFee / FeeDiff / NetDiff）全部由 Go decimal 计算，前端只格式化。
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
	FeeRate         string `json:"feeRate"`
	ExpectedFee     string `json:"expectedFee"`
	ExpectedNet     string `json:"expectedNet"`
	FeeDiff         string `json:"feeDiff"`
	NetDiff         string `json:"netDiff"`
	CreatedAt       string `json:"createdAt"`
	FileName        string `json:"fileName"`
	ImportedAt      string `json:"importedAt"`
	SourceRowNo     int    `json:"sourceRowNo"`
	RawJSON         string `json:"rawJson"`
}

// OrderFlowFilter 订单流水筛选（页面一次性完整展示，不分页）。
type OrderFlowFilter struct {
	SettleDate string `json:"settleDate"`
	Keyword    string `json:"keyword"`
	OnlyFeeAdj string `json:"onlyFeeAdj"`
}

// OrderFlowGroup 按结算日期（收入归属日）归集的小计行。
type OrderFlowGroup struct {
	SettleDate    string      `json:"settleDate"`
	Count         int         `json:"count"`
	OrderTotal    string      `json:"orderTotal"`
	FeeTotal      string      `json:"feeTotal"`
	NetTotal      string      `json:"netTotal"`
	AnomalyCount  int         `json:"anomalyCount"`
	ParseErrCount int         `json:"parseErrCount"`
	Items         []OrderFlow `json:"items"`
}

// OrderFlowPage 订单流水列表页数据：按结算日期分组 + 全量筛选合计。
type OrderFlowPage struct {
	Groups        []OrderFlowGroup `json:"groups"`
	Total         int              `json:"total"`
	OrderTotal    string           `json:"orderTotal"`
	FeeTotal      string           `json:"feeTotal"`
	NetTotal      string           `json:"netTotal"`
	ReconDiff     string           `json:"reconDiff"`
	FeeDiffCount  int              `json:"feeDiffCount"`
	NetDiffCount  int              `json:"netDiffCount"`
	ParseErrCount int              `json:"parseErrCount"`
	AnomalyCount  int              `json:"anomalyCount"`
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

// ---------- 店面收款（F9） ----------

// Collection 店面收款登记记录。金额为 decimal 字符串，日期 YYYY-MM-DD。
type Collection struct {
	ID              string `json:"id"`   // 收款单号 SCYYMMDDNNN
	Date            string `json:"date"` // 收款日期
	Store           string `json:"store"`
	Customer        string `json:"customer"`
	Phone           string `json:"phone"`
	Project         string `json:"project"`
	PayMethod       string `json:"payMethod"` // cash / wechat / alipay / bank_transfer / other
	Account         string `json:"account"`   // cash / personal / corporate
	Amount          string `json:"amount"`
	ReceiptNo       string `json:"receiptNo"`
	Operator        string `json:"operator"`
	Invoiced        bool   `json:"invoiced"`
	AttachmentCount int    `json:"attachmentCount"`
	Status          string `json:"status"` // draft / confirmed / summarized / voided
	VoidReason      string `json:"voidReason"`
	SummaryID       string `json:"summaryId"`
	Note            string `json:"note"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

// CollectionAttachment 附件元数据（内容不随列表返回）。
type CollectionAttachment struct {
	ID           string `json:"id"`
	CollectionID string `json:"collectionId"`
	FileName     string `json:"fileName"`
	Ext          string `json:"ext"`
	SizeBytes    int    `json:"sizeBytes"`
	SortNo       int    `json:"sortNo"`
	CreatedAt    string `json:"createdAt"`
}

// CollectionAttachmentData 附件内容（base64）：仅在查看/预览时按需读取。
type CollectionAttachmentData struct {
	ID         string `json:"id"`
	FileName   string `json:"fileName"`
	Ext        string `json:"ext"`
	MimeType   string `json:"mimeType"`
	SizeBytes  int    `json:"sizeBytes"`
	DataBase64 string `json:"dataBase64"` // 不含 data: 前缀
}

// CollectionAttachmentInput 保存时提交的附件：ID 非空=保留已有，DataBase64 非空=新增。
type CollectionAttachmentInput struct {
	ID         string `json:"id"`
	FileName   string `json:"fileName"`
	DataBase64 string `json:"dataBase64"`
}

// CollectionStats 期间统计（不受门店/方式/关键字等筛选影响，对齐原型统计卡）。
type CollectionStats struct {
	ValidCount      int    `json:"validCount"`
	TotalAmount     string `json:"totalAmount"`
	DraftCount      int    `json:"draftCount"`
	DraftAmount     string `json:"draftAmount"`
	ConfirmedCount  int    `json:"confirmedCount"`
	ConfirmedAmount string `json:"confirmedAmount"`
	SummarizedCount int    `json:"summarizedCount"`
	VoidCount       int    `json:"voidCount"`
	CashAmount      string `json:"cashAmount"`
	PersonalAmount  string `json:"personalAmount"`
	CorporateAmount string `json:"corporateAmount"`
}

// CollectionFilter 店面收款筛选（一次性完整展示，不分页）。
type CollectionFilter struct {
	DateFrom  string `json:"dateFrom"`
	DateTo    string `json:"dateTo"`
	Store     string `json:"store"`
	PayMethod string `json:"payMethod"`
	Account   string `json:"account"`
	Operator  string `json:"operator"`
	Status    string `json:"status"`
	Keyword   string `json:"keyword"`
}

// CollectionPage 收款登记页数据：items 为筛选结果，stats 为期间统计。
type CollectionPage struct {
	Items       []Collection    `json:"items"`
	Total       int             `json:"total"` // 筛选命中条数（含作废）
	ValidCount  int             `json:"validCount"`
	VoidCount   int             `json:"voidCount"`
	TotalAmount string          `json:"totalAmount"`
	Stats       CollectionStats `json:"stats"`
	Stores      []string        `json:"stores"`
	Projects    []string        `json:"projects"`
	Operators   []string        `json:"operators"`
}

// SaveCollectionReq 新增/编辑收款。Submit=true 表示保存并提交（直接 confirmed）。
type SaveCollectionReq struct {
	ID          string                      `json:"id"`
	Date        string                      `json:"date"`
	Store       string                      `json:"store"`
	Customer    string                      `json:"customer"`
	Phone       string                      `json:"phone"`
	Project     string                      `json:"project"`
	PayMethod   string                      `json:"payMethod"`
	Account     string                      `json:"account"`
	Amount      string                      `json:"amount"`
	ReceiptNo   string                      `json:"receiptNo"`
	Operator    string                      `json:"operator"`
	Invoiced    bool                        `json:"invoiced"`
	Note        string                      `json:"note"`
	Submit      bool                        `json:"submit"`
	Attachments []CollectionAttachmentInput `json:"attachments"`
}

// CollectionDetail 收款详情：主记录 + 附件元数据 + 操作记录 + 关联汇总状态。
type CollectionDetail struct {
	Collection    Collection             `json:"collection"`
	Attachments   []CollectionAttachment `json:"attachments"`
	AuditLogs     []AuditLog             `json:"auditLogs"`
	SummaryStatus string                 `json:"summaryStatus"`
}

// CollectionChannel 通道对账一行（非银通道 / 对公转账）。
type CollectionChannel struct {
	Key           string `json:"key"`
	Name          string `json:"name"`
	RegisterTotal string `json:"registerTotal"`
	RegisterCount int    `json:"registerCount"`
	ReconTotal    string `json:"reconTotal"`
	Diff          string `json:"diff"`
	Status        string `json:"status"` // matched / pending
	Note          string `json:"note"`
}

// CollectionPending 通道对账待处理项。
type CollectionPending struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Meta    string `json:"meta"`
	Amount  string `json:"amount"`
	Channel string `json:"channel"`
}

// CollectionRecon 通道对账视图数据。
type CollectionRecon struct {
	DateFrom     string              `json:"dateFrom"`
	DateTo       string              `json:"dateTo"`
	Channels     []CollectionChannel `json:"channels"`
	Pending      []CollectionPending `json:"pending"`
	PendingCount int                 `json:"pendingCount"`
}

// CollectionSummaryRef 生成数据汇总后的返回引用。
type CollectionSummaryRef struct {
	CollectionID string `json:"collectionId"`
	SummaryID    string `json:"summaryId"`
	Status       string `json:"status"`
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
