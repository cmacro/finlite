---
title: Tailwind 风格迁移与样式目录化
description: 全站切 Tailwind v4 theme 模式，样式收敛至 styles/ 目录，修复启动黑屏底色
---

# 20260910 tailwind style migration

## Changed

- 前端接入 Tailwind CSS v4（`tailwindcss` + `@tailwindcss/vite`，`vite.config.ts` 注册插件），CSS-first 配置，无 `tailwind.config.js`。
- `App.svelte` 布局、8 个 routes 页面、`Confirm.svelte` 的散装样式全部替换为 utilities；Svelte 内零 `<style>` 块。
- 样式独立目录管理：`frontend/src/styles/`（`index.css` 入口 → `tokens.css` @theme → `base.css` → `table.css` / `controls.css` / `modal.css`），删除旧 `src/app.css` / `src/app-layout.css` / `src/components.css`。
- 组件类收敛（`@layer components` + `@apply`，命名对齐 ForgeFin）：`.data-table`（th/td/`.num`/`.empty`）、`.btn`（+`.btn-sm` 高特异写法防 padding 冲突、+`.btn-danger`）、`.input`、`.toolbar`、`.page-title/.section-title`、`.modal-*`。
- 颜色纳入 `@theme` 统一管理（`ink/surface/mask/muted/danger/ok/warn/edge…/sider/thead/navactive`，色值与迁移前一致）；Svelte/TS 禁直接用色（`bg-white/bg-black`/十六进制），规则写入 `tokens.css` 头注释。
- `lib/format.ts statusClass` 改返 theme utilities（`text-danger/text-ok/text-muted`），三色语义不变。

## Fixed

- 启动先黑后白：`internal/app/app.go` 窗口 `BackgroundColour` 由近黑 `RGB(6,7,15)` 改为白 `RGB(255,255,255)`，与 `--color-surface` 对齐；`frontend/index.html` 加内联首屏底色，覆盖 CSS 包到达前的间隙。

## Scope / Impact

- 纯表现层：视觉与迁移前一致（13px 密集表、6px/8px 单元格、红/绿/灰三色状态）；无业务逻辑、无 SQL、无 bindings 改动。
- 新增运行时依赖：`tailwindcss`、`@tailwindcss/vite`（`package.json` / `package-lock.json` 更新）。

## Verification

- `npm run check --prefix frontend`：0 errors, 0 warnings。
- `npm run build --prefix frontend`（production）：成功，产物含 `.data-table/.btn/.modal-overlay/.toolbar`，13 个 token 全以 `var(--color-*)` 引用。
- `gofmt -l internal/app/app.go`：干净。
- `go vet ./...` / `go build ./...`：仍因本机缺系统 WebKit 依赖失败（Wails `linux_cgo` 既有环境问题，与本次前端改动无关，需在可编译环境复核 `wails build` 首屏）。
- 匿名化：改动域 grep 无账号/手机号类数字串；无真实名称。
