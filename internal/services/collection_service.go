package services

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"finlite/internal/decimal"
	"finlite/internal/store"

	"github.com/google/uuid"
	sdec "github.com/shopspring/decimal"
)

// CollectionService 店面收款（F9）：店面线下收款登记 / 附件 / 审核 / 生成汇总 / 通道对账。
// 状态机：draft → confirmed → summarized；draft|confirmed → voided。
// 金额全程 decimal（TEXT 存储），所有状态变更写 audit_logs（业务文档 §10）。
type CollectionService struct {
	DB *store.DB
}

func NewCollectionService(db *store.DB) *CollectionService { return &CollectionService{DB: db} }

const (
	collectionDraft      = "draft"
	collectionConfirmed  = "confirmed"
	collectionSummarized = "summarized"
	collectionVoided     = "voided"

	// defaultOperator 单机单账套 MVP 无登录，默认经手人沿用其他模块的 local。
	defaultOperator = "local"

	maxCollectionAttachments     = 10
	maxCollectionAttachmentBytes = 10 << 20 // 10MB
)

// collectionPayMethodLabels 收款方式枚举（与原型 e-method 一致）。
var collectionPayMethodLabels = map[string]string{
	"cash":          "现金",
	"wechat":        "微信个人码",
	"alipay":        "支付宝个人码",
	"bank_transfer": "对公转账",
	"other":         "其他",
}

// collectionAccountLabels 到账账户枚举：cash 现金 / personal 个人码 / corporate 对公。
var collectionAccountLabels = map[string]string{
	"cash":      "现金账户",
	"personal":  "个人收款账户",
	"corporate": "对公银行账户",
}

// collectionAccountByPayMethod 收款方式 → 默认到账账户（自动推荐，可手工改）。
var collectionAccountByPayMethod = map[string]string{
	"cash":          "cash",
	"wechat":        "personal",
	"alipay":        "personal",
	"bank_transfer": "corporate",
	"other":         "cash",
}

var collectionStatusLabels = map[string]string{
	collectionDraft:      "草稿",
	collectionConfirmed:  "已确认",
	collectionSummarized: "已汇总",
	collectionVoided:     "已作废",
}

// collectionAttachmentMime 允许的附件类型 → MIME。
var collectionAttachmentMime = map[string]string{
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"webp": "image/webp",
	"pdf":  "application/pdf",
}

const collectionSelect = `SELECT c.id, c.collect_date, c.store, c.customer, c.phone, c.project,
	c.pay_method, c.account, c.amount, c.receipt_no, c.operator, c.invoiced, c.status,
	c.void_reason, COALESCE(c.summary_id,''), c.note, c.created_at, c.updated_at,
	(SELECT COUNT(*) FROM collection_attachments a WHERE a.collection_id = c.id)
	FROM collections c`

// rowScanner 兼容 *sql.Row 与 *sql.Rows。
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCollection(sc rowScanner) (Collection, error) {
	var c Collection
	var invoiced int
	err := sc.Scan(&c.ID, &c.Date, &c.Store, &c.Customer, &c.Phone, &c.Project,
		&c.PayMethod, &c.Account, &c.Amount, &c.ReceiptNo, &c.Operator, &invoiced,
		&c.Status, &c.VoidReason, &c.SummaryID, &c.Note, &c.CreatedAt, &c.UpdatedAt,
		&c.AttachmentCount)
	c.Invoiced = invoiced == 1
	return c, err
}

func collectionStatusLabel(s string) string {
	if label, ok := collectionStatusLabels[s]; ok {
		return label
	}
	return s
}

func collectionPayMethodLabel(s string) string {
	if label, ok := collectionPayMethodLabels[s]; ok {
		return label
	}
	return s
}

