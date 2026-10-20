---
title: 健康管理公司简易财务处理业务文档
description: finlite 单账套业务唯一依据：银行流水 + 订单流水 + 数据汇总 → 对账 → 凭证 → 余额表的流程与规则
---

> 项目：`finlite`（Go + Wails3）| 参考基线：`ForgeFin`（Rust + Tauri + Leptos）`docs/analysis/*`（外部只读参考，无需复制到本仓库，见附录 C）
> 样本企业：锦绣健康管理有限公司（示例账套，真实名称已脱敏；示例商户号 `1001****7575`）| 本地样本：`../raw/*.tsv`（已按映射脱敏，可直接用于测试；另有 `pos流水.tsv` 为订单子集备查）
> 目标：用最小闭环跑通“银行流水 + 订单流水 + 数据汇总 → 对账 → 凭证”，让财务每天省 1~2 小时。
>
> 脱敏声明：本文档禁止出现真实公司名称和人员姓名，一律使用 `../raw/anonymization_mapping.tsv` 中的脱敏后名称。账号、商户号、电话仅保留掩码示例。后续 raw 数据转换直接使用该映射表。

---

## 1. 背景与目标

### 1.1 现状痛点

财务每天重复做：

```
打开银行网银 → 导出银行流水 → 打开工行商户后台 → 导出订单流水 →
打开微信群抄备注/事由 → 人工整理 Excel 数据汇总 → 手工录凭证 → 对余额 → 出表
```

耗时点不在会计知识，而在复制粘贴、找数据、核对金额、补备注。

### 1.2 本文档目标

1. 把健康管理这一家公司的真实流程固化为 **一套简易、可落地的业务规则**；
2. 作为 `finlite` Go 后端 + Svelte 前端的 **唯一业务依据**，后续需求/测试/验收都回溯到此；
3. 刻意做减法：首版只做单账套、固定表结构，不做 ForgeFin 的动态元数据模型（`source_types/source_fields`）和多公司隔离。

### 1.3 与 ForgeFin 的关系

| ForgeFin（参考） | finlite（本项目） |
|---|---|
| 多公司 `.db` 隔离、`source_records + raw_data JSON` 动态模型 | 单公司单库 `finlite.db`，`bank_flows / order_flows / data_summaries` 三张固定表 |
| Tauri command + `company_id` 路由 | Wails3 Service 直接调用，无 `company_id` |
| 完整凭证/总账/报表/税务/AI 路线图 | MVP 只到“导入→对账→汇总→凭证→余额表” |
| 余额校验 + 批量确认已实现 | 直接复用其规则（§6.3），Go 重写 |

---

## 2. 术语表

| 术语 | 说明 |
|---|---|
| 银行流水 | 对公账户资金实际变动，T+1 次日可导出，权威余额来源 |
| 订单流水 | 工行商户后台导出，交易级，含订单金额/手续费/实收金额，是收入确认主依据 |
| 数据汇总 | 财务人工整理的凭证草稿，事由/备注来自微信群，是生成凭证的直接依据 |
| POS 清算 | 银行将单日多笔订单合并为一笔转入，附言多为示例商户号（已脱敏） |
| T+1 清算 | 订单交易日 D，银行入账 D+1，结算日期以订单流水 `结算日期` 为准 |
| 三方勾稽 | 订单实收合计 vs 银行 POS 转入 vs 汇总实际收入，三者对上才可制单 |
| 收据编号 | 收入类汇总行的人工编号（如 `0000502`），支出多为空 |
| 凭证 | 正式会计凭证，`借=贷`，一经审核不可直接改，只能反审核/冲销 |

---

## 3. 角色与职责

| 角色 | 职责 | 系统权限 |
|---|---|---|
| 出纳/会计（制单人） | 导入文件、补备注、做汇总、生成凭证 | 导入、新增/编辑汇总、生成凭证 |
| 审核人（可兼任） | 确认余额批次、审核汇总、审核凭证 | 确认余额、审核/反审核、审核日志查看 |
| 管理员 | 科目维护、费率维护、备份恢复 | 全部 + 备份恢复 |

