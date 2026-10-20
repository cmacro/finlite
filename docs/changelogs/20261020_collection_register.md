---
title: 新增店面收款模块（F9）
description: 在「流水账务」分组新增店面收款登记模块，含附件、状态机、生成数据汇总与通道对账视图，样式对齐 docs/uidesign/collection.html
---

# 店面收款（F9）

## Added

- 后端 `internal/services/collection_service.go`：`CollectionService`（List / Get / GetAttachment / Save / Confirm / Reject / Void / GenerateSummary / Recon）。
  - 状态机 `draft → confirmed → summarized`，`draft|confirmed → voided`；仅草稿可编辑，已汇总须先作废关联汇总。
  - 金额全程 `shopspring/decimal`（TEXT 存储），收据号在收款台账内唯一，一收款一汇总。
  - 生成汇总写入 `data_summaries`（`source='collection'`，草稿），所有状态变更写 `audit_logs`。
  - 通道对账：非银通道（现金 + 个人码）对已生成汇总；对公通道按「金额相等 + 收款日后 7 天内」匹配银行转入，差异与未匹配项进待处理队列。
  - 附件存 SQLite BLOB（jpg/png/webp/pdf，单张 ≤ 10MB，单条 ≤ 10 张），随单文件库一起备份。
- 数据表 `collections` / `collection_attachments`（升级脚本 `internal/store/upgrade/04-collections.sql`，schema v4 → v5，基线同步）。
- 前端 `frontend/src/routes/Collection.svelte` + `frontend/src/styles/collection.css`：
  - 收款登记（统计卡 / 筛选条 / sticky 表头密集表格 / 固定操作列 / 表尾合计）。
  - 新增·编辑抽屉（门店/项目/经手人可输入候选，收款方式自动推荐到账账户，开票开关，附件网格与灯箱）。
  - 详情抽屉（状态 stepper、收款信息、附件预览、操作记录）与状态变更二次确认（作废/驳回需填原因）。
  - 通道对账视图（两张通道卡 + 核对明细 + 待处理队列）。
- 侧栏「流水账务」新增「店面收款」，顶栏「批量导入 / 新增收款」入口，草稿数侧栏徽标。
- `internal/services/collection_test.go`：状态机、收据号唯一、汇总生成、通道匹配与三方勾稽隔离测试。

## Changed

- `App.svelte` / `lib/types.ts` / `lib/Icon.svelte`：新增 collection 路由、store/paperclip/image/trash 图标、期间胶囊与导航徽标。
- `recon_service.go`：订单 POS 三方勾稽的汇总求和排除 `source='collection'` 行，避免现金/个人码收款抬高当日汇总收入造成误报。

## Docs

- `docs/business/00-overview-health-finance-flow.md`：新增 §6.7 店面收款与通道对账规则、§5 并行支线、F9 模块清单、数据模型与索引；§6.4 补充店面收款汇总不参与三方勾稽的说明。

## 范围与影响

- 仅新增模块与一条汇总过滤条件；银行/订单原始表仍只读，既有导入、对账、汇总、凭证流程不变。
- 旧库（v4）打开时自动执行 `04-collections.sql` 升级，不修改历史脚本。

## 验证

- `gofmt -l internal main.go`：无输出。
- `go vet -tags=gtk3 ./...`：通过。
- `go build -tags=gtk3 .`：通过。
- `go test ./internal/store/... ./internal/services/`：全部通过（含 v4 库升级到 v5 的临时验证、店面收款服务测试）。
- `npm run check --prefix frontend`：0 errors / 0 warnings。
- `npm run build --prefix frontend`：构建成功。
