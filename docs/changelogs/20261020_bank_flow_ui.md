---
title: 银行流水页按 UI 设计稿改版（批次信息栏 / 统计卡 / 余额衔接 / 抽屉）
description: 对齐 docs/uidesign/bank-flow.html 重做银行流水界面，并为 List/Batches/ExportTSV 补齐后端展示数据与导出能力
---

# 银行流水页按 UI 设计稿改版

## Added

- `BankFlowService.Batches`：返回银行流水批次统计（导入时间/行数/成功/失败、余额不平行数、已确认时间与行数）。
- `BankFlowService.ExportTSV`：按当前筛选导出 TSV，金额沿用数据库 `decimal` 字符串，不做二次计算；保存路径由前端保存对话框选择。
- 银行流水行展示字段：对方账号（读取 `raw_json`）、POS 清算标记、上行余额、应到余额、余额差额、关联凭证号、原始 JSON。
- 侧栏「银行流水」余额不平徽标；顶栏「会计期间」胶囊、`导出 TSV`、`导入明细`。
- 行内展开的余额衔接校验、相邻行定位、本地「已核对」标记、右侧行详情抽屉、Toast 提示。
- 新增页级样式 `frontend/src/styles/bankflow.css`（`bf-` 前缀，避免与导入中心样式冲突）。

## Changed

- 银行流水页 (`frontend/src/routes/BankFlow.svelte`) 按设计稿重做：只读提示条 → 批次信息栏（确认/撤销需二次确认）→ 统计卡 → 筛选条 → 密集表格 → 详情抽屉。
- 列表改为一次返回全部明细（DESIGN.md §7 禁止分页），并附带转入/转出合计、转入/转出笔数、不平/已确认/跳过行数。
- 行状态显示与设计稿一致：`正常` 绿、`不平` 整行红底 + 红左边框、`跳过` 灰、`已确认` 仅用于「原不平且已确认」行。
- 同一交易秒内的多行按导入时的余额衔接算法重排后再计算上行余额，保证展开等式与入库 `balance_status` 不矛盾。
- `BankFlowFilter` 移除分页参数；`Confirm.svelte` 支持可选标题、按钮文案与 info/danger 语气（向后兼容）。
- 页面底色新增 `canvas` token，顶栏/侧栏/弹窗补充对应组件样式。

## Docs

- 无业务文档变更；实现依据 `docs/uidesign/bank-flow.html` 与 `docs/uidesign/DESIGN.md`。

## 影响范围

- 仅影响银行流水页及其读取的展示字段、批次统计与导出；原始表仍只读，确认仅写 `balance_confirmed_at` 且可撤销。
- 金额全部由 Go `decimal` 计算，前端只格式化；账务规则、对账与凭证逻辑未改动。

## 验证

- `gofmt -l internal main.go`：无输出。
- `go vet -tags=gtk3 ./...`：通过。
- `go build -tags=gtk3 .`：通过。
- `go test -tags=gtk3 ./internal/...`：17 个测试通过。
- `npm run check --prefix frontend`：0 errors / 0 warnings。
- `npm run build --prefix frontend`：构建成功。
- 冒烟：用 `docs/raw/银行流水.tsv` 导入 20 行，逐行余额衔接全部对平；批次确认/撤销状态切换、批次统计与导出条数一致；筛选后首行仍取到批次内真实上行余额。
- 脱敏检查：本次变更文件对 `docs/raw/anonymization_mapping.tsv` 原值 grep 无命中。