首版不做登录多用户并发控制，单机单用户，预留 `created_by / reviewed_by` 字段。

---

## 4. 数据来源详解

### 4.1 银行流水 `银行流水.tsv`

网银次日导出，TSV，10 列。示例行见 `../raw/银行流水.tsv:2-21`。

| 列 | 业务含义 | 质量 | 备注 |
|---|---|---|---|
| 凭证号 | 恒为 `000000000` | 不可用 | 忽略 |
| 对方账号 | 对方卡号/内部户 | 中 | 手续费行为空 |
| 交易时间 | 精确到秒 | 高 | 排序基准 |
| 对方单位 | 转入/转出方 | 中 | POS 清算为 `待清算商户银行卡POS净收暂收销账户` |
| 用途/摘要 | 交易类型 | 高 | `货款/报销/跨行汇款手续费/拨款` |
| 附言 | POS 为示例商户号（已脱敏） | 中 | 附言为示例商户号即判 POS 清算 |
| 转入金额 | 收入 | 高 | Decimal(18,2) |
| 转出金额 | 支出 | 高 | 同上 |
| 余额 | 交易后余额 | 高 | **唯一自洽校验字段**，必须原样保留 |

业务特征：POS 收入合并入账、手续费单独转出、支出多为货款/报销/房费。

### 4.2 订单流水 `订单流水.tsv`

工行商户后台导出，40+ 列，交易级最全。示例见 `../raw/订单流水.tsv:2-14`。

核心字段（其余原样存 `raw_json` 备查）：

| 列 | 用途 |
|---|---|
| 工行订单号 | 主键，去重依据 |
| 订单金额 | 贷方收入原值 |
| 手续费金额 | 借方财务费用 |
| 商户实收金额 | 借方银行存款，恒有 `订单金额-手续费=实收`（允许 ±0.01） |
| 交易时间 / 结算日期 | 归属日期按 `结算日期`，核对按 `交易日期+1=结算日期` |
| 支付方式 `微信/支付宝`、借贷记 `借记卡/贷记卡` | 费率核验 + 统计 |
| 第三方订单号 | 微信/支付宝号，备查 |
| 客户备注 | 多为空，个别有值（如吴佳怡），不可依赖 |
| 商户补贴/消费立减/银行补贴 | 差异分析线索（如 7-01 差 0.45 元） |

### 4.3 数据汇总 `数据汇总.tsv`

财务人工 Excel，12 行样本见 `../raw/数据汇总.tsv:1-12`。

| 列 | 说明 |
|---|---|
| 日期 | 记账日期，多取银行交易日 |
| 收据编号 | 收入有号，支出为空 |
| 一级 `收入/支出`、项目 `营业收入/手续费/产品成本/护理成本/水电费` | 决定凭证科目方向 |
| 事由 | 如 `王浩然产康充值/赵雨桐牛奶光/2026.6房费`，来自微信群（示例已脱敏） |
| 借记卡/信用卡/微信支付宝金额、手续费、实际收入、支出、余额 | 收入行填前三者，支出行填支出 |
| 备注 | 对方单位/清算户 |

第二行为费率声明行：`0.005 / 0.006 / 0.0025`，导入时跳过。

### 4.4 微信群备注（非结构化）

来源不稳定，系统不做自动抓取。汇总表提供 `备注/事由` 可编辑 + 可选附件（截图），`来源=manual` 标记。

---

## 5. 端到端流程