// List 收款登记列表：按收款日期倒序一次性返回（DESIGN.md §7 禁止分页）。
// stats 只按日期区间统计（对齐原型统计卡），items 叠加门店/方式/账户/经手人/状态/关键字筛选。
func (s *CollectionService) List(ctx context.Context, f CollectionFilter) (CollectionPage, error) {
	where := []string{"1=1"}
	args := []any{}
	if d := strings.TrimSpace(f.DateFrom); d != "" {
		where = append(where, "c.collect_date>=?")
		args = append(args, d)
	}
	if d := strings.TrimSpace(f.DateTo); d != "" {
		where = append(where, "c.collect_date<=?")
		args = append(args, d)
	}
	rows, err := s.DB.Pool().QueryContext(ctx,
		collectionSelect+" WHERE "+strings.Join(where, " AND ")+
			" ORDER BY c.collect_date DESC, c.created_at DESC, c.id DESC", args...)
	if err != nil {
		return CollectionPage{}, fmt.Errorf("查询店面收款失败：%v", err)
	}
	defer rows.Close()

	all := make([]Collection, 0, 64)
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			return CollectionPage{}, fmt.Errorf("读取店面收款失败：%v", err)
		}
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		return CollectionPage{}, fmt.Errorf("读取店面收款失败：%v", err)
	}

	stats := CollectionStats{
		TotalAmount:     "0.00",
		DraftAmount:     "0.00",
		ConfirmedAmount: "0.00",
		CashAmount:      "0.00",
		PersonalAmount:  "0.00",
		CorporateAmount: "0.00",
	}
	total := sdec.Zero
	cash := sdec.Zero
	personal := sdec.Zero
	corporate := sdec.Zero
	draft := sdec.Zero
	confirmed := sdec.Zero
	for _, c := range all {
		amt, err := decimal.Parse(c.Amount)
		if err != nil {
			amt = sdec.Zero
		}
		switch c.Status {
		case collectionVoided:
			stats.VoidCount++
			continue
		case collectionDraft:
			stats.DraftCount++
			draft = draft.Add(amt)
		case collectionConfirmed:
			stats.ConfirmedCount++
			confirmed = confirmed.Add(amt)
		case collectionSummarized:
			stats.SummarizedCount++
		}
		stats.ValidCount++
		total = total.Add(amt)
		switch c.Account {
		case "cash":
			cash = cash.Add(amt)
		case "personal":
			personal = personal.Add(amt)
		case "corporate":
			corporate = corporate.Add(amt)
		}
	}
	stats.TotalAmount = decimal.Format(total)
	stats.DraftAmount = decimal.Format(draft)
	stats.ConfirmedAmount = decimal.Format(confirmed)
	stats.CashAmount = decimal.Format(cash)
	stats.PersonalAmount = decimal.Format(personal)
	stats.CorporateAmount = decimal.Format(corporate)

	// 明细筛选（在已按日期区间取出的集合上做，避免二次查询）。
	items := make([]Collection, 0, len(all))
	filteredTotal := sdec.Zero
	validCount := 0
	voidCount := 0
	kw := strings.ToLower(strings.TrimSpace(f.Keyword))
	for _, c := range all {
		if !matchCollection(c, f, kw) {
			continue
		}
		items = append(items, c)
		if c.Status == collectionVoided {
			voidCount++
			continue
		}
		validCount++
		if amt, err := decimal.Parse(c.Amount); err == nil {
			filteredTotal = filteredTotal.Add(amt)
		}
	}

	stores, err := s.distinctValues(ctx, "store")
	if err != nil {
		return CollectionPage{}, err
	}
	projects, err := s.distinctValues(ctx, "project")
	if err != nil {
		return CollectionPage{}, err
	}
	operators, err := s.distinctValues(ctx, "operator")
	if err != nil {
		return CollectionPage{}, err
	}

	return CollectionPage{
		Items:       items,
		Total:       len(items),
		ValidCount:  validCount,
		VoidCount:   voidCount,
		TotalAmount: decimal.Format(filteredTotal),
		Stats:       stats,
		Stores:      stores,
		Projects:    projects,
		Operators:   operators,
	}, nil
}

// matchCollection 明细筛选：下拉条件精确匹配，关键字命中客户/收据号/事由/项目/单号/门店/经手人。
func matchCollection(c Collection, f CollectionFilter, kw string) bool {
	if v := strings.TrimSpace(f.Store); v != "" && c.Store != v {
		return false
	}
	if v := strings.TrimSpace(f.PayMethod); v != "" && c.PayMethod != v {
		return false
	}
	if v := strings.TrimSpace(f.Account); v != "" && c.Account != v {
		return false
	}
	if v := strings.TrimSpace(f.Operator); v != "" && c.Operator != v {
		return false
	}
	if v := strings.TrimSpace(f.Status); v != "" && c.Status != v {
		return false
	}
	if kw != "" {
		hay := strings.ToLower(strings.Join([]string{
			c.Customer, c.ReceiptNo, c.Note, c.Project, c.ID, c.Store, c.Operator,
		}, " "))
		if !strings.Contains(hay, kw) {
			return false
		}
	}
	return true
}

