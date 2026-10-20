---
title: 订单流水页按 UI 设计稿改版（统计卡 / 结算日期归集 / 手续费复算 / 抽屉）
description: 对齐 docs/uidesign/order-flow.html 重做订单流水界面，并让 OrderFlowService.List 一次性返回全部明细、按结算日期归集小计与全量合计
---

# 订单流水页按 UI 设计稿改版

## Added

- `OrderFlowService.List` 返回结构升级：`OrderFlowPage` 新增 `groups`（按结算日期归集的 `OrderFlowGroup`，含笔数、订单金额/手续费/实收小计、异常与解析失败笔数）、全量合计（`orderTotal` / `feeTotal` / `netTotal` / `reconDiff`）与分类异常笔数（`feeDiffCount` / `netDiffCount` / `parseErrCount` / `anomalyCount`）。
- `OrderFlow` 展示字段：`feeRate`（费率百分数）、`expectedFee`（预期手续费）、`expectedNet`（应到实收）、`feeDiff`、`netDiff`、`fileName` / `importedAt`（关联 `import_batches`）、`sourceRowNo`、`rawJson`。
- `internal/decimal` 新增 `RatePercent`（`0.0025` → `0.25%`），费率展示不再由前端拼接。
- 页级样式 `frontend/src/styles/orderflow.css`（`of-` 前缀，避免与导入中心、银行流水页类名冲突）。
- 订单流水服务单测 `internal/services/orderflow_test.go`：结算日期归集与小计、费率差异/实收差异标记、`OnlyFeeAdj` 过滤、结算日期与关键字筛选。

## Changed

- 订单流水页 (`frontend/src/routes/OrderFlow.svelte`) 按设计稿重做：只读提示条 → 5 张统计卡 → 筛选条（结算日期 / 关键字 / 「仅看费率差异」开关 / 查询 / 重置）→ 按结算日期归集小计的密集表格 → 行内手续费复算 → 表尾合计 → 空态 → 行详情抽屉 → Toast。
- 列表取消分页（DESIGN.md §7 禁止分页），改为一次性返回全部明细，并在内部区域滚动 + sticky 表头。
- 费率差异行整行淡黄底 + 左侧黄条、实收差异/解析失败行整行淡红底 + 左侧红条；费率检查列用徽标（费率正确 / 费率差异 / 实收差异 / 金额解析失败）。
- 手续费复算与金额恒等式（订单金额 − 手续费 = 实收，±0.01）全部由 Go `decimal` 计算，前端只格式化展示。
- 修复原 `OnlyFeeAdj` 在分页后才过滤导致条数与命中不一致的问题（现为全量过滤后统计）。
- 顶栏：订单流水页增加「会计期间」胶囊与「导入明细」主操作（与银行流水页一致）；页面底色沿用 `canvas`。

## Docs

- 无业务文档变更；实现依据 `docs/uidesign/order-flow.html` 与 `docs/uidesign/DESIGN.md`。

## 影响范围

- 仅影响订单流水页的读取展示与列表 DTO；`order_flows` 仍为只读原始表，本页无任何编辑/删除/写入入口。
- 金额与费率全部由 Go `decimal` 计算；对账、汇总、凭证逻辑未改动。

## 验证

- `gofmt -l internal main.go`：无输出。
- `go vet -tags=gtk3 ./...`：通过。
- `go build -tags=gtk3 .`：通过。
- `go test ./internal/store/... ./internal/decimal/... ./internal/services/...`：20 个测试通过（含新增 3 个订单流水用例）。
- `npm run check --prefix frontend`：0 errors / 0 warnings。
- `npm run build --prefix frontend`：构建成功。
- 冒烟：导入 `docs/raw/订单流水.tsv` 共 13 行，归集为 8 个结算日期；07-10 实收 `11970.00`、07-11 实收 `9256.80`、07-12 实收 `446.88` 与业务文档一致；订单金额合计 `58431.00` − 手续费 `146.08` = 实收 `58284.92`，勾稽差额 `0.00`，费率异常 0 笔。
- 脱敏检查：对 `docs/raw/anonymization_mapping.tsv` 原值 grep `internal/`、`frontend/src/`、`docs/changelogs/` 无命中。