```
[1 收集] 银行网银导出银行流水(TSV) + 工行商户导出订单流水(TSV)
            ↓
[2 导入] 导入中心选文件或扫描默认原始目录 → 解析 → 去重 → 余额校验 → 入库(import_batch)
            ↓
[3 对账] 对账中心：订单(按结算日汇总实收) vs 银行(POS转入) 自动匹配，差异进待处理
            ↓
[4 汇总] 数据汇总：自动预填(由对账结果生成草稿) + 人工补事由/备注/收据号
            ↓
[5 凭证] 由已审核汇总一键生成凭证（借贷自动配平）→ 凭证审核 → 登账
            ↓
[6 报表] 科目余额表 / 收支日报（首版仅此两张）
```

页面映射（Svelte）：导入中心 → 银行流水 → 订单流水 → 店面收款 → 对账中心 → 数据汇总 → 凭证管理 → 科目余额表 → 系统设置（科目/费率/备份）。

并行支线（F9 店面收款，线下收款不经过 POS）：

```
[1 登记] 门店经手人录入收款（现金 / 个人码 / 对公转账）+ 单据照片附件
            ↓
[2 审核] 出纳/会计确认收款（或驳回草稿），可作废
            ↓
[3 汇总] 生成数据汇总草稿（source='collection'，一收款一汇总）
            ↓
[4 制单] 与其他汇总一致：审核 → 生成凭证 → 科目余额表
```

店面收款汇总单独走「通道对账」核对（现金/个人码对汇总，对公转账对银行到账），
不参与订单 POS 三方勾稽（见 §6.4 / §6.7）。

状态机：

```
汇总: draft → reviewed → vouchered；可驳回 reviewed→draft（vouchered 不可驳回）
凭证: draft → audited；反审核 audited→draft（留日志）
银行余额批次: mismatch --确认--> confirmed（可撤销）
```

生成凭证时同步回写 `data_summaries.voucher_id`；反查链：凭证 → 汇总 → 银行/订单行 → 文件+行号。

---

## 6. 核心业务规则

### 6.1 T+1 与归属

- 收入归属日期 = 订单 `结算日期`，不是交易时间。
- 银行 POS 入账日期 = 结算日期当日凌晨（如订单 7-09 → 银行 7-10 02:45 `11970.00`）。
- 跨日查询必须按结算日期聚合，否则永远对不上。

### 6.2 手续费率

| 支付方式/卡种 | 费率 |
|---|---|
| 借记卡 | 0.5% |
| 贷记卡 | 0.6% |
| 微信/支付宝 | 0.25% |

规则（复用 ForgeFin 外部基线 §5.7，结论已内化如下，无需复制原文）：

1. 系统按 `订单金额×费率` 预填，保留 2 位四舍五入；
2. 允许手工改，改后置 `fee_manual_adjusted=1`，列表黄标 `费` + 提供“仅看修正”筛选；
3. 样本多为微信 0.25%（如 `12000→30.00`），借记/贷记列在汇总表常为空，因都走微信/支付宝通道——费率判断以订单流水为准，不以汇总列名。

### 6.3 银行余额校验（强制，直接复用 ForgeFin）

- **校验顺序**：网银导出的行序不保证（样本 `银行流水.tsv` 为倒序，且同一秒多行如 7-10 13:39:14 的 4,800/717/149 也是乱序），校验前必须重排：先按 `交易时间` 升序；同一秒内按余额衔接关系（`上行余额 + 转入 - 转出 == 本行余额`）重建真实先后，避免把实际连续的行误判为不平。衔接关系无法唯一确定时（起点不唯一或存在无衔接行），退回文件原序并如实标不平。
- 逐行：`本行余额 == 上行余额 + 转入 - 转出`（首行无上行，直接 `ok`），`shopspring/decimal` 严格相等，**无容差**；
- 整批：`末行余额 - 首行余额 == Σ(本行转入 - 本行转出) 之和（自第 2 行起）`。因递推式首行净额已被 `首行余额` 吸收，**Σ 必须排除首行**，否则恒误报；不平则 `balance_check_warning`；
- 不平仍入库，写 `import_errors(field='balance_discontinuity')`，UI 红底 `mismatch` / 灰 `skip` / 绿 `ok`；
- 唯一消红路径：按批次人工“确认余额”（写 `balance_confirmed_at`），可撤销；确认不改 `status`，仅换样式。结论来源见外部基线 `ForgeFin/docs/analysis/health-source-data-analysis.md:105-117`（外部只读，含原始商户号原值，禁止原样复制）。