// distinctValues 去重取值（门店/项目/经手人 datalist 候选）。列名仅来自内部常量。
func (s *CollectionService) distinctValues(ctx context.Context, column string) ([]string, error) {
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT DISTINCT `+column+` FROM collections WHERE `+column+`<>'' ORDER BY `+column)
	if err != nil {
		return nil, fmt.Errorf("读取店面收款选项失败：%v", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("读取店面收款选项失败：%v", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Get 收款详情：主记录 + 附件元数据 + 操作记录 + 关联汇总状态。
func (s *CollectionService) Get(ctx context.Context, id string) (CollectionDetail, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CollectionDetail{}, fmt.Errorf("收款记录不能为空")
	}
	c, err := scanCollection(s.DB.Pool().QueryRowContext(ctx, collectionSelect+" WHERE c.id=?", id))
	if err == sql.ErrNoRows {
		return CollectionDetail{}, fmt.Errorf("收款记录不存在：%s", id)
	}
	if err != nil {
		return CollectionDetail{}, fmt.Errorf("读取收款记录失败：%v", err)
	}

	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT id, collection_id, file_name, ext, size_bytes, sort_no, created_at
		 FROM collection_attachments WHERE collection_id=? ORDER BY sort_no, created_at`, id)
	if err != nil {
		return CollectionDetail{}, fmt.Errorf("读取收款附件失败：%v", err)
	}
	attachments := []CollectionAttachment{}
	for rows.Next() {
		var a CollectionAttachment
		if err := rows.Scan(&a.ID, &a.CollectionID, &a.FileName, &a.Ext, &a.SizeBytes,
			&a.SortNo, &a.CreatedAt); err != nil {
			rows.Close()
			return CollectionDetail{}, fmt.Errorf("读取收款附件失败：%v", err)
		}
		attachments = append(attachments, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return CollectionDetail{}, fmt.Errorf("读取收款附件失败：%v", err)
	}

	auditRows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT id, entity_type, entity_id, action, COALESCE(old_values,''), COALESCE(new_values,''),
			COALESCE(operator,''), operated_at, COALESCE(remark,'')
		 FROM audit_logs WHERE entity_type='collection' AND entity_id=?
		 ORDER BY operated_at DESC LIMIT 100`, id)
	if err != nil {
		return CollectionDetail{}, fmt.Errorf("读取操作记录失败：%v", err)
	}
	logs := []AuditLog{}
	for auditRows.Next() {
		var a AuditLog
		if err := auditRows.Scan(&a.ID, &a.EntityType, &a.EntityID, &a.Action, &a.OldValues,
			&a.NewValues, &a.Operator, &a.OperatedAt, &a.Remark); err != nil {
			auditRows.Close()
			return CollectionDetail{}, fmt.Errorf("读取操作记录失败：%v", err)
		}
		logs = append(logs, a)
	}
	auditRows.Close()
	if err := auditRows.Err(); err != nil {
		return CollectionDetail{}, fmt.Errorf("读取操作记录失败：%v", err)
	}

	summaryStatus := ""
	if c.SummaryID != "" {
		if err := s.DB.Pool().QueryRowContext(ctx,
			`SELECT status FROM data_summaries WHERE id=?`, c.SummaryID).Scan(&summaryStatus); err != nil &&
			err != sql.ErrNoRows {
			return CollectionDetail{}, fmt.Errorf("读取关联汇总失败：%v", err)
		}
	}

	return CollectionDetail{
		Collection:    c,
		Attachments:   attachments,
		AuditLogs:     logs,
		SummaryStatus: summaryStatus,
	}, nil
}

// GetAttachment 按需读取附件内容（base64，不含 data: 前缀）。
func (s *CollectionService) GetAttachment(ctx context.Context, id string) (CollectionAttachmentData, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CollectionAttachmentData{}, fmt.Errorf("附件不能为空")
	}
	var out CollectionAttachmentData
	var content []byte
	err := s.DB.Pool().QueryRowContext(ctx,
		`SELECT id, file_name, ext, size_bytes, content FROM collection_attachments WHERE id=?`, id).
		Scan(&out.ID, &out.FileName, &out.Ext, &out.SizeBytes, &content)
	if err == sql.ErrNoRows {
		return CollectionAttachmentData{}, fmt.Errorf("附件不存在：%s", id)
	}
	if err != nil {
		return CollectionAttachmentData{}, fmt.Errorf("读取附件失败：%v", err)
	}
	if mime, ok := collectionAttachmentMime[out.Ext]; ok {
		out.MimeType = mime
	} else {
		out.MimeType = "application/octet-stream"
	}
	out.DataBase64 = base64.StdEncoding.EncodeToString(content)
	return out, nil
}

// Save 新增或编辑收款（仅草稿可编辑）。Submit=true 表示保存并提交，直接进入 confirmed。
// 附件在同一事务内对账：ID 非空=保留，DataBase64 非空=新增，未列出=删除。
func (s *CollectionService) Save(ctx context.Context, req SaveCollectionReq) (Collection, error) {
	date := strings.TrimSpace(req.Date)
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return Collection{}, fmt.Errorf("收款日期格式不正确（需 YYYY-MM-DD）：%s", req.Date)
	}
	storeName := strings.TrimSpace(req.Store)
	if storeName == "" {
		return Collection{}, fmt.Errorf("请选择或填写门店")
	}
	customer := strings.TrimSpace(req.Customer)
	if customer == "" {
		return Collection{}, fmt.Errorf("请填写客户姓名")
	}
	project := strings.TrimSpace(req.Project)
	if project == "" {
		return Collection{}, fmt.Errorf("请选择或填写项目 / 事由类别")
	}
	method := strings.TrimSpace(req.PayMethod)
	if _, ok := collectionPayMethodLabels[method]; !ok {
		return Collection{}, fmt.Errorf("收款方式不正确：%s", method)
	}
	account := strings.TrimSpace(req.Account)
	if account == "" {
		account = collectionAccountByPayMethod[method]
	}
	if _, ok := collectionAccountLabels[account]; !ok {
		return Collection{}, fmt.Errorf("到账账户不正确：%s", account)
	}
	amount, err := decimal.Parse(req.Amount)
	if err != nil {
		return Collection{}, fmt.Errorf("收款金额格式不正确：%s", req.Amount)
	}
	if !amount.GreaterThan(sdec.Zero) {
		return Collection{}, fmt.Errorf("收款金额必须大于 0（当前 %s）", decimal.Format(amount))
	}
	amount = amount.Round(2)
	operator := strings.TrimSpace(req.Operator)
	if operator == "" {
		operator = defaultOperator
	}
	receiptNo := strings.TrimSpace(req.ReceiptNo)
	if len(req.Attachments) > maxCollectionAttachments {
		return Collection{}, fmt.Errorf("单条收款记录最多 %d 张附件（当前 %d 张）",
			maxCollectionAttachments, len(req.Attachments))
	}

	now := time.Now().Format(time.RFC3339)
	var out Collection
	err = s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		if err := ensureCollectionReceiptUnique(tx, receiptNo, strings.TrimSpace(req.ID)); err != nil {
			return err
		}
		id := strings.TrimSpace(req.ID)
		if id == "" {
			if id, err = nextCollectionID(tx, date); err != nil {
				return err
			}
			status := collectionDraft
			action := "create"
			if req.Submit {
				status = collectionConfirmed
				action = "submit"
			}
			if _, err := tx.Exec(`INSERT INTO collections
				(id, collect_date, store, customer, phone, project, pay_method, account,
				 amount, receipt_no, operator, invoiced, status, void_reason, note, created_at, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?)`,
				id, date, storeName, customer, strings.TrimSpace(req.Phone), project, method, account,
				decimal.Format(amount), receiptNo, operator, boolInt(req.Invoiced), status,
				strings.TrimSpace(req.Note), now, now); err != nil {
				return fmt.Errorf("保存收款记录失败：%v", err)
			}
			newJSON, _ := json.Marshal(map[string]any{
				"id": id, "date": date, "store": storeName, "customer": customer,
				"amount": decimal.Format(amount), "status": status,
			})
			if err := store.WriteAudit(tx, "collection", id, action, "", string(newJSON), operator, ""); err != nil {
				return fmt.Errorf("写操作记录失败：%v", err)
			}
		} else {
			old, err := scanCollection(tx.QueryRow(collectionSelect+" WHERE c.id=?", id))
			if err == sql.ErrNoRows {
				return fmt.Errorf("收款记录不存在：%s", id)
			}
			if err != nil {
				return fmt.Errorf("读取收款记录失败：%v", err)
			}
			if old.Status != collectionDraft {
				return fmt.Errorf("只有草稿可修改，当前状态：%s；已确认/已汇总的记录请先驳回或作废后重录",
					collectionStatusLabel(old.Status))
			}
			// 草稿编辑时「保存并提交」同样推进到 confirmed（与新建行为一致）。
			status, action := collectionDraft, "update"
			if req.Submit {
				status, action = collectionConfirmed, "submit"
			}
			if _, err := tx.Exec(`UPDATE collections SET collect_date=?, store=?, customer=?, phone=?,
				project=?, pay_method=?, account=?, amount=?, receipt_no=?, operator=?, invoiced=?,
				status=?, note=?, updated_at=? WHERE id=?`,
				date, storeName, customer, strings.TrimSpace(req.Phone), project, method, account,
				decimal.Format(amount), receiptNo, operator, boolInt(req.Invoiced),
				status, strings.TrimSpace(req.Note), now, id); err != nil {
				return fmt.Errorf("更新收款记录失败：%v", err)
			}
			oldJSON, _ := json.Marshal(old)
			newJSON, _ := json.Marshal(map[string]any{
				"date": date, "store": storeName, "customer": customer,
				"amount": decimal.Format(amount), "status": status,
			})
			if err := store.WriteAudit(tx, "collection", id, action, string(oldJSON), string(newJSON), operator, ""); err != nil {
				return fmt.Errorf("写操作记录失败：%v", err)
			}
		}
		if err := reconcileCollectionAttachments(tx, id, req.Attachments, now); err != nil {
			return err
		}
		out, err = scanCollection(tx.QueryRow(collectionSelect+" WHERE c.id=?", id))
		if err != nil {
			return fmt.Errorf("读取收款记录失败：%v", err)
		}
		return nil
	})
	if err != nil {
		return Collection{}, err
	}
	return out, nil
}

// Confirm 确认收款：draft → confirmed。
func (s *CollectionService) Confirm(ctx context.Context, id, operator string) (Collection, error) {
	return s.transition(ctx, id, collectionDraft, collectionConfirmed, "confirm", operator, "")
}

// Reject 驳回：confirmed → draft，必须填写原因。
func (s *CollectionService) Reject(ctx context.Context, id, reason, operator string) (Collection, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Collection{}, fmt.Errorf("请填写驳回原因")
	}
	return s.transition(ctx, id, collectionConfirmed, collectionDraft, "reject", operator, reason)
}

// Void 作废：draft / confirmed → voided，必须填写原因；已汇总的记录须先作废关联汇总。
func (s *CollectionService) Void(ctx context.Context, id, reason, operator string) (Collection, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Collection{}, fmt.Errorf("请填写作废原因")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return Collection{}, fmt.Errorf("收款记录不能为空")
	}
	now := time.Now().Format(time.RFC3339)
	var out Collection
	err := s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		old, err := scanCollection(tx.QueryRow(collectionSelect+" WHERE c.id=?", id))
		if err == sql.ErrNoRows {
			return fmt.Errorf("收款记录不存在：%s", id)
		}
		if err != nil {
			return fmt.Errorf("读取收款记录失败：%v", err)
		}
		if old.Status == collectionVoided {
			return fmt.Errorf("该收款记录已作废，请勿重复操作")
		}
		if old.Status == collectionSummarized {
			return fmt.Errorf("该收款已生成数据汇总（%s），请先在数据汇总中作废后再撤销收款", old.SummaryID)
		}
		if _, err := tx.Exec(`UPDATE collections SET status=?, void_reason=?, updated_at=? WHERE id=?`,
			collectionVoided, reason, now, id); err != nil {
			return fmt.Errorf("作废收款记录失败：%v", err)
		}
		oldJSON, _ := json.Marshal(old)
		newJSON, _ := json.Marshal(map[string]any{"status": collectionVoided, "voidReason": reason})
		if err := store.WriteAudit(tx, "collection", id, "void", string(oldJSON), string(newJSON), operator, reason); err != nil {
			return fmt.Errorf("写操作记录失败：%v", err)
		}
		out, err = scanCollection(tx.QueryRow(collectionSelect+" WHERE c.id=?", id))
		return err
	})
	if err != nil {
		return Collection{}, err
	}
	return out, nil
}

// transition 通用状态迁移 + 审计。
func (s *CollectionService) transition(ctx context.Context, id, from, to, action, operator, remark string) (Collection, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Collection{}, fmt.Errorf("收款记录不能为空")
	}
	operator = strings.TrimSpace(operator)
	if operator == "" {
		operator = defaultOperator
	}
	now := time.Now().Format(time.RFC3339)
	var out Collection
	err := s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		old, err := scanCollection(tx.QueryRow(collectionSelect+" WHERE c.id=?", id))
		if err == sql.ErrNoRows {
			return fmt.Errorf("收款记录不存在：%s", id)
		}
		if err != nil {
			return fmt.Errorf("读取收款记录失败：%v", err)
		}
		if old.Status != from {
			return fmt.Errorf("当前状态为「%s」，不能执行该操作（需为「%s」）",
				collectionStatusLabel(old.Status), collectionStatusLabel(from))
		}
		if _, err := tx.Exec(`UPDATE collections SET status=?, updated_at=? WHERE id=?`, to, now, id); err != nil {
			return fmt.Errorf("更新收款状态失败：%v", err)
		}
		oldJSON, _ := json.Marshal(map[string]string{"status": old.Status})
		newJSON, _ := json.Marshal(map[string]string{"status": to})
		if err := store.WriteAudit(tx, "collection", id, action, string(oldJSON), string(newJSON), operator, remark); err != nil {
			return fmt.Errorf("写操作记录失败：%v", err)
		}
		out, err = scanCollection(tx.QueryRow(collectionSelect+" WHERE c.id=?", id))
		return err
	})
	if err != nil {
		return Collection{}, err
	}
	return out, nil
}

// GenerateSummary 生成数据汇总：confirmed → summarized，并写入 source='collection' 的汇总草稿。
// 一收款一汇总：summary_id 落库，重复生成直接拒绝。
func (s *CollectionService) GenerateSummary(ctx context.Context, id, operator string) (CollectionSummaryRef, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return CollectionSummaryRef{}, fmt.Errorf("收款记录不能为空")
	}
	operator = strings.TrimSpace(operator)
	if operator == "" {
		operator = defaultOperator
	}
	now := time.Now().Format(time.RFC3339)
	var ref CollectionSummaryRef
	err := s.DB.WithWriteTx(ctx, func(tx *sql.Tx) error {
		old, err := scanCollection(tx.QueryRow(collectionSelect+" WHERE c.id=?", id))
		if err == sql.ErrNoRows {
			return fmt.Errorf("收款记录不存在：%s", id)
		}
		if err != nil {
			return fmt.Errorf("读取收款记录失败：%v", err)
		}
		switch old.Status {
		case collectionSummarized:
			return fmt.Errorf("该收款已生成数据汇总（%s），请勿重复生成", old.SummaryID)
		case collectionVoided:
			return fmt.Errorf("该收款已作废，不能生成数据汇总")
		case collectionDraft:
			return fmt.Errorf("草稿收款需先确认收款，再生成数据汇总")
		}
		summaryID := uuid.NewString()
		// 收据号与 data_summaries 部分唯一索引冲突时留空，避免把收款收据号当作汇总收据号阻断生成。
		receiptNo := old.ReceiptNo
		if receiptNo != "" {
			var n int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM data_summaries WHERE receipt_no=?`, receiptNo).Scan(&n); err != nil {
				return fmt.Errorf("校验收据号失败：%v", err)
			}
			if n > 0 {
				receiptNo = ""
			}
		}
		remark := fmt.Sprintf("店面收款 %s · %s · %s · %s",
			old.ID, old.Store, old.Customer, collectionPayMethodLabel(old.PayMethod))
		if _, err := tx.Exec(`INSERT INTO data_summaries
			(id, date, receipt_no, category, project, reason, income_amount, expense_amount,
			 fee_auto, fee_actual, fee_manual_adjusted, balance, remark, source, status, created_at, updated_at)
			VALUES (?, ?, ?, '收入', '营业收入', ?, ?, '0', '0', '0', 0, '0', ?, 'collection', 'draft', ?, ?)`,
			summaryID, old.Date, receiptNo, old.Project, old.Amount, remark, now, now); err != nil {
			return fmt.Errorf("生成数据汇总失败：%v", err)
		}
		if _, err := tx.Exec(`UPDATE collections SET status=?, summary_id=?, updated_at=? WHERE id=?`,
			collectionSummarized, summaryID, now, id); err != nil {
			return fmt.Errorf("更新收款状态失败：%v", err)
		}
		oldJSON, _ := json.Marshal(map[string]string{"status": old.Status})
		newJSON, _ := json.Marshal(map[string]string{
			"status": collectionSummarized, "summaryId": summaryID,
		})
		if err := store.WriteAudit(tx, "collection", id, "summary", string(oldJSON), string(newJSON), operator, ""); err != nil {
			return fmt.Errorf("写操作记录失败：%v", err)
		}
		if err := store.WriteAudit(tx, "data_summary", summaryID, "generate", "",
			fmt.Sprintf(`{"date":%q,"collectionId":%q,"incomeAmount":%q}`, old.Date, old.ID, old.Amount),
			operator, "店面收款生成汇总"); err != nil {
			return fmt.Errorf("写汇总操作记录失败：%v", err)
		}
		ref = CollectionSummaryRef{CollectionID: id, SummaryID: summaryID, Status: collectionSummarized}
		return nil
	})
	if err != nil {
		return CollectionSummaryRef{}, err
	}
	return ref, nil
}

