---
title: 流水明细表性能与数据表控件
description: 银行流水/订单流水 2000+ 行卡顿修复：抽离 lib/DataTable.svelte 高性能数据表（固定列宽、全量 DOM、行级响应式、整表复制到 Excel），移除表内展开的差值面板，明细统一走右侧抽屉；同步更新 OpenDesign 原型
---

# 流水明细表性能与数据表控件（2026-10-21）

## 背景

银行流水 2269 行、订单流水 2019 行，进入页面后滚动与点击均有明显卡顿。实测后端 `List` 仅 27ms / 21ms
（`json.Marshal` 另 4~5ms，响应体 1.59MB / 1.32MB），瓶颈全在前端：

- 一次性渲染 2269 行 × ~25 个 DOM 节点（约 5.7 万节点），无任何窗口化；
- `page = $state(...)` 把 2269 行包成深层响应式代理，模板逐格走 proxy get；
- 展开/勾选状态整体替换（`{...expanded, [id]: v}`），点一次会让所有读过该状态的行一起失效；
- `formatMoney` 每格调用 `Number().toLocaleString('zh-CN', {...})`，单次渲染约 6800~9000 次 Intl 调用；
- 每行 2~3 个 `<Icon>` 组件实例（约 4500 次组件实例化）+ 每行 2~3 个闭包监听器。

## Added

- `frontend/src/lib/DataTable.svelte`：财务主控件。列定义驱动（key / 表头 / 固定宽度 / 右对齐 / 单元格 snippet /
  悬停全文 / 导出取值），负责粘性表头、`colgroup` 固定布局、分组行、行点击与 `data-act` 分发、表格工具条。
  不做虚拟滚动——全量行保持真实 DOM，框选 / `Ctrl+A` 后可原样粘进 Excel。
- `frontend/src/lib/table.ts`：`TableColumn<T>` 类型 + `buildTSV`（含表头、CRLF、金额取后端原始 decimal 字符串）
  + `copyTSV`（Wails `Clipboard.SetText`）。
- `frontend/src/styles/datatable.css`：控件结构样式，先于 `bankflow.css` / `orderflow.css` 导入，页面可覆盖。
- 「复制表格」按钮：银行流水在表格工具条、订单流水在筛选条右侧，一键复制当前筛选的全部行到剪贴板。
- `docs/design/04-design-data-table.md`：控件取舍、属性约定与性能红线。

## Changed

- `BankFlow.svelte` / `OrderFlow.svelte` 改用 `DataTable`：`page` 改 `$state.raw`；列定义为固定宽度
  （银行流水 1212→1178px、订单流水 1182→1142px，均为去掉展开列后的列宽合计）；长文本列改为省略号 +
  悬停全文（行高统一）。
- `format.ts` 的 `formatMoney` 改为纯字符串补零 + 千分位（不经过 `float64`、不调 `Intl`）：
  20000 次调用 285ms → 7ms，且 `11970.005` 之类的进位仍与旧实现逐字节一致。
- 表格行首不再有展开列；行级图标改为内联 `<svg>` / snippet，避免上千次组件实例化。
- 订单流水的分组行（结算日期小计）改由 `DataTable` 的 `groupBy` / `groupHead` 渲染，`colspan` 由列数推导。
- `docs/design/03-design-ui-style-guide.md` 增加表格红线：统一用 `DataTable`、不做虚拟滚动、行明细走抽屉。
- `docs/uidesign/bank-flow.html`、`order-flow.html`（含 OpenDesign 项目 `finlite-uidesign` 同步）：
  删除行内展开按钮、展开明细行（余额衔接校验 / 手续费复算）及其 CSS/JS，表头与 `colgroup` 去掉展开列，
  `min-width` 与前端一致；`README.md` 说明行详情走抽屉。

## Removed

- 银行流水表内的「余额衔接校验」展开面板（定位相邻行、标记已核对、查看完整详情）与本地「已核对」标记：
  明细改为点击行打开右侧抽屉，抽屉内仍展示余额衔接结论与差额；`bf-flash` 高亮随之删除。
- 订单流水表内的「手续费复算」展开面板：费率复算、恒等式与实收差异在抽屉中展示。
- 相关死样式：`.bf-expand-btn` / `.bf-detail-row` / `.bf-eq-*`（保留抽屉仍在用的 `.bf-eq-verdict`）、
  `.bf-checked-tag`、`.of-expand` / `.of-detail-row` / `.of-recalc*` / `.of-identity` / `.of-parse-note`。

## 验证

- `npm run check --prefix frontend`：0 errors / 0 warnings
- `npm run build --prefix frontend`：通过（CSS 141.4KB→133.7KB，JS 253.0KB→245.1KB）
- `gofmt -l .`、`go vet -tags=gtk3 ./...`、`go build -tags=gtk3 .`：通过
- `wails3 task build EXTRA_TAGS=gtk3`：通过，产物 `bin/finlite` 启动冒烟正常（窗口可开、资源正常加载）
- 后端耗时基线（本机样例库）：`BankFlowService.List` 27ms / 2269 行，`OrderFlowService.List` 21ms / 2019 行
- 原型：`bank-flow.html` / `order-flow.html` 内联脚本可解析（新增/改动 JS 语法检查通过），
  `docs/uidesign/` 与 OpenDesign 项目文件 md5 一致
- 脱敏：本次改动只涉及控件、样式与文案，未引入任何真实单位、账号或个人信息