### 6.4 三方勾稽

- 日粒度：`订单(结算日= D) Σ实收 == 银行(D) POS转入 == 汇总(D) Σ实际收入`。
- 已验证匹配例：7-11 `446.88`、7-10 `9256.80`、7-09 `11970.00` 全对上；7-01 差 0.45 元进待处理（疑补贴/立减）。
- 未匹配行单独列示，不阻止其他行制单。
- 店面收款生成的汇总行 `source='collection'` **不参与**本三方勾稽：现金/个人码收款不进银行，混入会使当日汇总收入虚高、误报差异；该部分由 §6.7 通道对账单独核对。

### 6.5 凭证生成模板（首版 5 类）

```
[POS收入] 借 银行存款- POS清算户(实收)  借 财务费用-手续费  贷 主营业务收入-服务项目(订单金额)
  例: 借 银行存款 11970.00 / 借 财务费用 30.00 / 贷 主营业务收入-产康 12000.00

[产品成本] 借 主营业务成本-产品成本  贷 银行存款
  例: 4800.00 货款→明瑞科技有限公司

[护理/人力报销] 借 管理费用-职工福利费/护理成本  贷 银行存款
  例: 717.00+149.00 →李娜娜

[手续费] 借 财务费用-手续费  贷 银行存款
  例: 31.50 / 9.00 跨行汇款手续费

[房租水电] 借 管理费用-水电费/房租  贷 银行存款
  例: 4552.00 →安泊酒店管理公司
```

通用约束：有借必有贷、借贷必相等；汇总未审核不可生成；一笔汇总只生成一张凭证（重生成先冲销）。

### 6.6 会计科目（首版最小集）

```
1002 银行存款（-POS清算户辅助）
6001 主营业务收入（-产康/美容/身体护理/产品销售辅助）
6401 主营业务成本（-产品成本）
6601 销售费用 / 6602 管理费用（-职工福利费/水电费/房租）
6603 财务费用（-手续费）
```

科目为树形 `code/name/parent_id/type`，首版内置导入，不做完整准则科目表。

---
### 6.7 店面收款与通道对账（F9）

- 店面收款是**线下登记台账**，与银行/订单原始表无关，页面可增删改（原始表只读约束不适用于本模块）。
- 状态机：`draft`（门店录入）→ `confirmed`（出纳/会计确认）→ `summarized`（已生成数据汇总）；`draft`/`confirmed` 可作废 `voided`。已汇总记录不可编辑/作废，需先在数据汇总中作废关联汇总。
- 金额 `TEXT` + `shopspring/decimal`，必须大于 0，展示 2 位；日期 `YYYY-MM-DD`。
- 收款方式 → 默认到账账户自动推荐：现金→现金账户、微信/支付宝个人码→个人收款账户、对公转账→对公银行账户；可手工改。
- 收据号在 `collections` 内唯一（空号不占号）；一收款一汇总（`collections.summary_id` 唯一关联），重复生成直接拒绝。
- 生成汇总写入 `data_summaries`：`source='collection'`、`status='draft'`、项目「营业收入」、实际收入=收款金额、收据号沿用收款收据号（与既有汇总冲突时留空，避免阻断生成）。
- 通道对账（按收款日期区间）：
  - 非银通道（现金 + 个人码）：登记合计 vs 已生成汇总合计；差额与「已确认未生成汇总」进待处理队列。
  - 对公通道：登记合计 vs 银行流水转**入**合计；按「金额相等 + 收款日 ≤ 银行到账日 ≤ 收款日+7 天」贪心匹配，一笔银行流水只匹配一笔收款；未匹配收款进待处理队列。
  - 差异与未匹配项不阻止其他记录制单（与 §6.4 同原则）。
