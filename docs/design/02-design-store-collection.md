---
title: 店面收款信息模块设计方案
description: 补齐店面线下收款（现金/个人收款码/对公转账等）的登记、单据照片附件留存、核对与入账方案：字段、附件存储、状态机、权限、与对账/汇总/凭证的集成及分期实施
---

> 业务参考：`docs/business/00-overview-health-finance-flow.md`（以下简称"业务文档"）§4 数据来源、§5 流程、§6 勾稽与凭证、§8 数据模型、§10 审计备份。
> 关联方案：`docs/design/01-design-recon-center-usage.md`（对账中心使用方案）。
> 脱敏：本文示例客户/门店/账号一律为脱敏值（如 `1001****7575`），照片附件仅存放于本地库，不进入 `docs/`。

---

## 1. 背景与问题

现有三条数据来源覆盖的是**对公 POS 通道**：

| 来源 | 覆盖 |
|---|---|
| 银行流水 | 对公账户资金变动（含 POS 清算入账） |
| 订单流水 | 工行商户后台交易（微信/支付宝/刷卡经 POS） |
| 数据汇总 | 财务人工整理的凭证草稿 |

但门店前台还存在**不经过 POS 的收款**，目前无处登记：

- 现金收款；
- 微信/支付宝**个人收款码**（进个人账户，不进对公/POS）；
- 客户直接**转账到对公账户**（会在银行流水出现，但无业务凭证）；
- 储值卡消费、优惠券抵扣、抹零等其他。

这些收款若只停留在纸质收据/员工手机里，会导致：
1. 收入漏记，账实不符；
2. 数据汇总"实际收入"与订单/POS 对不上（口径缺失）；
3. 审计时无法提供单据凭证（业务文档 §10 可追溯性）。

因此新增 **店面收款（Store Collection）** 模块，作为第 4 类数据来源，**登记信息 + 留存单据照片**，并向数据汇总/凭证供数。

---

## 2. 目标与边界

**目标**
- 门店经手人可快速登记一笔线下收款，并**拍照/上传单据照片**作为凭证；
- 财务可审核、汇总、核对，形成可追溯链条：收款记录 → 附件 → 汇总 → 凭证；
- 金额用 `decimal`、日期 `YYYY-MM-DD`，与现有规范一致。

**边界（本模块不做）**
- 不做 OCR 自动识别单据文字（照片仅作为附件留存与人工查看）；
- 不做支付渠道自动对接（个人收款码无法 API 拉取）；
- 不替代原始表 `bank_flows` / `order_flows`，仍只读；
- 不做多门店多账套隔离（单账套 MVP，`store_name` 仅作标签）。

---

## 3. 数据字段设计

### 3.1 `store_collections`（主表）

| 字段 | 类型 | 说明 | 必填 |
|---|---|---|---|
| `id` | TEXT PK | UUID | ✅ |
| `collect_date` | TEXT | 收款日期 `YYYY-MM-DD`（收入归属日，与业务文档 §6.1 一致） | ✅ |
| `store_name` | TEXT | 门店名称（标签，如"锦绣健康管理-门店A"） | ✅ |
| `customer_name` | TEXT | 客户姓名（脱敏值） | |
| `customer_phone` | TEXT | 客户电话（掩码，如 `160****593`） | |
| `project` | TEXT | 服务项目/产品（产康/美容/身体护理/产品销售等，与汇总 `project` 对齐） | |
| `pay_method` | TEXT | 收款方式：`cash` / `wechat` / `alipay` / `bank_transfer` / `other` | ✅ |
| `received_account` | TEXT | 到账账户：`cash`（现金）/ `personal`（个人户）/ `corporate`（对公户） | ✅ |
| `amount` | TEXT | 收款金额，decimal 字符串（2 位） | ✅ |
| `receipt_no` | TEXT | 纸质收据编号（可空，空号不占用唯一约束） | |
| `invoiced` | INTEGER | 是否开票 0/1 | |
| `collector` | TEXT | 收款经手人（员工） | ✅ |
| `remark` | TEXT | 事由/备注 | |
| `source` | TEXT | `manual` / `import` | ✅(默认) |
| `status` | TEXT | `draft` / `confirmed` / `summarized` / `voided` | ✅(默认) |
| `summary_id` | TEXT | 生成的汇总行 ID（反查链） | |
| `voucher_id` | TEXT | 生成的凭证 ID（反查链） | |
| `created_by` | TEXT | 录入人 | |
| `confirmed_by` | TEXT | 审核人 | |
| `confirmed_at` | TEXT | 审核时间 RFC3339 | |
| `void_reason` | TEXT | 作废原因 | |
| `created_at` | TEXT | RFC3339 | ✅ |
| `updated_at` | TEXT | RFC3339 | ✅ |

