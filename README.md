# finlite

财务工作助手（Financial Workspace）——桌面优先的单账套记账助手，面向健康管理公司日常对账制单。

## 产品目标

让每个财务人员每天节省 1～3 小时。

当前聚焦一件事：把「银行流水 + 订单流水 + 数据汇总 → 对账 → 凭证 → 余额表」最小闭环跑通（详见业务唯一依据 `docs/business/00-overview-health-finance-flow.md`）。

耗时点不在会计知识，而在复制粘贴、找数据、核对金额、补备注。finlite 先用软件替代这部分手工操作。

> 参考基线：`../ForgeFin`（Rust + Tauri + Leptos）。业务规则直接复用其已验证结论（余额校验、三方勾稽、费率），Go 重写；动态元数据模型与多公司隔离暂不引入。

## 技术栈

| 层 | 技术 |
| --- | --- |
| 桌面容器 | Wails3 `v3.0.0-beta`（Go backend + WebView） |
| 前端框架 | Svelte 5 + TypeScript + Vite（`frontend/`） |
| 语言 | Go 1.25+ |
| 数据库 | SQLite 单文件 `finlite.db`（纯 Go 驱动，如 `modernc.org/sqlite`，WAL + 单写 mutex） |
| 金额 | `shopspring/decimal` 端到端，SQLite 存 `TEXT`，展示保留 2 位；禁用 `float64` 算金额 |

**它不是：**

- 营销站 / landing page
- 多公司 SaaS（MVP 单账套）
- 通用会计平台（MVP 只做健康管理流程）

## MVP 流程（必须保持可跑通）

```
Import（银行/订单 TSV）-> Bank/Order 列表 -> 按 settle_date 对账
  -> 数据汇总（draft->reviewed->vouchered）-> 凭证（draft->audited）-> 余额表
```

页面映射：导入中心 → 银行流水 → 订单流水 → 对账中心 → 数据汇总 → 凭证管理 → 科目余额表 → 系统设置（科目/费率/备份）。

## 软件架构

### 单账套单库（与 ForgeFin 多库隔离的区别）

ForgeFin 是每公司独立 `.db`（`forgefin_system.db` + `companies/forgefin_company_{id}.db`），用户登录后按 `company_id` 路由。

finlite 首版刻意做减法，单机单用户单库：

```
finlite.db                 # 唯一业务库：银行/订单/汇总/凭证/科目/审计
└── backups/finlite_YYYYMMDD_HHmmss.db   # 设置页一键备份
```

- 前端绝不直连 SQLite，一律经 Wails Service 绑定调用
- 后端 `database/sql` + 裸 SQL，无 ORM；`context.Context` 为阻塞/DB 方法首参，返回 `(T, error)`
- 原始表 `bank_flows` / `order_flows` 只读，无 UPDATE 接口；错行走作废重导或调整行

### 工作流

```
收集资料 → 导入 → 对账 → 汇总 → 生成凭证 → 审核 → 报表
```

## MVP 功能清单

| # | 模块 | 作用 | 验收要点 |
| --- | --- | --- | --- |
| F1 | 导入中心 | TSV/CSV/XLSX 解析、去重、余额校验入库 | 去重按工行订单号 + 银行（时间+金额+对方） |
| F2 | 银行流水 | 列表 + 批次确认/撤销 | `mismatch` 红底，确认后变绿，可撤销 |
| F3 | 订单流水 | 列表 + 费率复算 | 手续费 ±0.01 外标异 |
| F4 | 对账中心 | 按结算日三方勾稽 | 差异进待处理，不阻塞其他行制单 |
| F5 | 数据汇总 | CRUD + 自动预填 + 审核/驳回 | 未审核不可生成凭证 |
| F6 | 凭证管理 | 新增（配平检查）+ 审核/反审核 | 借贷不等禁止保存 |
| F7 | 科目余额表 | 期初+本期借贷+期末 | 期末 = 期初 + 借 - 贷（按方向） |
| F8 | 基础设置 | 科目/费率维护、备份/恢复 | 恢复需二次确认 |

非目标：多账套、OCR、AI、税务申报、固定资产、应收账龄、微信自动同步。

## 核心业务规则（摘要，实现以业务文档为准）

- **T+1 归属**：收入归属日期 = 订单 `结算日期`，不是交易时间。
- **费率**：借记卡 `0.5%` / 贷记卡 `0.6%` / 微信支付宝 `0.25%`；系统预填，手工改置 `fee_manual_adjusted=1`。
- **银行余额校验（严格相等，无容差）**：`本行余额 == 上行余额 + 转入 - 转出`；不平仍入库标 `mismatch`，仅按批次人工确认可消红，可撤销。
- **三方勾稽（按结算日）**：`Σ订单实收 == 银行POS转入 == Σ汇总实际收入`，差异进待处理队列。
- **凭证**：有借必有贷、借贷必相等；仅 `reviewed` 汇总可生成；一汇总一凭证；每次汇总修改/审核/制单写 `audit_logs`（old/new JSON）。
- 用户-facing 错误为可操作的简体中文（如 `借贷不平：借 12000.00 ≠ 贷 11970.00`）。

## 快速开始

### WSL 下使用 GTK3

在 WSL Debian12 用 GTK3 作 WebView 后端时：

```
export GOFLAGS="-tags=gtk3"
```

### 开发 / 构建

```sh
wails3 dev -config ./build/config.yml   # 开发热重载；或 task dev
wails3 build                            # 生产构建，产物在 build/
go build ./...                          # 仅后端校验
npm run check --prefix frontend         # 前端类型检查（改了 frontend 必跑）
```

### 提交前自检

```sh
gofmt -l ./...
go vet ./...
go build ./...
npm run check --prefix frontend  # 动过 frontend 才需跑，但建议都跑
```

全部通过才算完成。

## 项目结构

```
main.go                      # 薄入口，仅 embed frontend/dist
internal/app/app.go          # Wails app + 窗口 + Service 注册
internal/services/*.go       # 一域一 Service，暴露给前端
internal/store/*.go          # SQLite open/migrate/query 助手
internal/decimal/*.go        # 金额 parse/format 封装
frontend/src/routes/         # 页面：import, bank-flow, order-flow, recon, summary, voucher, balance, settings
frontend/src/lib/            # 复用组件、格式化、api 封装
frontend/bindings/           # Wails 生成，禁止手改
docs/business/00-overview-health-finance-flow.md  # 业务唯一依据，代码冲突时以它为准
```

## 开发理念（沿用 ForgeFin）

1. **先记录，后编码** — 记录每天的工作流程和真实痛点
2. **小步快跑** — 每完成一个小功能立即在真实工作中使用
3. **持续度量** — 记录点击次数、耗时和易错点，持续优化
4. **从个人到通用** — 功能成熟后再抽象为通用能力

> 不是功能比金蝶更多，而是让一个财务人员每天节省 1～3 小时。
>
> 如果真正做到这一点，即使第一版只有完整系统 10% 的功能，它对目标用户的价值也可能远远超过一个功能齐全但操作复杂的系统。
