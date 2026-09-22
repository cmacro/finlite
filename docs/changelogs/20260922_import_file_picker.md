---
title: 导入中心接线：选文件 + 按文件名自动识别类型
description: Import.svelte 接通 ImportService，新增 ImportFile 统一入口，按文件名关键字自动匹配银行流水/订单流水
---

# 20260922 import file picker

## Added

- `ImportService.ImportFile(ctx, fileName, content)`：统一导入入口，按文件名自动识别类型（`DetectSourceType`：含「银行」/bank → `bank_flow`；含「订单」/order/pos → `order_flow`；含「汇总」/summary → 提示不受理并指路数据汇总页）。
- 前端导入中心选文件：`<input type="file">`（`.tsv/.csv`），读取文本内容后传后端；Service 层不引入 wails 对话框依赖，`make vet` 门禁不受影响。
- `ImportResult` 新增 `sourceType` 字段；`ListBatches` 返回并展示批次类型列。
- 单测 `internal/services/import_test.go`：`DetectSourceType` 8 组用例 + `parseTSVText` BOM/金额列解析。

## Changed

- `import_service.go` 重构：拆出 `parseTSVText`（文本解析）与 `importBankRows` / `importOrderRows`（导入核心，供路径入口与统一入口共用）。
- 订单流水导入去重改用 `RowsAffected` 判定，`INSERT OR IGNORE` 重复行与错行均计入 `SkipCount`，`success + skip == row_count` 严格成立（原先用差值推算且成功数虚增）。
- `readTSVRows` 改为读全量后复用 `parseTSVText`。
- 导入批次表新增「类型」列，页面加载即拉取批次列表。

## Docs

- 本文件；业务依据 `docs/business/00-overview-health-finance-flow.md` F1，未改动。

## Scope / Impact

- 仅导入链路（F1）；银行/订单流水的 Service 方法签名、SQL schema、其他页面均未改动。
- 绑定由 `wails3 generate bindings -ts -i` 重新生成（30 Methods），未手改。

## Verification

- `gofmt -l internal main.go`：干净。
- `go vet ./internal/store/... ./internal/decimal/... ./internal/services/...`：无问题。
- `go test`：5 个测试全过（含新增 2 组）。
- `npm run check --prefix frontend`：0 errors, 0 warnings；`npm run build`：成功。
- `go build ./...` 仍受本机缺 GTK4 头文件影响（既有环境问题，未新增）。
- 脱敏 grep：改动文件无原值/真实名称命中。