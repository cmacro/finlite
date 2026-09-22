---
title: 银行流水页接线与布局滚动修复
description: BankFlow.svelte 接入 BankFlowService 绑定实现银行流水显示；App 外壳改为导航与标题行固定、内容区独立滚动
---

# 20260922_bank_flow_display

## Added

- `frontend/src/routes/BankFlow.svelte`：银行流水页完整接线（F2）。
  - 经 `BankFlowService.List` 分页加载（每页 50 条），支持批次下拉、日期区间、对方/摘要/附言关键字筛选与上一页/下一页。
  - 三色余额状态：`mismatch` 整行红底（新增 `bg-danger-soft` token），`ok`/`confirmed` 绿字，`skip` 灰字。
  - 批次余额确认 / 撤销确认按钮，均经 `Confirm` 对话框二次确认后调用 `ConfirmBatch` / `UnconfirmBatch`；按批次生效，未选批次时禁用并提示。
- `frontend/src/styles/tokens.css`：新增语义色 `--color-danger-soft`（mismatch 行红底，符合 token 规则：Svelte 内不直接用色）。
- `frontend/src/styles/table.css`：新增 `.data-table .row-mismatch td` 红底样式。

## Changed

- `BankFlow.svelte` 类型改从 `bindings/finlite/internal/services/models` 导入（与 Import 页一致）；转入/转出为 `0` 时不显示，保持密集表可读性。
- `App.svelte` 布局调整：
  - `main` 改为 `h-screen overflow-hidden`，左侧导航固定，仅右侧内容区独立滚动（侧边栏不随内容滚动）。
  - 页面标题上移至 App 外壳固定 `header`（复用导航 `label`），8 个路由组件删除各自的 `<h2 class="page-title">`；内容列改为 flex-col：标题行 `shrink-0` 固定，仅标题下方主体 `overflow-y-auto` 滚动。表格 `sticky` 表头吸附参照随之变为内容滚动区，固定在标题下方，不被遮挡。

## 影响范围

- 仅前端展示层；后端 `BankFlowService` 无改动，原始表 `bank_flows` 保持只读。
- 绑定为 Wails 生成，未手改。

## 验证结果

- `npm run check --prefix frontend`：0 errors / 0 warnings
- `npm run build --prefix frontend`：成功，`bg-danger-soft` 已出现在产物 CSS
- `gofmt -l ./...`：无输出；`go vet -tags=gtk3 ./...`：无问题；`go build -tags=gtk3 .`：成功