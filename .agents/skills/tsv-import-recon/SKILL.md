---
name: tsv-import-recon
description: TSV import, dedup, balance check, and triple-match reconciliation. Use when implementing import, bank/order lists, recon center, fee checks, or summary prefill.
---

# TSV Import + Reconcile

Sources: `../ForgeFin/docs/raw/*.tsv`. Spec: business doc §4-§6, App. A.

## Parsing

- Encodings: UTF-8 (+BOM strip). Delimiter: TAB first, fallback comma. Use `encoding/csv` with `Comma='\t'`, `LazyQuotes=true`.
- Amounts: strip `,`, trim space, empty -> `0`, else `decimal.NewFromString`. Keep raw string for error messages.
- Bank time `2026-07-10 02:45:36`; order `txn_time` + `settle_date(YYYY-MM-DD)`; summary `date(YYYY-MM-DD)`.
- Summary file row 2 is a rate header (`0.005/0.006/0.0025`): skip when first col empty + row contains rates.
- Always record `import_batches(file_name,row_count,success,skip,balance_warning)` + per-row `source_row_no`.

## Column maps

- Bank: `凭证号→ignore | 对方账号→raw | 交易时间→txn_time | 对方单位→counterparty | 用途+摘要→summary | 附言→remark | 转入→in_amount | 转出→out_amount | 余额→balance`. POS iff `remark==<示例商户号，已脱敏>` or counterparty contains `POS净收`.
- Order: keep `icbc_order_no(UNIQUE)`, `third_order_no`, `order_amount`, `fee_amount`, `net_amount`, `pay_method`, `debit_credit_flag`, `settle_date`, rest -> `raw_json`.
- Summary: `日期/收据编号/一级/项目/事由/手续费/实际收入/支出/余额/备注`.

## Dedup

- Order: `icbc_order_no` UNIQUE; reimport same key -> skip count, not error.
- Bank: no natural key; dedup on `(txn_time,in_amount,out_amount,counterparty)`; report `skip_count`.
- Never silently overwrite; always report success/skip/fail per batch.

## Checks

1. **Fee check (order):** expected `order_amount * rate` (debit 0.005 / credit 0.006 / WeChat-Alipay 0.0025), 2-dec round. Diff > 0.01 -> flag. Manual summary edit sets `fee_manual_adjusted=1`.
2. **Bank balance:** strict decimal equality, no tolerance (see `sqlite-decimal-ledger`). Smoke rows: 07-10 `11970.00`, 07-11 `9256.80`, 07-12 `446.88`.
3. **T+1:** income date = `settle_date` (= txn_date + 1). Recon groups by `settle_date`.
4. **Triple-match per date:** `sum(order.net) == bank POS inflow == sum(summary.actual_income)`. 07-01 diff `0.45` is the canonical pending case. Diffs -> pending queue, never block other dates.

## Summary prefill

`GenerateFromRecon(date)`: from matched recon create `data_summaries(status='draft',source='auto')`, fee prefilled, reason/remark left for manual补录. Only `reviewed` -> voucher.

## Checklist

- BOM/comma-amounts handled? Rate-header row skipped?
- Dedup keys correct? Batch counts accurate?
- Strict balance + fee + triple-match implemented with decimal?
- Pending diffs listed with drill-down to file+row?
- Anonymization: mapping `docs/raw/anonymization_mapping.tsv` applied first (longest-match)? No real names in code/samples/errors?
