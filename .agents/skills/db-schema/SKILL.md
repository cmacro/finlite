---
name: db-schema
description: Generate and manage SQLite incremental schema upgrade scripts and the latest-schema baseline, including version numbering, one-line version headers, compatibility clauses, and baseline synchronization.
---

# DB Schema Upgrade (SQLite)

Schema evolution for a single-file SQLite database = `00-latest-schema.sql` (the **full schema file**: a complete, runnable SQL dump of the whole database) plus a set of incremental scripts. This skill defines the SQL script upgrade conventions only: file naming, version numbering, headers, SQL content, and full-schema synchronization.

## 1. Version Model

- **v1**: the first version, created from nothing, carried by the baseline `00-latest-schema.sql`. There is no `01-*.sql`.
- **`00-latest-schema.sql` is a full schema file**, not a fragment and not an appended patch: it holds every table, index, view, trigger, and seed row needed to create the whole database from empty, in one runnable order.
- **It is executed only when the database is initialized** (no database, or no schema yet). An existing database never runs it — it applies the pending incremental scripts, starting from the version it already stores.
- **The baseline is the single source of version truth**: the `TO` value in the baseline's first line *is* the current version, i.e. the latest schema version.
- **A new incremental script's version = baseline `TO` + 1**; once the script lands, update the baseline `TO` to that version.
- Read the version only from the baseline header; never infer it from the number of files.
- Each new script raises the version by exactly one: when the baseline `TO` is `V`, the next script is `{V+1}-*.sql`.
- Version numbers are history and never move backward. A gap is allowed: a script may be deleted by hand once a later script makes it unnecessary — e.g. `04-*.sql` added a column and `05-*.sql` removes that column again, so `04` has no net effect. Deleting a script never changes the baseline `TO`; a gap is never filled and never renumbered.

Example: baseline header `-- v0 -> v7 ...` → new script `08-*.sql` with header `-- v8 ...`, and the baseline becomes `-- v0 -> v8 ...`.

## 2. File Naming

```
{VER}-{short-kebab-description}.sql
```

- `VER`: the schema version this script introduces = baseline `TO` + 1, zero-padded to at least two digits (`02`, `07`, `100`).
- Description: short English kebab-case, one phrase describing the schema change.
- The baseline file name is fixed: `00-latest-schema.sql` (the full schema file), with no version in it.
- Never create `01-*.sql`; never rename or renumber a script — numbers are stable history.
- A script may be deleted only when a later script makes it unnecessary: that later script must reach the same final schema whether or not the deleted one was applied (typically because it reverses or rebuilds the earlier change). Judging that is a manual decision, never an automatic step; the gap it leaves stays.

## 3. Header Format

The first line of every `.sql` file must be a version header: single line, English, no trailing period, no trailing whitespace; leave one blank line after it before the body. The header is metadata only and is not executed, so it must be checked by hand.

`{ONE_LINE_DESCRIPTION}` is a short, standard-English phrase: sentence capitalisation, no trailing period, no abbreviations, and it states what the version changes.

### 3.1 Full schema file: `00-latest-schema.sql`

```sql
-- v0 -> v{TO} (compatible with v{COMPAT}+): {ONE_LINE_DESCRIPTION}
```

- `FROM`: fixed `0` — an empty database starts at v0, before any schema version exists.
- `TO`: the current version (latest schema version).
- `COMPAT`: the **modification base version** carried by the baseline — the maximum of the incremental scripts' `COMPAT` values, i.e. the highest script version that a later script corrected. If scripts carry `v4+` and `v3+`, the baseline carries `v4+`, even though a script may itself carry the older `v3+`. Never `v0+`; when no incremental script carries a `COMPAT`, write `v1+`.
- Examples: `-- v0 -> v7 (compatible with v4+): Latest schema`; a database with no incremental scripts yet reads `-- v0 -> v1 (compatible with v1+): Latest schema`.

### 3.2 Incremental scripts: `{VER}-*.sql`

No arrow. Exactly two forms:

