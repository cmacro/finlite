---
name: sqlite-decimal-ledger
description: SQLite single-file ledger with decimal money handling. Use when touching schema, migrations, balance checks, vouchers, audit logs, or backup.
---

# SQLite Decimal Ledger

Single DB `finlite.db`. Driver: pure-Go (`modernc.org/sqlite`, no CGO). Access: `database/sql` + raw SQL only, no ORM. WAL mode, single-writer mutex in `internal/store`.

Business source: `docs/business/00-overview-health-finance-flow.md` §8.

## Money

- Go: `shopspring/decimal` end-to-end. Never `float64` for amounts.
- SQLite: store as `TEXT` (canonical decimal string). Parse on read, `String()` on write.
- Display: 2 decimals. Compare with `decimal.Equal`, add/sub with decimal ops only.

```go
amt, err := decimal.NewFromString(s) // after stripping "," and trim
sum := a.Add(b).Sub(c)
if !sum.Equal(expected) { /* mismatch */ }
```

## Tables (see business doc §8)

`import_batches`, `import_errors`, `bank_flows`, `order_flows`, `data_summaries`, `vouchers`, `voucher_entries`, `accounts`, `fee_rates`, `audit_logs`.

Conventions:

- PK `id TEXT` (uuid), dates `YYYY-MM-DD`, timestamps `RFC3339`.
- `internal/store/*.go` owns open/migrate/query. Services own rules, never inline DDL in Services.

## Mandatory rules

1. **Bank balance, strict, no tolerance:**
   Reorder rows before checking: sort by `txn_time`, then within the same second rebuild the real order by balance chaining (`prev + in - out == balance`). Network-bank exports scramble both (the sample file is descending). Fall back to file order when the chain is ambiguous, and report mismatch honestly.
   `balance == prev_balance + in - out` per row; `last - first == sum(in) - sum(out)` over rows **from the second onward** (the first row's net is already absorbed into `first`).
   Mismatch still imports (`balance_status='mismatch'` + `import_errors`), cleared only by batch confirm (`balance_confirmed_at`), revocable.
2. **Voucher balance:** `sum(debit) == sum(credit)` via decimal. Reject with Chinese diff message.
3. **State gates:** only `reviewed` summary -> voucher; one summary -> one voucher.
4. **Raw read-only:** no UPDATE API for `bank_flows`/`order_flows`. Fix by reimport/adjustment.
5. **Audit:** every summary edit/audit/voucher action inserts `audit_logs(old/new JSON)`.

## Backup

File-level copy of `finlite.db` to `backups/finlite_YYYYMMDD_HHmmss.db`. Restore requires confirm dialog + app restart. Never `cp` a WAL DB while writer holds it; checkpoint/close first.

## Checklist

- Amounts TEXT + decimal ops? Dates in correct format?
- Migration idempotent (`CREATE TABLE IF NOT EXISTS`)?
- Balance/voucher/audit rules enforced in Service, not frontend?
- Indexes on `bank_flows(txn_time)`, `order_flows(settle_date)`, `data_summaries(date,status)`?
