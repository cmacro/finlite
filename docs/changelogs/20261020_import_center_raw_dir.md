---
title: 导入中心默认原始目录扫描与批量导入
description: 新增默认原始目录（库文件同目录 /raw，可配置并持久化）扫描、预览、批量导入与可选归档，配套 app_settings 存储及导入批次时间展示
---

# 导入中心默认原始目录导入

## Added

- `internal/store/upgrade/03-app-settings.sql` + `00-latest-schema.sql`：新增 `app_settings(key, value, updated_at)`
  键值表，保存本地界面偏好；当前用于导入中心默认原始目录。
- `internal/services/import_raw_dir.go`：默认路径导入服务
  - `GetRawDir` / `SetRawDir`：默认目录为「库文件同目录 /raw」，可修改并持久化。
  - `ScanRawDir`：扫描 TSV / CSV / XLSX，识别类型、缺列、不支持文件；
    同名批次导入时间不早于文件修改时间视为已导入；目录不存在仅提示不报错。
  - `PreviewRawFile`：仅允许预览默认目录内文件，避免前端读取任意路径。
  - `ImportRawFiles`：逐个导入选中文件，单文件失败不阻断其他文件；可选归档到 `archived/`（重名追加时间戳）。
- `internal/services/import_raw_dir_test.go`：覆盖默认目录、扫描状态、路径穿越防护、归档去重等场景。

## Changed

- `internal/services/models.go`：新增 `RawFile`、`RawScan`、`ImportRawReq`、`ImportRawResult`；
  `ImportResult` 增加 `ImportedAt`（RFC3339），批次历史可展示导入时间。
- `internal/services/import_service.go`：批次列表返回并填充 `ImportedAt`；列映射按类型统一走 `columnSpec`。
- `frontend/src/routes/Import.svelte`：导入中心接入默认路径扫描、文件勾选、批量导入、归档开关与结果提示；
  保留原有选择文件、列映射预览、去重与余额校验流程。
- `frontend/bindings/finlite/internal/services/*`：重新生成 Wails TS 绑定，包含默认路径导入接口与模型。
- `docs/business/00-overview-health-finance-flow.md`：F1 流程补充「扫描默认原始目录」与归档说明，
  业务表结构补充 `app_settings`。

## Docs

- `docs/uidesign/import-center.html`：默认路径导入区域与状态徽标（待导入 / 已导入 / 不支持）。

## 影响范围

- 新增本地文件扫描与批量导入入口，复用既有解析、去重、余额校验与入库逻辑。
- 原始表仍只读；文件归档只移动默认目录内的文件，不修改已入库数据。
- 目录设置仅保存在本机 `app_settings`，不改变单账套/单机约束。

## 验证

- `make verify`：`gofmt` 无输出、`go vet -tags=gtk3,extra ./...` 通过、
  `go test ./internal/store/... ./internal/decimal/... ./internal/services/...` 全部通过、
  `go build -tags=gtk3,extra .` 通过、`npm run check --prefix frontend` 0 errors / 0 warnings。
- 脱敏检查：本次变更文件对 `docs/raw/anonymization_mapping.tsv` 原值 grep 无命中。