// Recon 通道对账视图：非银通道以「已生成汇总」核对，对公通道按金额匹配银行转入。
// 差异与未匹配项进入待处理队列，不影响其他记录（对齐业务文档 §6.4 的不阻塞原则）。
func (s *CollectionService) Recon(ctx context.Context, f CollectionFilter) (CollectionRecon, error) {
	from := strings.TrimSpace(f.DateFrom)
	to := strings.TrimSpace(f.DateTo)

	where := []string{"1=1"}
	args := []any{}
	if from != "" {
		where = append(where, "c.collect_date>=?")
		args = append(args, from)
	}
	if to != "" {
		where = append(where, "c.collect_date<=?")
		args = append(args, to)
	}
	rows, err := s.DB.Pool().QueryContext(ctx,
		collectionSelect+" WHERE "+strings.Join(where, " AND ")+
			" ORDER BY c.collect_date, c.id", args...)
	if err != nil {
		return CollectionRecon{}, fmt.Errorf("查询店面收款失败：%v", err)
	}
	collections := []Collection{}
	for rows.Next() {
		c, err := scanCollection(rows)
		if err != nil {
			rows.Close()
			return CollectionRecon{}, fmt.Errorf("读取店面收款失败：%v", err)
		}
		collections = append(collections, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return CollectionRecon{}, fmt.Errorf("读取店面收款失败：%v", err)
	}

	nonBankReg := sdec.Zero
	corpReg := sdec.Zero
	nonBankCount := 0
	corpCount := 0
	corpCollections := []Collection{}
	pending := []CollectionPending{}
	for _, c := range collections {
		if c.Status == collectionVoided {
			continue
		}
		amt, err := decimal.Parse(c.Amount)
		if err != nil {
			continue
		}
		switch c.Account {
		case "cash", "personal":
			nonBankReg = nonBankReg.Add(amt)
			nonBankCount++
			if c.Status == collectionConfirmed {
				pending = append(pending, CollectionPending{
					ID:      c.ID,
					Channel: "nonbank",
					Title: fmt.Sprintf("%s ¥%s 待生成汇总",
						collectionPayMethodLabel(c.PayMethod), decimal.Format(amt)),
					Meta:   fmt.Sprintf("%s · %s · %s", c.Date, c.Customer, c.Store),
					Amount: "待补录",
				})
			}
		case "corporate":
			corpReg = corpReg.Add(amt)
			corpCount++
			corpCollections = append(corpCollections, c)
		}
	}

	// 非银通道对账合计 = 已生成汇总的收款（经 summary_id 关联）实际收入合计。
	summaryWhere := []string{"c.status=?", "c.account IN ('cash','personal')", "c.summary_id IS NOT NULL"}
	summaryArgs := []any{collectionSummarized}
	if from != "" {
		summaryWhere = append(summaryWhere, "c.collect_date>=?")
		summaryArgs = append(summaryArgs, from)
	}
	if to != "" {
		summaryWhere = append(summaryWhere, "c.collect_date<=?")
		summaryArgs = append(summaryArgs, to)
	}
	srows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT s.income_amount FROM data_summaries s
		 JOIN collections c ON c.summary_id = s.id
		 WHERE `+strings.Join(summaryWhere, " AND "), summaryArgs...)
	if err != nil {
		return CollectionRecon{}, fmt.Errorf("汇总已生成汇总的收款失败：%v", err)
	}
	nonBankRecon := sdec.Zero
	nonBankSummarized := 0
	for srows.Next() {
		var v string
		if err := srows.Scan(&v); err != nil {
			srows.Close()
			return CollectionRecon{}, fmt.Errorf("读取收款汇总失败：%v", err)
		}
		if amt, err := decimal.Parse(v); err == nil {
			nonBankRecon = nonBankRecon.Add(amt)
		}
		nonBankSummarized++
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return CollectionRecon{}, fmt.Errorf("读取收款汇总失败：%v", err)
	}

	// 对公通道：按金额 + 收款日后 7 天窗口贪心匹配银行转入（一笔银行流水只匹配一笔收款）。
	corpRecon := sdec.Zero
	matchedCount := 0
	if len(corpCollections) > 0 {
		type bankIn struct {
			amount sdec.Decimal
			date   string
			used   bool
		}
		bankWhere := []string{"in_amount<>''", "in_amount<>'0'", "in_amount<>'0.00'"}
		bankArgs := []any{}
		if from != "" {
			bankWhere = append(bankWhere, "substr(txn_time,1,10)>=?")
			bankArgs = append(bankArgs, from)
		}
		bankEnd := ""
		if to != "" {
			if d, err := time.Parse("2006-01-02", to); err == nil {
				bankEnd = d.AddDate(0, 0, 7).Format("2006-01-02")
				bankWhere = append(bankWhere, "substr(txn_time,1,10)<=?")
				bankArgs = append(bankArgs, bankEnd)
			}
		}
		brows, err := s.DB.Pool().QueryContext(ctx,
			`SELECT in_amount, substr(txn_time,1,10) FROM bank_flows WHERE `+
				strings.Join(bankWhere, " AND ")+` ORDER BY txn_time`, bankArgs...)
		if err != nil {
			return CollectionRecon{}, fmt.Errorf("读取银行流水失败：%v", err)
		}
		banks := []bankIn{}
		for brows.Next() {
			var amtStr, date string
			if err := brows.Scan(&amtStr, &date); err != nil {
				brows.Close()
				return CollectionRecon{}, fmt.Errorf("读取银行流水失败：%v", err)
			}
			amt, err := decimal.Parse(amtStr)
			if err != nil || !amt.GreaterThan(sdec.Zero) {
				continue
			}
			banks = append(banks, bankIn{amount: amt, date: date})
		}
		brows.Close()
		if err := brows.Err(); err != nil {
			return CollectionRecon{}, fmt.Errorf("读取银行流水失败：%v", err)
		}

		for _, c := range corpCollections {
			amt, err := decimal.Parse(c.Amount)
			if err != nil {
				continue
			}
			found := -1
			for i := range banks {
				if banks[i].used || !banks[i].amount.Equal(amt) {
					continue
				}
				if banks[i].date < c.Date {
					continue
				}
				if found == -1 || banks[i].date < banks[found].date {
					found = i
				}
			}
			if found >= 0 {
				banks[found].used = true
				corpRecon = corpRecon.Add(amt)
				matchedCount++
				continue
			}
			pending = append(pending, CollectionPending{
				ID:      c.ID,
				Channel: "corporate",
				Title:   fmt.Sprintf("对公转账 ¥%s 未匹配银行流水", decimal.Format(amt)),
				Meta:    fmt.Sprintf("%s · %s · %s", c.Date, c.Customer, c.Store),
				Amount:  "待匹配",
			})
		}
	}

	nonBankDiff := nonBankReg.Sub(nonBankRecon)
	corpDiff := corpReg.Sub(corpRecon)

	nonBankStatus := "matched"
	if !nonBankDiff.IsZero() {
		nonBankStatus = "pending"
	}
	for _, p := range pending {
		if p.Channel == "nonbank" {
			nonBankStatus = "pending"
		}
	}
	corpStatus := "matched"
	if !corpDiff.IsZero() {
		corpStatus = "pending"
	}

	nonBankNote := fmt.Sprintf("登记 %d 笔；已生成汇总 %d 笔，登记合计与汇总合计差 %s。",
		nonBankCount, nonBankSummarized, decimal.Format(nonBankDiff))
	corpNote := fmt.Sprintf("登记 %d 笔；匹配银行到账 %d 笔，差额 %s 进入待处理队列。",
		corpCount, matchedCount, decimal.Format(corpDiff))
	if corpCount == 0 {
		corpNote = "本期无对公转账收款。"
	}

	return CollectionRecon{
		DateFrom: from,
		DateTo:   to,
		Channels: []CollectionChannel{
			{
				Key:           "nonbank",
				Name:          "店面非银通道（现金 + 个人码）",
				RegisterTotal: decimal.Format(nonBankReg),
				RegisterCount: nonBankCount,
				ReconTotal:    decimal.Format(nonBankRecon),
				Diff:          decimal.Format(nonBankDiff),
				Status:        nonBankStatus,
				Note:          nonBankNote,
			},
			{
				Key:           "corporate",
				Name:          "对公转账通道",
				RegisterTotal: decimal.Format(corpReg),
				RegisterCount: corpCount,
				ReconTotal:    decimal.Format(corpRecon),
				Diff:          decimal.Format(corpDiff),
				Status:        corpStatus,
				Note:          corpNote,
			},
		},
		Pending:      pending,
		PendingCount: len(pending),
	}, nil
}

// ensureCollectionReceiptUnique 收据号唯一：空号不占号，排除自身后不得重复。
// 数据库侧另有部分唯一索引兜底（upgrade/04-collections.sql）。
func ensureCollectionReceiptUnique(tx *sql.Tx, receiptNo, selfID string) error {
	no := strings.TrimSpace(receiptNo)
	if no == "" {
		return nil
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM collections WHERE receipt_no=? AND id<>?`,
		no, selfID).Scan(&n); err != nil {
		return fmt.Errorf("校验收据号失败：%v", err)
	}
	if n > 0 {
		return fmt.Errorf("收据号已存在：%s", no)
	}
	return nil
}