- 附件：jpg/png/webp/pdf，单张 ≤ 10MB，单条 ≤ 10 张，内容存 SQLite BLOB，保证 `finlite.db` 单文件备份完整。
- 所有状态变更（创建/提交/修改/确认/驳回/作废/生成汇总）写 `audit_logs`，详情页展示操作记录。


## 7. 功能模块（MVP 清单）

| # | 模块 | 功能 | 验收 |
|---|---|---|---|
| F1 | 导入中心 | 选 TSV/CSV/XLSX 或扫描默认原始目录批量导入，预览列映射，导入银行/订单，返回成功/重复/失败数+余额警告，可选导入后归档 | 样本文件一次导入成功，去重按工行订单号+银行(时间+金额+对方) |
| F2 | 银行流水 | 列表（日期/对方/转入/转出/余额三色/确认态）、批次确认/撤销、按日期/关键字筛 | 不平红底，确认后变绿，撤销恢复红 |
| F3 | 订单流水 | 列表（订单号/金额/手续费/实收/支付方式/结算日）、费率复算差异提示 | 手续费 ±0.01 外标异 |
| F4 | 对账中心 | 按结算日自动勾稽三方，匹配/差异两栏，差异一键定位原行 | 7-10/11/12 全绿，7-01 标差异 0.45 |
| F5 | 数据汇总 | CRUD，自动预填，收据号唯一，手工费标记黄标，审核/驳回 | 未审核不可生成凭证 |
| F6 | 凭证管理 | 列表/详情/按 §6.5 模板由已审核汇总一键生成、审核/反审核、打印预览 | 借贷不等禁止保存 |
| F7 | 科目余额表 | 期初+本期借贷+期末，按期间筛 | 期末=期初+借-贷（按方向） |
| F8 | 基础设置 | 科目维护、费率维护、备份/恢复（sqlite `.backup` 级拷贝） | 备份文件可恢复打开 |
| F9 | 店面收款 | 收款登记（门店/客户/项目/方式/账户/金额/收据号/经手人/开票）、单据照片附件、草稿→确认→生成汇总状态机、作废/驳回、通道对账视图与待处理队列 | 状态流转留审计日志；一收款一汇总；通道差异进队列不阻塞 |

非目标（明确不做）：多账套、OCR、AI、税务申报、固定资产、应收账龄、微信自动同步。

---

## 8. 数据模型（SQLite 单库 `finlite.db`）

> Go 用 `modernc.org/sqlite`（纯 Go，免 CGO）+ `shopspring/decimal`；金额存 `TEXT`（decimal 字符串），时间存 `RFC3339` 文本。

