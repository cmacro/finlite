---
title: MVP 闭环修复：余额校验、对账差异、凭证模板与八个页面接线
description: 修复银行余额校验（顺序无关 + 整批口径）、对账差异遗漏、汇总状态机、收据号唯一、余额表期初，实现 §6.5 五类凭证模板与备份恢复，并接通 F1/F3/F4/F5/F6/F7/F8 全部前端页面
---

# 20260923 mvp_flow_completion

## Added

- `internal/services/xlsx.go`：XLSX 读取（仅标准库 `archive/zip` + `encoding/xml`，首工作表，共享字符串/内联字符串/显式列引用补齐），未引入新依赖。
- `internal/services/import_service.go`：
  - `ParseAnyFile` / `detectDelimiter`：TSV/CSV 按首行检测分隔符（制表符优先，其次逗号）。
  - `PreviewColumns` + `ColumnMapping` 模型：导入前列映射预览（识别/缺失关键列 + 样本行），对应 §7 F1。
  - `checkBankBalance` / `orderBankRows` / `chainByBalance`：余额校验改为顺序无关。
  - `raw_json`：银行流水整行入库存档（附录 A「对方账号→存 raw」）。
- `internal/services/voucher_service.go`：`BuildEntries` 实现 §6.5 五类凭证模板（POS 收入 / 产品成本 / 护理人力 / 手续费 / 房租水电），自动配平并由 `Create` 复核；`backfillSourceVoucherID` 按收支口径回写 `voucher_id`，打通 §10 反查链。
- `internal/services/backup_report_service.go`：`BackupService.Restore`（校验备份可打开 → checkpoint → 留 `pre-restore` 快照 → 覆盖并清 WAL/SHM）。
- `internal/store/migrate.go`：按 `PRAGMA user_version` 顺序执行 `upgrade/NN-*.sql` 增量脚本；新库走 baseline，旧库逐脚本升级，版本必须连续。
- `internal/store/upgrade/01-receipt-no-unique.sql`、`02-bank-flows-raw-json.sql`（均同步 `00-latest-schema.sql`）。
- 前端接线（此前均为 `disabled` 桩）：`OrderFlow.svelte`（F3）、`Recon.svelte`（F4）、`Summary.svelte`（F5，含编辑表单）、`Voucher.svelte`（F6，含详情与打印预览）、`Balance.svelte`（F7，含收支日报）、`Settings.svelte`（F8，科目/费率/备份恢复）、`Import.svelte`（F1，列映射预览 + xlsx/csv 上传）。
- 样式：`modal-box-wide`、`input.num-input`。
- 回归测试 `import_recon_test.go`、`xlsx_test.go`（9 例新增，覆盖顺序无关校验、真实断点、状态机、收据号唯一、五类模板、期初承接、银行独有日期、CSV/XLSX 解析）。

## Fixed

- **银行余额校验恒误报**：整批公式 `last-first == Σin-Σout` 未剔除首行净额（递推式首行已被首余额吸收），必然报「整批不平」；改为自第 2 行起累加，并先按 `txn_time` 排序、同秒内按余额衔接关系重建真实顺序。样本 `银行流水.tsv` 由「19/20 行不平」修正为「20/20 正常」。
- **对账差异遗漏**：`ListDiffs` 仅遍历订单结算日，银行侧独有日期的差异被静默吞掉；改为订单 ∪ 银行 POS 日期并集，并按差异字段（订单 vs 银行 / 银行 vs 汇总）分类定位。
- **汇总状态机漏洞**：`vouchered` 汇总可被驳回成 `draft`（凭证仍在，破坏「一汇总一凭证」）；现在 `vouchered` 为终态并给出中文指引。
- **收据号唯一未实现**：新增服务层查重（空号不占用）+ 数据库部分唯一索引兜底。
- **余额表期初恒 0 且金额走 float**：`SUM(CAST(... AS REAL))` + `float64` 扫描违反「金额禁用 float64」，且按日期过滤导致期初永远为 0；改为取全部分录后在 Go 内用 decimal 分期累加，展示名取科目表标准名。`期初 = 期间前累计`、`期末 = 期初 + 借 - 贷`（按科目方向）。
- **CSV/XLSX 名为支持实则按 TSV 解析**（`Comma` 固定为 `\t`）；现按首行检测分隔符并支持 XLSX。
- 导入中心死代码 `isSummaryRateRow` 移除（汇总不经导入中心受理）；`readTSVRows` 合并入 `ParseAnyFile` 路径。
- `GreetService` 脚手架与重复的 `frontend/src/lib/types.ts` DTO 定义移除（绑定改为唯一来源，`types.ts` 只保留 `RouteKey`）。

## Changed

- `internal/store/sqlite.go`：`SchemaVersion` 1 → 3；新增 `Checkpoint()`（备份/恢复前落盘 WAL）。
- `store.migrate` 拆分为 `migrate.go`，支持增量升级（旧库就地升级，行数保留）。
- `import_service.go` 重构：解析与校验分离，余额校验结果按行号回填，`用途+摘要` 合并入 `summary`。
- 文档同步：§6.3 校验顺序与整批口径、§5 状态机终态、§7 F1/F6 描述、§10 恢复留快照、附录 A 导入格式；`.agents/skills/sqlite-decimal-ledger/SKILL.md` 同步修正余额校验口径。
- `frontend/bindings/` 由 `wails3 generate bindings -ts -i` 重新生成（11 Services / 32 Methods / 25 Models），未手改。

## Scope / Impact

- 影响导入（F1）、对账（F4）、汇总（F5）、凭证（F6）、报表（F7）、设置（F8）与银行流水展示口径（F2）。
- 原始表 `bank_flows` / `order_flows` 仍只读，无 UPDATE 接口；`voucher_id` 为新增的唯一回写字段（§10 反查用）。
- 旧库首次打开会自动升级至 v3，已用真实 v1 库（2269 行银行流水）验证行数与结构保留。

## Verification

- `gofmt -l internal main.go`：干净。
- `go vet -tags=gtk3 ./...`：无问题。
- `go test ./internal/store/... ./internal/decimal/... ./internal/services/...`：全部通过（14 例，含 9 例新增）。
- `go build -tags=gtk3 .`：成功。
- `npm run check --prefix frontend`：0 errors / 0 warnings；`npm run build`：成功。
- 浏览器实测（server 模式 + headless Chrome，脱敏样本库）：八个页面全部加载真实数据，无桩态；F2 确认/撤销红绿互转、F4 差异定位、F5 新增/审核/驳回、F6 模板生成+审核登账+打印预览、F7 登账后期末勾稽、F8 费率更新/备份/恢复全部走通。
- §11 贯穿示例复演：7-10 `11970.00`、7-11 `9256.80`、7-12 `446.88` 三方一致；五类凭证各出一张且借贷相等。
- 脱敏检查：原值仅命中映射表与本地未提交的 `bin/finlite.db`（`bin/` 已 gitignore）。
