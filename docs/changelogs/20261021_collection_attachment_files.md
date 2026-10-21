---
title: 店面收款附件外置与上传压缩
description: 收款单据照片不再存 SQLite BLOB，改为账套 attachments/ 目录内容寻址文件；上传前前端压缩到长边 2000px／JPEG 0.7，旧库 BLOB 开库自动外置。
---

# 店面收款附件外置与上传压缩（2026-10-21）

## Added

- `internal/store/upgrade/06-attachment-files.sql`（schema v5 → v6，基线同步）：
  `collection_attachments` 重建为 `(id, collection_id, file_name, ext, size_bytes, sha256, rel_path, sort_no, created_at)`，
  新增 `idx_collection_attachments_sha256`，移除 `content BLOB`
- `internal/store/attach_files.go`
  - `PreMigrateAttachments`：开库前置，把旧库 `content` 里的 BLOB 写到账套
    `attachments/YYYY/MM/<sha256>.<ext>` 并回填 `sha256`/`rel_path`（幂等；失败中止开库，不丢凭据）
  - `PutAttachmentFile` / `ReadAttachmentFile` / `RemoveAttachmentFileIfUnused`：内容寻址写入、越界安全读取、
    按引用计数清理（同一张图被多条记录引用只存一份，引用归零才删文件）
  - `Queryer` 接口：写事务内必须传 `*sql.Tx`（单连接池下用 `*sql.DB` 查询会自锁）
- 前端 `Collection.svelte`：上传前图片压缩（长边 ≤ 2000px、JPEG 质量 0.7，已足够小的图跳过），
  重编码后扩展名同步改为 `jpg`；PDF 不压缩；压缩后仍超 10MB 的跳过并提示

## Changed

- `CollectionService.GetAttachment`：从账套附件文件读取，前端接口签名不变（仍返回 base64 + mime）
- `CollectionService.Save`：附件新增走「文件落盘 → 写 `sha256`/`rel_path`」，删除走「删行 → 引用归零则删文件」
- 收款附件提示文案：说明自动压缩与账套 `attachments/` 存放

## Fixed

- 单元测试卡死：`PutAttachmentFile` / `RemoveAttachmentFileIfUnused` 在写事务内用连接池查询，
  `MaxOpenConns(1)` 下等待自己持有的事务提交 → 自锁。改为事务内传 `*sql.Tx`
- `Makefile` `test` 目标加 `-timeout 120s`：单测挂住时 2 分钟失败，而不是等默认 10 分钟

## Docs

- `docs/business/00-overview-health-finance-flow.md`：§5 附件规则、§8 `collection_attachments` 表结构、
  §8.1 第 3 条（附件外置）同步为外置 + 内容寻址 + 压缩口径

## Verification

- `gofmt -l ./internal ./main.go`：无输出
- `go vet -tags=gtk3 ./...`：通过
- `go build -tags=gtk3 .`：通过
- `go test -timeout 120s ./internal/store/... ./internal/decimal/... ./internal/services/...`：
  55 个用例通过（store 27 + decimal 2 + services 26），总耗时约 2s
  （新增：附件外置/引用共享/删除清理/旧库 BLOB 外置）
- `npm run check --prefix frontend`：0 errors / 0 warnings
