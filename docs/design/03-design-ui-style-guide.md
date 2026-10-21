---
title: UI 统一风格规范（design system 基线）
description: 定义 finlite 全部界面原型的统一视觉规范与 OpenDesign 生成流程，确保后续 UI 设计保持同一风格；机器可读 token 与完整规范见 docs/uidesign
---

> 关联：`docs/design/01-design-recon-center-usage.md`、`docs/design/02-design-store-collection.md`。
> 机器可读 token：`docs/uidesign/tokens.css`；完整规范（OpenDesign 面向）：`docs/uidesign/DESIGN.md`；基准页：`docs/uidesign/import-center.html`。

## 1. 目的

finlite 的界面原型由 OpenDesign 生成，若不约束会各自成风。本规范把已定稿页面的视觉语言固化为**单一事实源**，并规定后续生成的强制流程，使新模块天然沿用同一风格。

## 2. 基线

- **设计系统**：Ant（企业级、数据密集），主色 `#1677ff`。
- **状态色**：一律 `oklch`（成功 / 警告 / 错误），禁止写死 hex。
- **字体**：Inter + 中文回退；金额/数量等宽右对齐（`tabular-nums`）、两位小数、千分位。
- **布局**：侧栏 220px + 顶栏 52px + 内容区；表格 sticky 表头。
- **圆角**：4 / 6 / 8 / 12；间距走 8pt 网格。

完整色板、字号阶梯、组件与状态色映射见 `docs/uidesign/DESIGN.md`；token 变量见 `docs/uidesign/tokens.css`。

## 3. 设计层硬约束

- 原始表只读；破坏性/状态变更操作必须二次确认。
- 明细与批次列表**一次性完整展示，禁止分页**。
- 表格一律用 `lib/DataTable.svelte`（固定列宽 + 粘性表头 + 全量 DOM）；为保住「整表框选复制到 Excel」，
  **不做虚拟滚动**，性能靠固定布局与行级响应式（见 `04-design-data-table.md`）。
- 行明细统一走右侧详情抽屉，**表内不插入展开行**（长表插行会触发整表重排）。
- 金额 `decimal` 全链路，错误中文可执行。
- 示例数据脱敏（账号如 `1001****7575`）。

## 4. OpenDesign 生成流程（强制）

0. **统一项目**：所有原型集中在 OpenDesign 项目 `finlite-uidesign`，文件放项目根，预览地址为 `http://127.0.0.1:7456/api/projects/finlite-uidesign/raw/{文件名}`。
1. 固定 design system：`start_run({ plugin: "design-system-ant" })`（或 `create_project({ designSystem: "ant", skill: "design-md" })`）。
2. 生成前将 `docs/uidesign/DESIGN.md`、`docs/uidesign/tokens.css` 写入项目根，并放一份基准页（如 `import-center.html`）。
3. Prompt 固定前导：明确「严格遵循 DESIGN.md / tokens.css / 基准页，不得自创配色字体圆角；明细不分页；中文；金额等宽右对齐；脱敏」。
4. 产物按 `docs/uidesign/` 约定落盘；风格变体放在 `docs/uidesign/styles/`。

## 5. 落地清单

- [x] `docs/uidesign/tokens.css` 规范 token
- [x] `docs/uidesign/DESIGN.md` 完整规范（OpenDesign 面向）
- [x] OpenDesign 统一为单一项目 `finlite-uidesign`，内置 `DESIGN.md` + `tokens.css` + 全部原型
- [x] `docs/uidesign/styles/` 保存 Ant / Material / Linear / Stripe 四风格变体供对比
- [x] `docs/uidesign/README.md` 原型清单与预览入口
