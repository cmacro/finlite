---
title: 升级脚本按文件名前缀版本读取，版本头作为校验
description: 修复 migrate 无法读取 03/04 增量脚本的问题：文件名前缀即目标版本，版本可不连续按前缀升序执行，首行版本头仅用于校验（版本、形式、英文、空行、COMPAT），并统一全量脚本 TO 与 SchemaVersion
---

# 20261021 schema_script_loader

## Added

- `internal/store/migrate.go`：
  - `loadUpgradeStepsFS(fsys fs.FS, dir string)`：文件系统可注入的脚本加载器，便于测试非连续版本。
  - `splitHeader` / `checkHeaderText` / `parseScriptHeader`：校验增量脚本首行版本头（Form A / Form B），检查单行英文、句首大写、无句号结尾、无行尾空白、头部后空一行、`COMPAT` 落在 `v2 ~ v{版本-1}`、版本头与文件名前缀一致。
  - `validateBookkeeping`：校验全量脚本的 `TO == SchemaVersion == 最高脚本版本`、`COMPAT` 取脚本 `COMPAT` 最大值（无则 `v1+`）、头部 `FROM` 为 `0`。
- 测试 `TestLoadUpgradeStepsSortsNonContiguousVersions`：`03`、`04`、`101` 按前缀升序执行，缺口不补号。
- 测试 `TestLoadUpgradeStepsRejectsBadHeaders`：覆盖文件名/头部不一致、形式不合规、行尾空白、缺空行、中文描述、小写开头、`COMPAT` 越界、`01-*.sql`、版本重复等 12 种拒绝场景。
- 测试 `TestOpenToleratesNewerSchema`：库版本高于程序版本时不降级、不改写。

## Changed

- 脚本版本语义改为「文件名前缀 = 该脚本引入的目标版本」，不再假定 `to = 前缀 + 1`，也不再要求版本连续；旧库升级改为「按前缀升序执行所有 `to > user_version` 的脚本」，缺口直接跳过。
- 版本校验提前到 `version == 0` 判断之前，新库执行全量脚本前也会校验脚本账目，避免「能跑但版本漂移」。
- `internal/store/sqlite.go`：`SchemaVersion` 5 → 4，与最高增量脚本（`04-collections.sql`）及全量脚本 `TO` 对齐；注释改为「等于最高增量脚本的文件名前缀」。
- `internal/store/upgrade/00-latest-schema.sql`：版本头 `-- v0 -> v5 …` → `-- v0 -> v4 …`（仅元数据，正文未改）。
- `internal/store/upgrade/00-latest-schema.sql`：正文首段注释统一改称「全量脚本」（与 AGENTS.md、代码注释术语一致）。
- `internal/store/upgrade/04-collections.sql`：版本头描述去中文，改为 `(F9)`，满足「首行英文」约定（仅元数据，正文未改）。
- `migrate_test.go` 重写：不再断言版本连续，改为断言「按前缀升序、最高版本等于 `SchemaVersion`、版本头与文件名一致」。

## Fixed

- `Open()` 在任意库上都会因「升级脚本版本不连续：期望 v2，实际 03-app-settings.sql」失败（新库同样失败，DB 完全打不开）。
- `03-app-settings.sql` / `04-collections.sql` 被错误解析为 `v3 -> v4` / `v4 -> v5` 的错误区间。

## Docs

- 无业务文档变更；约定来源 `.agents/skills/db-schema/SKILL.md` §1/§3/§6。
- `AGENTS.md`：目录规则中 `00-latest-schema.sql` 统一称 full-schema script，术语与代码一致。
- `internal/services/summary_service.go`：删除指向已移除脚本 `upgrade/01-receipt-no-unique.sql` 的过期注释（唯一性由部分唯一索引 `uq_data_summaries_receipt_no` 兜底）。

## Scope / Impact

- 仅影响 `internal/store` 的迁移路径与本目录脚本的元数据行；不改任何表结构、不改业务代码、不涉及前端。
- 历史遗留库（旧编号方案下已到 `user_version` 3/4/5 的库）无法通过增量脚本自动修复：`01`/`02` 已在 `b288672` 删除，编号语义变更后其版本含义与新方案冲突。这类库需人工核对缺失对象（如 `app_settings`、`collections`、`bank_flows.raw_json`）后手工补建，或重建库。仓库根目录的空库 `finlite.db` 已无业务数据，可直接删除重建。
- 本次未修改 `03`/`04` 的 SQL 正文；两脚本仍带 `IF NOT EXISTS` 防御子句，与 db-schema 技能 §4 不符，待确认后再单独修订。

## Verification

- `gofmt -l main.go internal/`：干净。
- `go vet -tags=gtk3 ./...`：无问题。
- `go build -tags=gtk3 .`：成功。
- `go test -tags=gtk3 ./internal/...`：40 例全部通过（含上述新增/重写用例）。
- 手工核对：`PRAGMA user_version` 新库 = 4；`03`/`04` 按升序执行后结构与全量脚本指纹一致。