索引：`idx_store_collections_date`、`idx_store_collections_status`、`idx_store_collections_pay_method`、`idx_store_collections_collector`；
`uq_store_collections_receipt_no` 部分唯一索引（`receipt_no <> ''`，同汇总收据号规则）。

### 3.2 校验规则（Service 层，decimal 严格）

- `amount > 0`，否则报"收款金额必须大于 0"；
- `collect_date` 必填且合法；
- `pay_method=bank_transfer` 时 `received_account` 必须为 `corporate`；`cash` 必须为 `cash`；`wechat/alipay` 通常为 `personal`（允许改）；
- `receipt_no` 非空时全局唯一（含已汇总/已作废行？作废行释放号）；
- 已 `confirmed` 的记录金额字段锁定，改金额需先驳回或作废后重录。

---

## 4. 单据照片附件方案

### 4.1 存储选型（**已定稿：账套目录外置 + 内容寻址**）

> 2026-10-21 修订：初版选择库内 BLOB（单文件自洽）；按"100 条/天 × 2 张照片"实测量级
> （未压缩约 150–290 GB/年），BLOB 会让全量备份、恢复、交接全部失效，改为外置文件。

| 方案 | 优点 | 缺点 | 结论 |
|---|---|---|---|
| A. SQLite BLOB | 单文件自洽、事务一致 | 库体积暴涨；全量备份 OOM/耗时；`UPDATE` 元数据会重写整条 BLOB | 已废弃（v5 及以前） |
| **B. 账套目录外置 + 内容寻址（采用）** | 库保持 MB 级；附件可 rsync 增量；交接包可校验；元数据更新不触碰内容 | 需引用计数与孤儿清理 | v6 起采用 |

**定稿**：附件内容放 `<账套目录>/attachments/YYYY/MM/<sha256>.<ext>`，库内只留
`sha256` + `rel_path`（相对账套目录，随账套整体搬迁）：

- **内容寻址**：同一张照片被多条记录引用只存一份（`idx_collection_attachments_sha256`）；
  删除时按引用计数清理，引用归零才删文件。
- **上传前压缩**：前端 canvas 重编码为 JPEG（长边 ≤ 2000px、质量 0.7，已足够小的图跳过），
  手机原图 3–4 MB → 200–400 KB；重编码后扩展名同步改 `jpg`，MIME 与内容一致。
- **旧库迁移**：开库前置 `PreMigrateAttachments` 把 BLOB 写盘并回填指纹，`06-attachment-files.sql`
  再重建表去掉 `content` 列（失败中止开库，不丢凭据）。
- **备份/交接**：备份仍是账套内文件拷贝；交接包已把 `attachments/` 与 `manifest.tsv` 一并打包。

### 4.2 表结构

```sql
CREATE TABLE store_collections ( ... );  -- §3.1

-- 实际落地（v6）：单表 + 外置文件，无 BLOB 表
CREATE TABLE collection_attachments (
    id            TEXT NOT NULL PRIMARY KEY,
    collection_id TEXT NOT NULL,
    file_name     TEXT NOT NULL,        -- 原始文件名（压缩后扩展名改为 jpg）
    ext           TEXT NOT NULL,        -- jpg / jpeg / png / webp / pdf（决定 MIME）
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    sha256        TEXT NOT NULL DEFAULT '',  -- 内容指纹 = 去重键
    rel_path      TEXT NOT NULL DEFAULT '',  -- attachments/YYYY/MM/<sha256>.<ext>（相对账套目录）
    sort_no       INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL
);
CREATE INDEX idx_collection_attachments_collection ON collection_attachments (collection_id);
CREATE INDEX idx_collection_attachments_sha256 ON collection_attachments (sha256);
```

若后续要支持"数据汇总行也可挂附件"（业务文档 §4.4「可选附件（截图）」），按同一形态新增
`summary_attachments` 并复用 `PutAttachmentFile` / `ReadAttachmentFile`（内容寻址天然跨表共享），
不必再引 BLOB 表。

### 4.3 上传/查看/删除

- **上传**：前端隐藏 `<input type="file" multiple accept="image/*,application/pdf">`（沿用 `Import.svelte` 既有模式，server 模式也能用）；
  - 前端 `ArrayBuffer` → base64（与 `Import.svelte` 读 XLSX 同思路）→ 调 `AttachmentService.AddBase64(ownerType, ownerId, fileName, mimeType, base64, operator)`；
  - 备选：`@wailsio/runtime` 的 `Dialogs.OpenFile` 取得路径 → `AddFromPaths`，适合大图，桌面模式更省 IPC；
