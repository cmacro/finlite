-- finlite SQLite 基线 schema v1（单账套 finlite.db）。
-- 业务依据：docs/business/00-overview-health-finance-flow.md §8。
-- 约定：金额 TEXT（decimal 字符串），日期 YYYY-MM-DD，时间戳 RFC3339。
-- 移植参考：ForgeFin src-tauri/src/db/schema.rs（多账套/分INTEGER已简化为单库/TEXT）。

CREATE TABLE IF NOT EXISTS import_batches (
    id               TEXT NOT NULL PRIMARY KEY,
    source_type      TEXT NOT NULL,
    file_name        TEXT NOT NULL,
    row_count        INTEGER NOT NULL DEFAULT 0,
    success_count    INTEGER NOT NULL DEFAULT 0,
    skip_count       INTEGER NOT NULL DEFAULT 0,
    balance_warning  TEXT,
    imported_at      TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS import_errors (
    id          TEXT NOT NULL PRIMARY KEY,
    batch_id    TEXT NOT NULL,
    row_no      INTEGER,
    field_name  TEXT,
    message     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS bank_flows (
    id                   TEXT NOT NULL PRIMARY KEY,
    batch_id             TEXT NOT NULL,
    file_name            TEXT NOT NULL,
    row_no               INTEGER NOT NULL,
    txn_time             TEXT NOT NULL,
    counterparty         TEXT,
    summary              TEXT,
    remark               TEXT,
    in_amount            TEXT NOT NULL DEFAULT '0',
    out_amount           TEXT NOT NULL DEFAULT '0',
    balance              TEXT NOT NULL DEFAULT '0',
    balance_status       TEXT NOT NULL DEFAULT 'skip',
    balance_confirmed_at TEXT,
    voucher_id           TEXT,
    created_at           TEXT NOT NULL,
    raw_json             TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_bank_flows_txn_time ON bank_flows (txn_time);
CREATE INDEX IF NOT EXISTS idx_bank_flows_batch ON bank_flows (batch_id);

CREATE TABLE IF NOT EXISTS order_flows (
    id                TEXT NOT NULL PRIMARY KEY,
    batch_id          TEXT NOT NULL,
    icbc_order_no     TEXT NOT NULL,
    third_order_no    TEXT,
    txn_time          TEXT NOT NULL,
    settle_date       TEXT NOT NULL,
    order_amount      TEXT NOT NULL DEFAULT '0',
    fee_amount        TEXT NOT NULL DEFAULT '0',
    net_amount        TEXT NOT NULL DEFAULT '0',
    pay_method        TEXT,
    debit_credit_flag TEXT,
    voucher_id        TEXT,
    raw_json          TEXT NOT NULL DEFAULT '{}',
    created_at        TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_order_flows_icbc_order_no ON order_flows (icbc_order_no);
CREATE INDEX IF NOT EXISTS idx_order_flows_settle_date ON order_flows (settle_date);
CREATE INDEX IF NOT EXISTS idx_order_flows_batch ON order_flows (batch_id);

CREATE TABLE IF NOT EXISTS data_summaries (
    id                  TEXT NOT NULL PRIMARY KEY,
    date                TEXT NOT NULL,
    receipt_no          TEXT,
    category            TEXT NOT NULL,
    project             TEXT NOT NULL,
    reason              TEXT,
    income_amount       TEXT NOT NULL DEFAULT '0',
    expense_amount      TEXT NOT NULL DEFAULT '0',
    fee_auto            TEXT NOT NULL DEFAULT '0',
    fee_actual          TEXT NOT NULL DEFAULT '0',
    fee_manual_adjusted INTEGER NOT NULL DEFAULT 0,
    balance             TEXT NOT NULL DEFAULT '0',
    remark              TEXT,
    source              TEXT NOT NULL DEFAULT 'manual',
    status              TEXT NOT NULL DEFAULT 'draft',
    voucher_id          TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_data_summaries_date ON data_summaries (date);
CREATE INDEX IF NOT EXISTS idx_data_summaries_status ON data_summaries (status);
-- 收据号唯一（业务文档 §7 F5）：仅对已填号的行生效，空串/未编号允许重复。
CREATE UNIQUE INDEX IF NOT EXISTS uq_data_summaries_receipt_no
ON data_summaries (receipt_no)
WHERE receipt_no IS NOT NULL AND receipt_no <> '';

CREATE TABLE IF NOT EXISTS vouchers (
    id                TEXT NOT NULL PRIMARY KEY,
    voucher_date      TEXT NOT NULL,
    voucher_no        TEXT NOT NULL,
    summary           TEXT NOT NULL DEFAULT '',
    status            TEXT NOT NULL DEFAULT 'draft',
    source_summary_id TEXT,
    created_at        TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_vouchers_voucher_no ON vouchers (voucher_no);
CREATE INDEX IF NOT EXISTS idx_vouchers_date ON vouchers (voucher_date);

CREATE TABLE IF NOT EXISTS voucher_entries (
    id           TEXT NOT NULL PRIMARY KEY,
    voucher_id   TEXT NOT NULL,
    account_code TEXT NOT NULL,
    account_name TEXT NOT NULL,
    debit        TEXT NOT NULL DEFAULT '0',
    credit       TEXT NOT NULL DEFAULT '0',
    remark       TEXT,
    sort_no      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_voucher_entries_voucher ON voucher_entries (voucher_id);

CREATE TABLE IF NOT EXISTS accounts (
    code         TEXT NOT NULL PRIMARY KEY,
    name         TEXT NOT NULL,
    parent_code  TEXT,
    account_type TEXT NOT NULL,
    is_active    INTEGER NOT NULL DEFAULT 1
);

CREATE TABLE IF NOT EXISTS fee_rates (
    pay_method TEXT NOT NULL PRIMARY KEY,
    rate       TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id          TEXT NOT NULL PRIMARY KEY,
    entity_type TEXT NOT NULL,
    entity_id   TEXT NOT NULL,
    action      TEXT NOT NULL,
    old_values  TEXT,
    new_values  TEXT,
    operator    TEXT,
    operated_at TEXT NOT NULL,
    remark      TEXT
);
CREATE INDEX IF NOT EXISTS idx_audit_logs_entity ON audit_logs (entity_type, entity_id);

-- 本地界面偏好（导入中心默认原始数据目录等）；键值对，单账套单机使用。
CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT NOT NULL PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- 店面收款登记（F9）：店面线下收款台账，状态 draft → confirmed → summarized / voided。
CREATE TABLE IF NOT EXISTS collections (
    id           TEXT NOT NULL PRIMARY KEY,
    collect_date TEXT NOT NULL,
    store        TEXT NOT NULL,
    customer     TEXT NOT NULL,
    phone        TEXT NOT NULL DEFAULT '',
    project      TEXT NOT NULL,
    pay_method   TEXT NOT NULL,
    account      TEXT NOT NULL,
    amount       TEXT NOT NULL DEFAULT '0',
    receipt_no   TEXT NOT NULL DEFAULT '',
    operator     TEXT NOT NULL,
    invoiced     INTEGER NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'draft',
    void_reason  TEXT NOT NULL DEFAULT '',
    summary_id   TEXT,
    note         TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_collections_date ON collections (collect_date);
CREATE INDEX IF NOT EXISTS idx_collections_status ON collections (status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_collections_receipt_no
ON collections (receipt_no)
WHERE receipt_no <> '';

-- 收款单据附件：内容存 BLOB，保证 finlite.db 单文件自洽。
CREATE TABLE IF NOT EXISTS collection_attachments (
    id            TEXT NOT NULL PRIMARY KEY,
    collection_id TEXT NOT NULL,
    file_name     TEXT NOT NULL,
    ext           TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    content       BLOB,
    sort_no       INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_collection_attachments_collection
ON collection_attachments (collection_id);
