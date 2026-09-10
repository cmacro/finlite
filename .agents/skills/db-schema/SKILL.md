---
name: db-schema
description: Generate and manage SQLite schema upgrade scripts in internal/store/upgrade/. Use when creating or changing database tables, columns, indexes, or data migrations, or when syncing 00-latest-schema.sql.
---

# DB Schema Upgrade (SQLite)

You generate and manage incremental SQL schema upgrade scripts for finlite's SQLite single-file ledger.

## Project Context

- Stack: Go 1.25, SQLite single file `finlite.db`, pure-Go driver (`modernc.org/sqlite`, no CGO), `database/sql` + raw SQL only, WAL mode, single-writer mutex in `internal/store`.
- Business source: `docs/business/00-overview-health-finance-flow.md` §8. Money/types: see `sqlite-decimal-ledger` skill (amounts `TEXT`, dates `YYYY-MM-DD`, timestamps `RFC3339`).
- Upgrade scripts location: `internal/store/upgrade/` (create the directory if missing).
- Baseline schema: `00-latest-schema.sql` in the same directory. A fresh open from the baseline must produce the same schema as applying all incremental scripts sequentially.
- Version tracking: `PRAGMA user_version` records the applied version; pending scripts run in order at open, each exactly once.

## Rules

### File Naming

```
{fromVersion}-{short-kebab-description}.sql
```

- `fromVersion`: integer schema version this script upgrades **from**, zero-padded to 2 digits (e.g. `02`, not `2`).
- Description: concise English, kebab-case.
- Examples: `02-add-voucher-entries.sql`, `03-settle-date-index.sql`.
- Baseline file name: `00-latest-schema.sql`.

The version sequence is defined by existing files: sort them, take the highest `fromVersion + n`; the new script's `fromVersion` must equal that highest script's `fromVersion + 1` (or `00` for the first incremental script ever).

### Mandatory Header

The first line must match:

```sql
-- v{FROM} -> v{TO} (compatible with v{COMPAT}+): {ONE_LINE_DESCRIPTION}
```

- `FROM`: starting version integer.
- `TO`: ending version integer; always `FROM + 1`.
- `COMPAT`: oldest schema version allowed to jump directly to the latest version.
- Example: `-- v2 -> v3 (compatible with v0+): Add settle-date index for recon queries`.

### Transaction Control

Each script runs inside a single SQLite transaction by default (SQLite DDL is transactional), with the `user_version` bump committed in the same transaction. To override, add immediately after the header:

```sql
-- transaction: off
```

Allowed values: `on` (default), `off`.

### SQL Content

- **Incremental only**: new tables, columns, indexes, and data migrations for this version. Never repeat prior DDL.
- **SQLite types**: `TEXT` for ids, amounts (decimal strings), dates, timestamps, and JSON; `INTEGER` for counts/flags; `BLOB` for binary. Never `REAL`/`FLOAT` for money.
- **Idempotency**: `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`. `ALTER TABLE ... ADD COLUMN` has no `IF NOT EXISTS` in SQLite; rely on version gating (each script runs once) and do not add extra guards.
- **Data migrations**: write `WHERE` clauses so they only touch existing data, not future rows.
- **Index naming**: `idx_<table>_<columns>` or `uq_<table>_<columns>` for unique indexes.
- **CHECK constraints**: documentation only, never the sole enforcement; money/balance rules are enforced in Services with `shopspring/decimal` (see `sqlite-decimal-ledger`).
- **Destructive operations**: no `DROP TABLE`/`DROP COLUMN`, and no destructive change to the append-only raw tables (`bank_flows`, `order_flows`), unless explicitly required by the task.
- **ALTER TABLE limits**: SQLite supports `ADD COLUMN` / `DROP COLUMN` / `RENAME`; there is no `MODIFY COLUMN`. Column changes that SQLite cannot express go through a table rebuild inside a new version script.

### SQLite Compatibility

Scripts must run on SQLite via `modernc.org/sqlite`. Forbidden:

- MySQL dialect (`AUTO_INCREMENT`, `UNSIGNED`, `ENGINE=`, `CHARSET=`, backtick-required syntax, `DATETIME` defaults like `ON UPDATE CURRENT_TIMESTAMP`).
- Any non-SQLite function or CGO-dependent extension.
- Changing the database driver; this skill never switches drivers.

### Baseline Synchronization

After creating an upgrade script, update `00-latest-schema.sql`:

- New table: append the full `CREATE TABLE`.
- New column: add the column inside the matching `CREATE TABLE`.
- New index: append the `CREATE INDEX`.

A fresh database from the baseline must match the result of applying all incremental scripts sequentially.

## Generation Template

```sql
-- v{FROM} -> v{TO} (compatible with v{COMPAT}+): {ONE_LINE_DESCRIPTION}

-- New table example
CREATE TABLE IF NOT EXISTS {table_name} (
    id         TEXT NOT NULL PRIMARY KEY,
    date       TEXT NOT NULL,
    amount     TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'draft',
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_{table_name}_date_id
ON {table_name} (date, id);

-- New column example (version gating guarantees single application)
ALTER TABLE {table_name} ADD COLUMN remark TEXT NOT NULL DEFAULT '';

-- New index example
CREATE INDEX IF NOT EXISTS idx_{table_name}_date
ON {table_name} (date);
```

## Post-Generation Checklist

Before reporting completion, verify every item:

- [ ] File name matches `{from}-{description}.sql`, `fromVersion` continues the sequence.
- [ ] First line header matches `-- vN -> vM (compatible with vK+): ...`.
- [ ] New/modified tables or columns are synced to `00-latest-schema.sql`.
- [ ] SQL is valid SQLite; no MySQL dialect or forbidden features.
- [ ] Money columns are `TEXT`; enforcement lives in Services, not SQL.
- [ ] `gofmt -l ./...`, `go vet ./...`, `go build ./...` pass (scoped to affected packages when the full build needs system webkit deps).
- [ ] `git diff` contains no changes to already-published historical scripts.

## Forbidden

- Modifying `vendor/`, generated `frontend/bindings/`, or live `finlite.db` files.
- Modifying published historical scripts that users have already executed; fixes go through a new version.
- Using MySQL dialect or any non-SQLite syntax.
- `REAL`/`FLOAT` columns for money.
- Skipping `00-latest-schema.sql` synchronization.

## After Generation

Run the checklist above and ask the user for guidance if any step cannot be completed. Do not skip silently.
