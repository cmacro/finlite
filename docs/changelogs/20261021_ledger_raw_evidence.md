---
title: 账套目录与原始数据证据层
description: MVP 固定单账套 ledger-1001，原始文件归档到账套目录并登记 sha256，处理数据与人工态留在 finlite.db；同步账套身份互校、流式备份、交接包与存量迁移。
---

# 账套目录与原始数据证据层（2026-10-21）

## Added

- `internal/store/upgrade/05-raw-files.sql`（schema v4 → v5，基线同步）：
  - `ledger_profile`（单行账套身份：`code` / `display_name` / `created_at`）
  - `raw_files`（原始文件证据索引：`sha256` 主键 + `rel_path` + `orig_name` + `source_type` + 体积/行数/登记时间）
  - `import_batches.raw_sha256`（批次 → 原始文件指纹，空串表示历史批次缺原始文件）
- `internal/store/ledger.go`：账套目录唯一入口 `Ledger`
  - 固定布局 `<data root>/ledgers/ledger-1001/{finlite.db, ledger.json, raw/inbox, raw/archive/YYYY/MM, attachments/YYYY/MM, backups}`
  - `ResolveLedger`（账套码校验）、`Ensure`、`DBPath/RawInboxDir/RawArchiveDir/AttachDir/BackupDir/PackageDir`
  - `Rel/Abs`：相对路径 ↔ 绝对路径，越界（`..`、绝对路径）一律拒绝
- `internal/store/evidence.go`：证据层能力
  - `RegisterRawFile`：sha256 内容寻址 + 归档落盘（临时文件 + rename + fsync）+ 写清单；同内容只落一份
  - `RegisterRawFileInPlace` / `SetRawFileRowCount`：存量登记与行数回填
  - `ListRawFiles` / `VerifyRawFiles`：清单与完整性校验（检测缺失 / 被改动 / 未登记）
  - `ExportBundle`：`VACUUM INTO` 库快照 + `ledger.json` + `raw/archive` + `attachments` + `manifest.tsv` 打成交接包
- `internal/services/rawfile_service.go`：`RawFileService`（`Info` / `List` / `Verify` / `ExportBundle` / `MigrateLegacy`），已注册到 `internal/app/app.go`
- 旧版布局收编：`<data root>/finlite.db`、`raw/`、`backups/` 一次性并入 `ledgers/ledger-1001/`（`Ledger.AdoptLegacy`，幂等、跨盘回退为复制）
- 存量迁移（启动时幂等执行）：旧 `raw/*` → `raw/inbox/`，旧 `raw/archived/*` → `raw/archive/legacy/`，逐文件补登记指纹并回填 `import_batches.raw_sha256`；匹配不上的历史批次保持空值并标「缺原始文件」

## Changed

- 导入链路改为**先归档后解析**：`ImportFile` / `ImportBankFile` / `ImportOrderFile` 先登记原始文件再解析，解析失败也留证；`import_batches` 写入 `raw_sha256`
- 「已导入」判定改为内容指纹（`raw_files` + `import_batches` 联查），不再依赖文件名 + mtime（改内容不改 mtime 不再漏导，改名不再重复导入）
- 默认原始数据目录改为本账套 `raw/inbox/`（外部目录仍可指定，但导入必须复制进账套归档）
- 备份：目录改为账套内 `backups/`，文件名带账套码 `ledger-1001_YYYYMMDD_HHmmss.db`；`ListBackups` 返回 `BackupFile`（名称/路径/大小/时间）
- 恢复：新增账套身份校验，拒绝把其他账套的备份覆盖进当前账套
- 设置页新增「账套与原始数据」：账套信息、校验原始文件（问题明细）、导出交接包、迁移存量原始文件；备份列表展示大小与时间
- 导入中心批次列表展示原始文件指纹（缺原始文件标红）
- `.gitignore` 增加 `ledgers/`、`packages/`、`backups/`、`*.db`、`*.db-wal`、`*.db-shm`

## Fixed

- 全量脚本版本头 `-- v0 -> v1` 与 `SchemaVersion=4` 不一致（历史增量脚本已合并进全量文件），修正为 `v5` 并消除 `go test ./internal/store` 的 `validateBookkeeping` 失败
- 升级路径测试与「过旧库」行为：历史脚本合并后，早于最低脚本版本的库不再做半升级，改为中文可操作报错
- `copyFile` 用 `os.ReadFile` 整库进内存（大库备份必然 OOM）→ 改为 `io.Copy` + `Sync` + 大小校验
- 账套目录与库内账套不一致时静默继续（把 A 账套的库拷进 B 目录会串档）→ 改为拒绝打开并给出修复提示

## Docs

- `docs/business/00-overview-health-finance-flow.md`：新增 §8.1「账套目录与原始数据/处理数据分离」（目录布局、7 条规则、账套三角互校、不加 `ledger_id`），同步 §1.3 关系表、§8 表结构、§10 审计备份口径、非目标措辞
- `README.md`：架构章节改为「单账套单目录」，补充原始文件归档与交接包说明

## Verification

- `gofmt -l ./internal ./main.go`：无输出
- `go vet -tags=gtk3 ./...`：通过
- `go build -tags=gtk3 .`：通过
- `go test ./internal/store/... ./internal/decimal/... ./internal/services/...`：52 个用例通过
  （新增：原始文件归档/去重、篡改与未登记检测、交接包 manifest、账套身份互校、路径越界、解析失败留证、指纹判重、存量迁移幂等、旧版布局收编）
- 样本冒烟（`docs/raw`）：银行流水 20 行 / 订单流水 13 行全部导入成功，证据归档为
  `raw/archive/2026/10/20261021-bank-flow-56d8b0e1.tsv`、`…-order-flow-751bf7dc.tsv`，完整性校验 2/2 通过
- `npm run check --prefix frontend`：0 errors / 0 warnings
- `wails3 generate bindings -ts -i`：已重新生成（未手改 bindings）
