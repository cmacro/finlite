---
title: 顶栏标题统一为「分类 | 模块名称」并补齐银行流水导入入口
description: 统一所有模块顶栏左侧标题格式与刷新按钮形态，银行流水页新增导入入口，设计原型与 Svelte 应用同步对齐
---

# 顶栏标题统一与银行流水导入入口

## Changed

- 顶栏左侧标题统一为 `分类 | 模块名称`：分类取侧栏导航分组名（弱化 12.5px），模块名保持强调（16px）。
  - 设计原型：`import-center.html`、`bank-flow.html`、`order-flow.html`、`collection.html`、
    `recon-center-prototype.html`、`reconciliation.html`。
  - Svelte 应用：`frontend/src/App.svelte` 改为按当前页所属导航分组渲染标题，
    `frontend/src/styles/topbar.css` 新增 `topbar-cat` / `topbar-sep`，移除旧面包屑与竖线分隔样式。
- 刷新按钮统一为图标按钮（`title` / `aria-label` 提供「刷新」说明）：
  - `bank-flow.html`、`order-flow.html` 去掉「刷新」文字。
  - Svelte 顶栏刷新按钮在所有模块显示，各路由监听 `finlite:refresh` 后重拉本页数据。
- 银行流水页新增导入入口，样式与订单流水模块一致，置于顶栏最右侧：
  - 设计原型：`bank-flow.html` 顶栏「导入明细」按钮与空状态「前往导入中心」均跳转 `import-center.html`。
  - Svelte 应用：`App.svelte` 在银行流水路由的顶栏最右侧显示「前往导入中心」按钮并切换到导入中心。

## Docs

- `docs/uidesign/DESIGN.md`：组件规范补充顶栏标题格式、刷新按钮只显示图标、
  银行流水页顶栏最右侧提供「前往导入中心」入口的约定。

## 影响范围

- 设计原型与前端顶栏/路由刷新交互；不改变账务规则。
- 原始表只读、金额 `decimal`、确认/审核二次确认等约束保持不变。

## 验证

- `make verify`：`gofmt` 无输出、`go vet -tags=gtk3,extra ./...` 通过、Go 单测通过、
  `go build -tags=gtk3,extra .` 通过、`npm run check --prefix frontend` 0 errors / 0 warnings。
- 脱敏检查：本次变更文件对 `docs/raw/anonymization_mapping.tsv` 原值 grep 无命中。
