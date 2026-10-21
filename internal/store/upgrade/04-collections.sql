-- v4: Add store-collection register and attachment tables (F9)

-- 店面收款登记（F9）：店面线下收款台账，状态机 draft → confirmed → summarized，
-- 以及作废 voided；金额 TEXT（decimal 字符串），日期 YYYY-MM-DD，时间戳 RFC3339。
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
-- 收据号唯一（同 data_summaries 规则）：空串不占号，填号后不得重复。
CREATE UNIQUE INDEX IF NOT EXISTS uq_collections_receipt_no
ON collections (receipt_no)
WHERE receipt_no <> '';

-- 收款单据附件：内容直接存 BLOB，保证 finlite.db 单文件自洽（备份即完整）。
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
