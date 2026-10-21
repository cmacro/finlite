-- v5: Add ledger profile and raw file evidence tables

-- 账套身份（单行）：MVP 固定一个账套，code 必须等于所在账套目录名。
-- 账套归属由目录结构表达，不在业务表上加 ledger_id，避免两套真相。
CREATE TABLE IF NOT EXISTS ledger_profile (
    id           INTEGER NOT NULL PRIMARY KEY CHECK (id = 1),
    code         TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    created_at   TEXT NOT NULL
);

-- 原始文件清单（证据索引）：文件本体在 <账套目录>/raw/archive/YYYY/MM/，
-- 本表只存内容指纹与相对路径；rel_path 相对账套目录，迁移不失效。
CREATE TABLE IF NOT EXISTS raw_files (
    sha256      TEXT NOT NULL PRIMARY KEY,
    rel_path    TEXT NOT NULL,
    orig_name   TEXT NOT NULL,
    source_type TEXT NOT NULL,
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    row_count   INTEGER NOT NULL DEFAULT 0,
    ingested_at TEXT NOT NULL,
    operator    TEXT NOT NULL DEFAULT 'local',
    note        TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_raw_files_type ON raw_files (source_type, ingested_at);

-- 批次 → 原始文件指纹；空串表示历史批次缺原始文件（不假装补齐）。
ALTER TABLE import_batches ADD COLUMN raw_sha256 TEXT NOT NULL DEFAULT '';