- **限制**：单文件 ≤ 10 MB，单记录 ≤ 10 张；类型白名单 `image/jpeg|png|webp|application/pdf`；超限返回中文错误（"单张附件不能超过 10MB"）；
- **查看**：列表只返回元数据（`Attachment`），点击缩略图/文件名时按需调 `GetData(id)` 返回 base64 data URL；列表页绝不批量拉 BLOB；
- **删除**：需二次确认（`Confirm.svelte`），写 `audit_logs`（记录文件名/哈希，不写二进制内容）；`confirmed` 及以后状态的记录删除附件需审核权限；
- **哈希**：`sha256` 既是完整性校验依据，也是**跨记录去重键**；共享删除由引用计数处理，不会误删他人在用的文件。

### 4.4 缩略图

MVP 不做服务端缩略图（避免引入图像处理依赖）；前端用 `<img>` 限制展示尺寸。若后续照片过大影响体验，再评估用 Go 标准库 `image/jpeg` + `image/draw` 生成缩略图另存 `attachment_blobs`（增加一个 `kind` 字段区分原图/缩略图），仍不引第三方依赖。

---

## 5. 状态机与角色权限

### 5.1 状态流转

```
draft ──提交/审核──▶ confirmed ──生成汇总──▶ summarized
  ▲                     │                       │
  └──────驳回───────────┘                       │
  │                                             │
  └──────────── 作废（voided，留痕）◀───────────┘
                （summarized 作废需先作废汇总，见 §7）
```

- `draft`：录入中，可自由编辑/加删附件；
- `confirmed`：财务已核对单据与金额，锁定金额，可开始汇总；
- `summarized`：已生成数据汇总（`summary_id` 回写），终态；
- `voided`：作废，记录保留、附件保留、状态置灰，`summary_id` 为空；金额不计入合计。

> 与汇总状态机对齐：`confirmed` 对应"可入账"，`summarized` 对应汇总 `reviewed` 后由凭证模块继续。

### 5.2 权限（首版单机单用户，字段预留）

| 角色 | 权限 |
|---|---|
| 门店经手人 | 新增/编辑 `draft`、上传附件、提交 |
| 出纳/会计 | 查看全部、发起汇总 |
| 审核人 | `confirmed` 审核/驳回、作废、删除附件 |
| 管理员 | 全部 + 附件清理 |

---

## 6. 页面与交互

新增页面 `frontend/src/routes/Collection.svelte`，导航置于"订单流水"与"对账中心"之间：

```
导入中心 → 银行流水 → 订单流水 → 店面收款 → 对账中心 → 数据汇总 → 凭证管理 → 科目余额表 → 系统设置
```

### 6.1 列表区

- 顶部统计卡：**本期收款合计**、**待审核笔数/金额**、**按收款方式合计**（现金 / 个人 / 对公）、**未生成汇总笔数**；
- 筛选：日期区间、门店、收款方式、到账账户、经手人、状态、关键字（客户/收据号/事由）；
- 主表列：收款日期、门店、客户、项目、**收款方式**、到账账户、金额（右对齐等宽数字）、收据号、经手人、附件数、状态、操作；
- 状态色：`draft` 灰、`confirmed` 绿、`summarized` 蓝/绿、`voided` 灰删除线；
- 底部合计：按当前筛选求和。

### 6.2 录入/编辑抽屉或弹窗

- 表单字段见 §3.1；`pay_method` 与 `received_account` 联动（选择后自动推荐账户类型）；
- **附件区**：缩略图列表 + "添加照片"按钮 + 预览大图 + 删除（确认）；
- 金额用 `input.num-input`，前端仅格式化，校验在 Go；
- 保存为 `draft`；"保存并提交"直接进入待审核。

### 6.3 详情与核对

- 右侧详情显示完整信息 + 附件画廊；
- 审核人按钮：**确认收款** / **驳回**（填原因）；
- **生成汇总**：按日期+门店一键生成（见 §7.1），跳转数据汇总页。

### 6.4 导入（可选，P2）

支持 TSV/CSV/XLSX 批量导入历史收款记录（列：日期/门店/客户/项目/收款方式/到账账户/金额/收据号/经手人/备注），复用 `ParseAnyFile` + 列映射预览；附件按"收据号-文件名"约定批量匹配上传（P2 再评估）。

---

## 7. 与现有模块的集成

### 7.1 数据汇总（F5）

