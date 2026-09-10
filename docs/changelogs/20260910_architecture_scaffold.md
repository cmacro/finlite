---
title: 移植 ForgeFin 架构骨架到 finlite
description: 基于 ForgeFin 实现生成 finlite 后端 9 大 Service、SQLite 基线 schema、Svelte 8 页面骨架与全局样式 token
---

# Changelog 2026-09-10 架构骨架移植

## Added

- 后端 `internal/decimal`：金额封装（解析/2 位展示/费率映射/±0.01 容差）
- 后端 `internal/store`：WAL + 单写 mutex + `user_version` 迁移，基线 `upgrade/00-latest-schema.sql`（10 表），费率/科目种子
- 后端 `internal/services`：models + Import / BankFlow / OrderFlow / Recon / Summary / Voucher / Account+FeeRate / Backup+Report / Audit 共 9 组 Service，`app.go` 注册 12 个服务
- 前端 `routes/` 8 页面骨架（导入/银行/订单/对账/汇总/凭证/余额表/设置）+ `lib/`（format/types/Icon/Confirm）
- 前端 `src/app.css`：全局样式 + 12 个语义 token；`Makefile`（fmt/vet/test/check/verify/bindings/dev/build）
- Wails TS 绑定：12 个 Service 绑定文件（生成器产出，未手改）

## Changed

- `App.svelte`：Greet 演示页替换为左侧导航框架；布局样式保留 scoped，共享样式移入 `app.css`
- `index.html`：删除模板 `public/style.css` 引用与背景层，标题改为 finlite 财务助手
- `go.mod`：新增 `shopspring/decimal`、`modernc.org/sqlite`、`google/uuid` 直接依赖
- 前端依赖：`@lucide/svelte`（图标，与 ForgeFin 同名映射）

## Fixed

- 消除模板 `public/style.css` 与 `app.css` 的 `.btn`/`.input` 样式冲突
- 移除绑定生成残留的 `frontend/.bindings-tmp-*/` 临时目录

## Docs

- 本文件（提交日志）；业务依据仍为 `docs/business/00-overview-health-finance-flow.md`，未改动

## Scope / Impact

- 从 M0 模板进入可编译骨架阶段；MVP 链路方法签名已定，未接真实数据（页面按钮 disabled，待绑定接线）
- 单账套 `finlite.db`，金额 TEXT + decimal，余额严格校验/凭证配平/审计日志规则已在 Service 层落地

## Verification

- `gofmt -l` 干净；`go vet`（业务包）无问题；`go test` 3 通过；`npm run check` 0 错误；`make verify` 全过
- `go build ./...` 仍受系统 GTK 头文件缺失影响（模板自带环境问题，与本次改动无关）
- 脱敏 grep：原值仅存在于 `docs/raw/anonymization_mapping.tsv`，其余零命中