**Form A — no v2+ upgrade script is involved**

```sql
-- v{VER}: {ONE_LINE_DESCRIPTION}
```

- A brand-new object that depends on nothing introduced by a v2+ script: a new table, a new namespace, or a new index (an index is a new object; it does not change the definition of the table it sits on); or
- a change that only touches content defined in **v1** — v1 is the base version carried by the baseline, so changes to it carry no `COMPAT`.

**Form B — the script corrects or extends content of a v2+ upgrade script**

```sql
-- v{VER} (compatible with v{COMPAT}+): {ONE_LINE_DESCRIPTION}
```

- `COMPAT` = the version of the upgrade script whose content this script corrects or extends (`2 <= N < {VER}`): v11 changes content of the v4 script → `-- v11 (compatible with v4+): ...`; v12 changes content of the v3 script → `-- v12 (compatible with v3+): ...`.
- Never write `v0+`. Content that comes from v1 (the baseline) needs no `COMPAT` — that is Form A.

**Decision rule**: locate which upgrade script's content this change lands on —

- content of the baseline (v1), or a brand-new object that depends on nothing from v2+ → Form A;
- content of the vN upgrade script (`N >= 2`) — a new column on a table that the vN script created, or a replacement of an index it created — → Form B with `COMPAT = N`;
- when one script touches several places spanning multiple versions, take the **largest** `N`.

## 4. SQL Content

### Structure

- Index naming: `idx_<table>_<columns>`, unique indexes `uq_<table>_<columns>`.
- An incremental script contains only this version's changes: no repeated historical DDL, no copy of the whole schema (a table rebuild is the only exception).

### `ALTER TABLE` limits

- Supported: `ADD COLUMN`, `RENAME COLUMN`, `DROP COLUMN`, `RENAME TO`.
- Not supported: `MODIFY COLUMN`, `ALTER COLUMN`, `ADD COLUMN IF NOT EXISTS`.
- A new `NOT NULL` column must carry a non-null **constant** `DEFAULT` (`DEFAULT ''`, `DEFAULT '{}'`, `DEFAULT 0`).
- A new column cannot be `UNIQUE` / `PRIMARY KEY`; express uniqueness with a following `CREATE UNIQUE INDEX`.
- Defaults cannot be functions or expressions.
- Changes SQLite cannot express (type, constraint, primary key) require "create new table → migrate data → rename", rebuilding indexes and constraints in the same script.

### Data migrations

- Write explicit `WHERE` clauses that touch only the intended existing rows.
- Handle NULLs, duplicates, and existing rows that would violate new constraints.
- Adding a column with a `DEFAULT` is not a data migration; when backfilling is required, run an explicit `UPDATE`.
- Never silently discard historical financial data or change established business semantics.

### Types

- `TEXT`: ids, amounts (decimal strings), dates, timestamps, JSON.
- `INTEGER`: counts, flags. `BLOB`: binary data.
- Never `REAL` / `FLOAT` for money; money and balance rules are enforced by Services with decimal arithmetic — SQL `CHECK` is only a backstop.
- JSON columns default to `'{}'`, never `''` (an empty string is not valid JSON).

### Body style

The version header is English only. Body comments may use the project's working language; they explain the business reason and cite the relevant business-document section. Order: header → blank line → comments → DDL. Note on money columns that amounts are `TEXT` (decimal strings); dates are `YYYY-MM-DD`; timestamps are RFC3339.

### Destructive changes

- Destructive DDL (`DROP TABLE`, `DROP COLUMN`, `RENAME`) belongs to a version script that rebuilds or repairs a structure; it is gated by the script version like any other change, so no extra permission flag is needed.
- A rebuild must preserve data, indexes, constraints, triggers, and views.
- Append-only raw tables are only extended (new columns), never destructively changed, and historical data is never silently dropped.

## 5. Templates

**Form A: new table**