新增 `CollectionService.GenerateSummary(date, storeName, operator)`：
- 取该日期/门店 `status='confirmed'` 的行，逐行生成 `data_summaries`：
  - `category='收入'`、`project` 取记录的 `project`、`income_amount=amount`、`receipt_no` 沿用、
  - `source='store_collection'`（新增来源枚举）、`remark` 注明收款方式/门店/经手人；
  - 回写 `store_collections.summary_id`，状态置 `summarized`；
- 一行收款 → 一行汇总，保证"一收款一汇总"可反查；
- 汇总仍走 `draft → reviewed`，审核后才可生成凭证（业务文档 §6.5）。

### 7.2 对账中心（F4）扩展建议

现有三方勾稽仅覆盖 POS 通道；店面收款属**非 POS 收入通道**，不能直接并入 POS 等式，否则会误报差异。建议在业务文档确认后按通道拆分（详见 §11）：

- **POS 通道**（不变）：`Σ订单实收 = 银行 POS 转入`；
- **店面非银通道**（现金/个人）：`Σ店面收款(现金+个人) = Σ汇总中来源=store_collection 的收入`；
- **对公转账通道**：`Σ店面收款(对公转账) = 银行流水中对应的非 POS 转入`（按对方/摘要人工核对）。

对账中心页面可增加"店面收款"子表，展示按收款方式的登记合计 vs 汇总合计 vs（转账类）银行到账合计，差异进同一待处理队列。

### 7.3 凭证（F6）

店面收款来源的汇总，按收款方式进行分录（需业务文档确认科目）：

```
[现金收款]        借 库存现金(1001)                贷 主营业务收入-服务项目(6001)
[个人码/个人转账] 借 其他货币资金(1012)/银行存款    贷 主营业务收入-服务项目(6001)
[对公转账]        借 银行存款(1002)                贷 主营业务收入-服务项目(6001)
```

- 若个人码款项后续提现/转入对公，再做"提现/内部划转"凭证（借 银行存款 贷 其他货币资金），本模块不自动处理；
- 需在 `accounts` 种子中补 `1001 库存现金`、`1012 其他货币资金`（若采用）。凭证模板扩展须与业务文档 §6.5 同步。

### 7.4 备份/审计（F8、§10）

- 附件为库内 BLOB，现有 `BackupService.Backup/Restore` **不改代码**即含附件；
- 所有写操作（新增/编辑/审核/驳回/作废/生成汇总/附件增删）写 `audit_logs`（`entity_type='store_collection'` 或 `'attachment'`，old/new JSON，操作人）；
- 反查链：凭证 → 汇总 → 店面收款记录 → 附件。

---

## 8. 数据模型与迁移

- 新增升级脚本 `internal/store/upgrade/03-store-collections.sql`，首行头：
  `-- v3 -> v4 (compatible with v0+): Add store collections and attachments`
- 同步 `00-latest-schema.sql`：追加 `store_collections`、`attachments`、`attachment_blobs` 三表与索引；
- `internal/store/sqlite.go` 的 `SchemaVersion` 由 `3` 调整为 `4`；
- 遵循 `db-schema` 技能：`CREATE TABLE IF NOT EXISTS`、金额 `TEXT`、时间 `TEXT`、无破坏性操作；
- 旧库首次打开自动升级，验证行数/结构保留。

---

## 9. Service API 设计

```go
// CollectionService —— 店面收款登记与核对
type CollectionService struct{ DB *store.DB }

func (s *CollectionService) List(ctx context.Context, f CollectionFilter) (CollectionPage, error)
func (s *CollectionService) Get(ctx context.Context, id string) (StoreCollection, error)
func (s *CollectionService) Save(ctx context.Context, req SaveCollectionReq) (StoreCollection, error)
func (s *CollectionService) Confirm(ctx context.Context, id, operator string) (StoreCollection, error)
func (s *CollectionService) Reject(ctx context.Context, id, operator, reason string) (StoreCollection, error)
func (s *CollectionService) Void(ctx context.Context, id, operator, reason string) (StoreCollection, error)
func (s *CollectionService) GenerateSummary(ctx context.Context, date, storeName, operator string) ([]Summary, error)
func (s *CollectionService) Totals(ctx context.Context, f CollectionFilter) ([]CollectionTotal, error) // 按方式/账户汇总

// AttachmentService —— 通用附件（owner_type 区分业务）
type AttachmentService struct{ DB *store.DB }

func (s *AttachmentService) AddBase64(ctx context.Context, req AddAttachmentReq) (Attachment, error)
func (s *AttachmentService) AddFromPaths(ctx context.Context, ownerType, ownerID string, paths []string, operator string) ([]Attachment, error) // 备选
func (s *AttachmentService) List(ctx context.Context, ownerType, ownerID string) ([]Attachment, error)   // 仅元数据
func (s *AttachmentService) GetData(ctx context.Context, id string) (AttachmentData, error)              // 按需 base64
func (s *AttachmentService) Delete(ctx context.Context, id, operator string) error
```

