---
title: UI 原型统一项目与预览工具
description: 将 OpenDesign 原型统一到单一项目 finlite-uidesign，补充预览地址规则、目录说明与 od-preview 脚本，并同步统一设计规范文档
---

# UI 原型统一项目与预览工具

## Added

- `docs/uidesign/README.md`：原型目录说明、统一预览地址规则
  `http://127.0.0.1:7456/api/projects/finlite-uidesign/raw/{文件名}`、文件清单与新增原型流程。
- `scripts/od-preview.sh`：枚举 OpenDesign 项目中的可预览 HTML 并输出预览 URL；
  支持项目/文件名过滤（`./scripts/od-preview.sh bank`）与 HTTP 状态检查（`--check`）。

## Changed

- OpenDesign 项目合并为单一项目 `finlite-uidesign`（原按模块拆分的 11 个项目已删除）：
  所有原型统一放在项目根目录，新增 `index.html` 总览入口，按文件名直接访问。
- `docs/uidesign/DESIGN.md`：补充「统一项目 `finlite-uidesign` 与预览地址规则」。
- `docs/design/03-design-ui-style-guide.md`：生成流程补充统一项目约定，落地清单更新。

## Docs

- 预览入口：`…/api/projects/finlite-uidesign/raw/index.html`
- 设计规范与 token 同步至项目根：`DESIGN.md`、`tokens.css`。

## 影响范围

- 仅文档与本地开发脚本，不涉及 Go / Svelte / 数据库结构，不影响运行时行为。
- OpenDesign 项目目录为本地工具数据，不进入版本库。

## 验证

- `bash -n scripts/od-preview.sh` 通过；脚本实测输出全部 11 个原型 URL，HTTP 状态均为 `200`。
- `gofmt -l ./...` 无输出。
- `go vet -tags=gtk3 ./...` 通过。
- `go build -tags=gtk3 .` 通过。
- 前端未改动，未运行 `npm run check`。
- 脱敏检查：本次变更文件对 `docs/raw/anonymization_mapping.tsv` 原值 grep 无命中。