// nextCollectionID 生成收款单号 SCYYMMDDNNN（当日序号递增），冲突时顺延。
func nextCollectionID(tx *sql.Tx, date string) (string, error) {
	prefix := "SC" + date[2:4] + date[5:7] + date[8:10]
	var maxID sql.NullString
	if err := tx.QueryRow(`SELECT MAX(id) FROM collections WHERE id LIKE ?`, prefix+"%").
		Scan(&maxID); err != nil {
		return "", fmt.Errorf("生成收款单号失败：%v", err)
	}
	seq := 1
	if maxID.Valid && len(maxID.String) > len(prefix) {
		if n, err := strconv.Atoi(maxID.String[len(prefix):]); err == nil {
			seq = n + 1
		}
	}
	for {
		id := fmt.Sprintf("%s%03d", prefix, seq)
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM collections WHERE id=?`, id).Scan(&n); err != nil {
			return "", fmt.Errorf("生成收款单号失败：%v", err)
		}
		if n == 0 {
			return id, nil
		}
		seq++
	}
}

// reconcileCollectionAttachments 附件对账：保留 ID 命中的旧附件，插入新附件，删除未列出的旧附件。
func reconcileCollectionAttachments(tx *sql.Tx, collectionID string, items []CollectionAttachmentInput, now string) error {
	rows, err := tx.Query(`SELECT id FROM collection_attachments WHERE collection_id=?`, collectionID)
	if err != nil {
		return fmt.Errorf("读取收款附件失败：%v", err)
	}
	existing := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("读取收款附件失败：%v", err)
		}
		existing[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("读取收款附件失败：%v", err)
	}

	kept := map[string]bool{}
	for i, it := range items {
		if id := strings.TrimSpace(it.ID); id != "" {
			if !existing[id] {
				return fmt.Errorf("附件不存在或已删除：%s", id)
			}
			kept[id] = true
			if _, err := tx.Exec(`UPDATE collection_attachments SET sort_no=? WHERE id=?`, i, id); err != nil {
				return fmt.Errorf("更新附件排序失败：%v", err)
			}
			continue
		}
		prepared, err := prepareCollectionAttachment(it)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO collection_attachments
			(id, collection_id, file_name, ext, size_bytes, content, sort_no, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), collectionID, prepared.fileName, prepared.ext,
			prepared.size, prepared.content, i, now); err != nil {
			return fmt.Errorf("保存附件失败：%v", err)
		}
	}
	for id := range existing {
		if kept[id] {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM collection_attachments WHERE id=?`, id); err != nil {
			return fmt.Errorf("删除附件失败：%v", err)
		}
	}
	return nil
}