```sql
-- v{VER}: Add {object} table for {purpose}

-- Explain the business reason and cite the business-document section.
CREATE TABLE {object} (
    id         TEXT NOT NULL PRIMARY KEY,
    {date_col} TEXT NOT NULL,
    amount     TEXT NOT NULL,
    status     TEXT NOT NULL DEFAULT 'draft',
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX uq_{object}_{date_col}_id
ON {object} ({date_col}, id);
```

**Form B: change content of a v2+ upgrade script**

```sql
-- v{VER} (compatible with v{COMPAT}+): Add {object}.{column} to {purpose}

-- Explain the business reason and cite the business-document section.
ALTER TABLE {object} ADD COLUMN {column} TEXT NOT NULL DEFAULT '{}';
```

`{COMPAT}` is the version of the script whose content this change corrects (`2 <= {COMPAT} < {VER}`). If that content comes from v1 (the baseline), use Form A instead.

## 6. Full Schema Synchronization

After adding an incremental script, update `00-latest-schema.sql`:

- New table: add the complete `CREATE TABLE` (with constraints).
- New column: add it inside the matching `CREATE TABLE`.
- New index / trigger / view: add the final definition.
- Rebuilt table: write the resulting table plus its indexes and constraints.
- Data migration: sync the final **structure** only (seed data aside); historical backfills do not belong in the full schema file.

Rules:

- `00-latest-schema.sql` stays a complete, runnable SQL file: executed on an empty database it must create the whole database in one pass — tables first, then indexes, views and triggers, then seed data.
- It is a schema definition, not a concatenation of migration SQL.
- Only initialization executes this file. An existing database starts from the version it already stores and runs the pending incremental scripts in order — changing the file alone upgrades nothing.
- Set the `TO` in the header to the new version, and set `COMPAT` to the maximum `COMPAT` among all incremental scripts (never below `v1+`).
- The full schema file must equal the schema an empty database reaches after running every existing incremental script in order.

## 7. Checklist

- [ ] File name follows `{VER}-{kebab}.sql`, with `VER` = the baseline `TO` before the update + 1 and `VER >= 2`.
- [ ] No `01-*.sql` was created.
- [ ] The first line uses the correct header form from 3.1 or 3.2: single line, English, no period, no trailing whitespace.
- [ ] Changes landing on v1 (baseline) content, or on a brand-new object that depends on nothing from v2+, use Form A (no `COMPAT`).
- [ ] Changes landing on content of the vN upgrade script (`2 <= N < VER`) use Form B with `COMPAT = N`.
- [ ] When multiple versions are involved, the largest `N` is used.
- [ ] The full schema file is synced: every table, column, index, view, trigger, and seed row is present and the file still creates the whole database from empty.
- [ ] The header's `TO` is updated and `COMPAT` is the maximum among the scripts' `COMPAT` values and never `v0+`.
- [ ] The baseline header reads `-- v0 -> v{TO} ...`.
- [ ] The script contains only this version's changes (a rebuild is the only exception), and no `IF NOT EXISTS` / defensive clauses.
- [ ] SQL is valid SQLite: no MySQL dialect, no CGO-dependent or extension-only features.
- [ ] Money columns are `TEXT`; JSON columns default to `'{}'`.
- [ ] Data migrations touch only intended rows and preserve historical data.
- [ ] No script was renamed or renumbered; any deletion is limited to a script a later one makes unnecessary (same final schema either way), and the gap stays.
- [ ] The change stays inside the upgrade script and the baseline: no other file is touched.

If any item cannot be verified, say so explicitly; never claim it passed.

## 8. Forbidden

- Creating `01-*.sql`, renaming or renumbering a script, or deleting one that a later script does not make unnecessary.
- Updating only the incremental script without the baseline, or only the baseline without an incremental script.
- Omitting `COMPAT` on a v2+ change, or writing `COMPAT` on a v1 change.
- Writing `v0+` in any header, or using a `FROM` other than `0` in the baseline header.
- Writing version bookkeeping (schema version numbers, applied flags) into a script; a script carries schema changes only.
- MySQL dialect, non-SQLite syntax, or `REAL`/`FLOAT` for money.