约定：`context.Context` 首参、返回 `(T, error)`、错误中文可执行、金额 `decimal`；新增 Service 在 `internal/app/app.go` 注册。

对应前端绑定：`frontend/src/routes/Collection.svelte`、`frontend/src/lib/`（附件预览组件、收款方式中文与配色 helper）、`RouteKey` 增加 `collection`、`App.svelte` 导航与路由增加一项。

---

## 10. 安全、脱敏与隐私

- 照片附件仅存本地库，**不上传、不外发**；导出功能默认不含附件二进制（可选导出为本地 zip 需管理员确认）；
- 客户姓名/电话入库前套用 `docs/raw/anonymization_mapping.tsv` 或掩码规则；示例数据一律脱敏（如 `1001****7575`）；
- 照片可能含真实姓名/人脸：不写入 `docs/`、不参与脱敏 grep 例外之外的文本；仅在 `bin/`（gitignore）或用户本地库；
- 附件类型与大小白名单，防止把可执行文件当附件传入；
- 删除附件需审核权限 + 二次确认 + 审计留痕。

---

## 11. 业务文档待补充项（实现前必须先改文档）

按 AGENTS「业务文档优先」，落地前需在 `docs/business/00-overview-health-finance-flow.md` 增加/修改：

1. §4 数据来源新增 **4.4 店面收款**（字段、来源、与 POS 的区别）；
2. §5 流程在导入/流水后插入"店面收款"节点；
3. §6.4 三方勾稽扩展为**按通道核对**（POS / 店面非银 / 对公转账），明确各通道等式与差异处理；
4. §6.5 凭证模板新增现金/个人码/对公转账三类分录，并确认科目（1001/1012）；
5. §6.6 科目最小集补 `1001 库存现金`（及可选 `1012 其他货币资金`）；
6. §7 功能模块新增 **F9 店面收款**（验收：登记 N 笔 + 附件可预览可删除 + 审核 + 生成汇总）；
7. §8 数据模型补 `store_collections`、`attachments`、`attachment_blobs`。

---

## 12. 分期实施建议

### P0 — 登记与附件（本模块主体）
- 结构已在全量脚本 `00-latest-schema.sql`（历史增量已合并进基线，当前 `SchemaVersion=5`）；
- `CollectionService`（List/Get/Save/Confirm/Reject/Void/Totals）+ `AttachmentService`（AddBase64/List/GetData/Delete）；
- `Collection.svelte`：列表 + 录入弹窗 + 附件上传/预览/删除 + 审核；
- `App.svelte` / `types.ts` 导航与路由接线；注册 Service；重新生成绑定。

### P1 — 入账闭环
- `GenerateSummary` 一键生成汇总并回写 `summary_id`；
- 业务文档 §6.5 科目与凭证模板扩展 + `1001` 种子科目；
- 对账中心增加"店面收款"子表（按通道核对）。

### P2 — 增强
- 批量导入历史记录；
- 附件缩略图与导出打包；
- 大图走原生 `Dialogs.OpenFile` + `AddFromPaths` 降低 IPC；
- 附件外置磁盘的可选迁移（仅迁 `attachment_blobs`）。

---

## 13. 验收清单

- [ ] 新增店面收款一笔（现金/微信个人/对公转账各一），金额用 decimal 存储；
- [ ] 上传 jpg/png/pdf 各一张可预览、可删除（删除需确认 + 审计）；
- [ ] 超 10MB 或非白名单类型被拒并返回中文错误；
- [ ] `draft → confirmed → summarized` 流转正确，`confirmed` 后金额锁定，作废留痕；
- [ ] 收据号唯一（空号不占用）；
- [ ] 一键生成汇总，`summary_id` 回写，反查链凭证→汇总→收款→附件可通；
- [ ] 备份 `.db` 后恢复，附件仍在（验证 BLOB 自洽）；
- [ ] 对账/汇总合计与登记合计一致（按通道）；
- [ ] 脱敏：文档/界面/日志无真实姓名与原值，示例均掩码；
- [ ] `gofmt -l ./...`、`go vet -tags=gtk3 ./...`、`go build -tags=gtk3 .` 通过；前端 `npm run check --prefix frontend` 通过；
- [ ] 迁移：旧库打开自动升到 v4，原行数保留；`git diff` 不改历史脚本。
