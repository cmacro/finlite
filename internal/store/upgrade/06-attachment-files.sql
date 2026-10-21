-- v6: Store collection attachment content as files outside the database

-- 附件内容不再入库：改为 <账套目录>/attachments/YYYY/MM/<sha256>.<ext>，
-- 库内只留内容指纹与相对路径（rel_path 相对账套目录，随账套整体搬迁）。
-- 旧库的 BLOB 在开库前置步骤（store.PreMigrateAttachments）已写盘并回填 sha256/rel_path，
-- 本脚本只做表重建，把 content 列去掉。
CREATE TABLE collection_attachments_v6 (
    id            TEXT NOT NULL PRIMARY KEY,
    collection_id TEXT NOT NULL,
    file_name     TEXT NOT NULL,
    ext           TEXT NOT NULL,
    size_bytes    INTEGER NOT NULL DEFAULT 0,
    sha256        TEXT NOT NULL DEFAULT '',
    rel_path      TEXT NOT NULL DEFAULT '',
    sort_no       INTEGER NOT NULL DEFAULT 0,
    created_at    TEXT NOT NULL
);
INSERT INTO collection_attachments_v6
    (id, collection_id, file_name, ext, size_bytes, sha256, rel_path, sort_no, created_at)
SELECT id, collection_id, file_name, ext, size_bytes, sha256, rel_path, sort_no, created_at
FROM collection_attachments;
DROP TABLE collection_attachments;
ALTER TABLE collection_attachments_v6 RENAME TO collection_attachments;
CREATE INDEX idx_collection_attachments_collection ON collection_attachments (collection_id);
CREATE INDEX idx_collection_attachments_sha256 ON collection_attachments (sha256);
