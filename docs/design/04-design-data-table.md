---
title: 数据表控件（DataTable）设计说明
description: finlite 财务主控件 DataTable 的实现取舍、属性约定与性能红线：全量 DOM + 固定列宽以保留整表复制到 Excel，明细统一走右侧抽屉
---

> 关联：`docs/design/03-design-ui-style-guide.md`（视觉规范）、`docs/uidesign/bank-flow.html`、`docs/uidesign/order-flow.html`。
> 代码：`frontend/src/lib/DataTable.svelte`（骨架）、`frontend/src/lib/table.ts`（列定义 + TSV 导出）、`frontend/src/styles/datatable.css`（结构样式）。

## 1. 背景

流水类页面（银行流水 2200+ 行、订单流水 2000+ 行）一次性展示全部明细，且财务用户后续会**把表格内容复制粘贴到 Excel**。这两条需求决定了表格不能走「虚拟滚动 / 分页」路线——见 §3。

旧实现把每行展开的差值面板（余额衔接校验、手续费复算）直接插在表体里，`<tr>` 行数翻倍，且展开一行会让整张长表重新计算列宽，滚动与点击都有明显卡顿。

## 2. 控件职责

`DataTable.svelte` 只负责表格骨架与交互分发，不含任何业务逻辑：

| 能力 | 说明 |
|---|---|
| 列声明 | 页面传 `columns: TableColumn<T>[]`（key / 表头 / 固定宽度 / 右对齐 / 单元格 snippet / 悬停全文 / 导出取值） |
| 粘性表头 + 固定布局 | `table-layout: fixed` + `colgroup`，列宽由页面给定，跨批次/筛选恒定 |
| 分组行 | `groupBy` + `groupHead`：与上一行分组键不同时插入一行分组标题（订单流水按结算日期） |
| 点击行 | `onRowClick`（打开右侧详情抽屉）；行内按钮用 `data-act` 交给 `onAct`，不触发整行点击 |
| 工具条 | `toolbar` snippet（标题 / 图例 / 复制表格按钮） |
| 空态 | 由页面自己渲染（`{#if rows.length === 0}`），控件不参与文案 |

金额、状态、费率等一切计算与格式化都在 Go Service 或页面的展示函数里完成，控件只把列定义按顺序渲染。

## 3. 为什么不做虚拟滚动（关键取舍）

财务的实际用法是「选中一片单元格 → 粘到 Excel」：

- 全量行必须是**真实 DOM**，否则 `Ctrl+A` / 拖选 / 浏览器原生复制会漏行；
- 分页会让「共 N 条 · 全部展示」的语义与对账习惯冲突（业务文档要求明细一次性完整展示）。

因此性能靠下面四条，而不是裁掉 DOM：

1. **固定布局**：列宽来自 `colgroup`，浏览器单趟布局；不因单元格内容长度重新分配列宽。
2. **行级信号**：展开/勾选这类状态用 `$state` 对象的**属性就地写**（`obj[id] = !obj[id]`），不要整体替换
   （`obj = {...obj, [id]: v}` 会让所有读过该对象的行一起失效，2000+ 行逐行 diff）。
   列表数据整体替换、内部不改写时用 `$state.raw`，避免把上千行包成深层响应式代理。
3. **一个点击处理函数**：所有行共用同一个 handler，靠 `data-dt-index` / `data-act` 分发，不为每行创建闭包。
4. **展示格式化要便宜**：`lib/format.ts` 的 `formatMoney` 用纯字符串补零 + 千分位（不过 `Number()`、不调
   `Intl`），20000 次约 7ms；旧实现 `toLocaleString` 约 285ms。行级图标用内联 `<svg>` 或 snippet，
   不用组件实例（2000 行 × 2 个图标 = 4000 次组件实例化）。

## 4. 表内不展开明细

明细（余额衔接等式、手续费复算、原始 JSON）统一放在**右侧详情抽屉**（点击行打开）：

- 表内插入 `<tr>` 会让 2000+ 行长表重新布局，代价远高于收益；
- 抽屉里空间更充裕，可以放等式、来源、关联凭证等完整信息；
- 表格保持「一行一记录、行高统一」，便于目视比对金额列。

## 5. 复制到 Excel

- 「复制表格」按钮：`buildTSV(columns, rows)` → `clipboard.SetText`。
- TSV 含表头，CRLF 换行；金额用后端原始 decimal 字符串（`11970.00`），**不带千分位**，粘到 Excel 即数值；
  文本内的 Tab / 换行会被替换为空格，避免串列。
- 列的 `copy` 可返回自定义值（如状态中文），`copy: false` 表示该列不导出（如「操作」列）。

## 6. 使用示例

```svelte
<DataTable
  rows={items}
  {columns}
  rowKey={rowKeyOf}
  rowClass={rowClassOf}
  tableClass="bf-table"          <!-- 页面自有视觉 -->
  minWidth="1178px"              <!-- = 列宽合计 -->
  maxHeight="calc(100vh - 424px)"
  onRowClick={openDrawer}
/>
```

页面自带样式按需覆盖（`styles/` 里 `datatable.css` 先于 `bankflow.css` / `orderflow.css` 导入）。

## 7. 验收

- 2000+ 行页面：首屏可交互，滚动无整表重排；点击行/展开无掉帧（明细走抽屉）。
- `npm run check --prefix frontend`、`npm run build --prefix frontend` 通过。
- 复制表格后粘到 Excel：列数正确、金额为数值、无串列。
