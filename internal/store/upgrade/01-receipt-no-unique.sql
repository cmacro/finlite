-- v1 -> v2 (compatible with v0+): Add unique index on data_summaries.receipt_no for receipt-number uniqueness (F5)

-- 收据号唯一（业务文档 §7 F5）：空串/未编号行允许重复，只有填了号的行才参与唯一约束。
-- 部分唯一索引：SQLite 支持 WHERE 子句的部分索引，空值天然不冲突。
CREATE UNIQUE INDEX IF NOT EXISTS uq_data_summaries_receipt_no
ON data_summaries (receipt_no)
WHERE receipt_no IS NOT NULL AND receipt_no <> '';