type preparedAttachment struct {
	fileName string
	ext      string
	size     int
	content  []byte
}

// prepareCollectionAttachment 校验附件格式 / 大小并解码 base64（允许 data: 前缀）。
func prepareCollectionAttachment(it CollectionAttachmentInput) (preparedAttachment, error) {
	name := strings.TrimSpace(it.FileName)
	if name == "" {
		name = "附件"
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if _, ok := collectionAttachmentMime[ext]; !ok {
		return preparedAttachment{}, fmt.Errorf("附件「%s」格式不支持（仅 jpg / png / webp / pdf）", name)
	}
	data := strings.TrimSpace(it.DataBase64)
	if data == "" {
		return preparedAttachment{}, fmt.Errorf("附件「%s」内容为空，请重新选择文件", name)
	}
	if strings.HasPrefix(data, "data:") {
		if idx := strings.Index(data, ","); idx >= 0 {
			data = data[idx+1:]
		}
	}
	content, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return preparedAttachment{}, fmt.Errorf("附件「%s」内容解码失败：%v", name, err)
	}
	if len(content) == 0 {
		return preparedAttachment{}, fmt.Errorf("附件「%s」内容为空，请重新选择文件", name)
	}
	if len(content) > maxCollectionAttachmentBytes {
		return preparedAttachment{}, fmt.Errorf("附件「%s」超过 10MB，请压缩后重试", name)
	}
	return preparedAttachment{fileName: name, ext: ext, size: len(content), content: content}, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