```sql
import_batches(id TEXT PK, source_type TEXT, file_name TEXT, row_count INT,
  success_count INT, skip_count INT, balance_warning TEXT, imported_at TEXT);
import_errors(id TEXT PK, batch_id TEXT, row_no INT, field_name TEXT, message TEXT);

bank_flows(id TEXT PK, batch_id TEXT, file_name TEXT, row_no INT,
  txn_time TEXT, counterparty TEXT, summary TEXT, remark TEXT,
  in_amount TEXT, out_amount TEXT, balance TEXT,
  balance_status TEXT, -- ok/mismatch/skip
  balance_confirmed_at TEXT, voucher_id TEXT, created_at TEXT);

order_flows(id TEXT PK, batch_id TEXT, icbc_order_no TEXT UNIQUE, third_order_no TEXT,
  txn_time TEXT, settle_date TEXT, order_amount TEXT, fee_amount TEXT, net_amount TEXT,
  pay_method TEXT, debit_credit_flag TEXT, voucher_id TEXT, raw_json TEXT, created_at TEXT);

data_summaries(id TEXT PK, date TEXT, receipt_no TEXT, category TEXT, project TEXT,
  reason TEXT, income_amount TEXT, expense_amount TEXT, fee_auto TEXT, fee_actual TEXT,
  fee_manual_adjusted INT DEFAULT 0, balance TEXT, remark TEXT,
  source TEXT, -- auto/manual
  status TEXT DEFAULT 'draft', voucher_id TEXT, created_at TEXT, updated_at TEXT);

vouchers(id TEXT PK, voucher_date TEXT, voucher_no TEXT UNIQUE,
  summary TEXT, status TEXT DEFAULT 'draft', source_summary_id TEXT, created_at TEXT);
voucher_entries(id TEXT PK, voucher_id TEXT, account_code TEXT, account_name TEXT,
  debit TEXT, credit TEXT, remark TEXT, sort_no INT);

accounts(code TEXT PK, name TEXT, parent_code TEXT, account_type TEXT, is_active INT DEFAULT 1);
fee_rates(pay_method TEXT PK, rate TEXT, updated_at TEXT);
audit_logs(id TEXT PK, entity_type TEXT, entity_id TEXT, action TEXT,
  old_values TEXT, new_values TEXT, operator TEXT, operated_at TEXT, remark TEXT);

-- 本地界面偏好（当前仅导入中心 raw_dir）
app_settings(key TEXT PK, value TEXT, updated_at TEXT);

-- 店面收款登记（F9）
collections(id TEXT PK, collect_date TEXT, store TEXT, customer TEXT, phone TEXT,
  project TEXT, pay_method TEXT, -- cash/wechat/alipay/bank_transfer/other
  account TEXT, -- cash/personal/corporate
  amount TEXT, receipt_no TEXT,
  operator TEXT, invoiced INT DEFAULT 0,
  status TEXT DEFAULT 'draft', -- draft/confirmed/summarized/voided
  void_reason TEXT, summary_id TEXT, note TEXT, created_at TEXT, updated_at TEXT);

-- 收款单据附件（内容存 BLOB，与主库同文件）
collection_attachments(id TEXT PK, collection_id TEXT, file_name TEXT, ext TEXT,
  size_bytes INT DEFAULT 0, content BLOB, sort_no INT DEFAULT 0, created_at TEXT);
```

索引：`bank_flows(txn_time)`、`order_flows(settle_date, icbc_order_no)`、`data_summaries(date, status)`、`vouchers(voucher_date)`、`collections(collect_date, status)`、`collection_attachments(collection_id)`；`collections.receipt_no` 部分唯一（空串不占号）。

---

## 9. Go + Wails3 技术映射

```
finlite/
  main.go（embed frontend/dist，仅传 FS）
  internal/
    app/app.go（Wails Application + Window + Service 注册）
    services/
      ImportService（解析 TSV/CSV/XLSX、去重、余额校验、写 batch+errors）
      BankFlowService（List/Get/ConfirmBatch/UnconfirmBatch）
      OrderFlowService（List/Get/FeeCheck）
      ReconService（ReconcileByDate、ListDiffs）
      SummaryService（CRUD/Audit/预填 GenerateFromRecon）
      VoucherService（Create/List/Get/Audit/UnAudit，配平检查）
      AccountService / FeeRateService / BackupService
      ReportService（BalanceSheet lite、日报）
    store/sqlite.go（open/migrate，WAL，single writer mutex）
    decimal/money.go（shopspring/decimal 封装：parse/format/add/sub/cmp）
  frontend/src/（Svelte5：routes 对应 §5 页面，bindings 调用 Service）
  docs/（本文 + 后续 测试方案/导入列映射表）
```

Wails3 约定：Service 方法 `context.Context` 首参可选，返回 `(T, error)`，前端 `bindings` 自动生成；错误中文直接面向财务（如“借贷不平：借 12000.00 ≠ 贷 11970.00”）。

---

## 10. 审计、备份与可追溯

- 任何汇总修改/审核/凭证生成写 `audit_logs`（old/new JSON）。
- 原始 `bank_flows/order_flows` 只读，不提供 Update 接口；错行用“作废+备注”或重新导入。
- 凭证保留 `source_summary_id`，汇总保留 `voucher_id`，可一键反查：凭证→汇总→银行/订单行→文件+行号。
- 备份：设置页一键备份 `finlite.db` 到 `backups/finlite_YYYYMMDD_HHmmss.db`；恢复需二次确认后重启应用。恢复前自动留一份当前库的 `pre-restore` 快照，防误恢复。

---

## 11. 里程碑与验收

| 里程碑 | 内容 | 验收标准 |
|---|---|---|
| M1 导入+流水 | F1+F2+F3 | 三个样本文件导入，对账前数据与 `docs/raw` 行数一致，余额警告复现 |
| M2 对账+汇总 | F4+F5 | 对账结果与 §6.4 一致；汇总审核流转正常 |
| M3 凭证+报表 | F6+F7+F8 | §6.5 五类凭证各出一张且借贷平；余额表期末勾稽上银行末行余额 |

贯穿示例（必测，均已按映射脱敏）：7-10 `王浩然产康 12000/30/11970`、7-11 `赵雨桐 8680 + 晓璐 600` 合计 `9256.80`、7-10 `货款4800/报销717+149/手续费9`、7-13 `房费4552`，全链路从导入到凭证可复演。

---

## 附录 A：原始列映射（导入实现用）

银行：`凭证号→忽略 / 对方账号→counterparty_account(存raw) / 交易时间→txn_time / 对方单位→counterparty / 用途+摘要→summary / 附言→remark / 转入→in_amount / 转出→out_amount / 余额→balance`，金额去千分位逗号后转 decimal。

订单：取 §4.2 核心列，其余整行进 `raw_json`；`工行订单号` 唯一去重；`结算日期` 为空则用交易日期+1 天并标待确认。

汇总第二行费率行跳过（首列为空且含 `0.005`）。

导入中心受理 TSV / CSV / XLSX：文本文件按首行检测分隔符（制表符优先，其次逗号）；XLSX 取首个工作表，首行即表头。列映射预览展示识别到的关键列与样本前若干行。

默认路径导入：默认目录为「库文件同目录 /raw」（可在导入中心更改并持久化），扫描该目录下的 TSV/CSV/XLSX；文件名不含「银行」「订单」等类型关键字、缺关键列、解析失败的文件分别标「未识别 / 缺列 / 不支持」，仅提示不阻断其他文件。同名批次导入时间不早于文件修改时间视为已导入；导入成功后可选归档到 `archived/`（重名追加时间戳），归档后文件不再出现在扫描列表。

## 附录 B：脱敏映射使用

- 唯一映射：`../raw/anonymization_mapping.tsv`（`类型 / 原值 / 脱敏后值 / 说明`）。
- 转换规则：raw 文件导入或落盘前，先按该表做精确字符串替换（公司、人员、商户号全量替换）；账号类按掩码规则保留前后段、中间掩码；订单号/第三方流水号中内嵌的商户号前缀同步替换。
- 业务文档、UI 文案、报错信息、测试断言只允许出现脱敏后值，禁止回写原值。映射表是唯一允许出现原值的例外文件。

## 附录 C：参考映射（均为 ForgeFin 仓库外部只读基线，无需复制到本仓库）

- 流程总览：`ForgeFin/README.md:25-50`
- 数据分析：`ForgeFin/docs/analysis/health-source-data-analysis.md`（含原始商户号原值，禁止原样复制；结论已内化于 §6.3～§6.4）
- 保存/审计：`ForgeFin/docs/analysis/source-data-preservation.md`（结论已内化于 §10）
- 动态模型（本项目暂不采用，远期多行业扩展时再引入）：`ForgeFin/docs/analysis/dynamic-source-data-model.md`
