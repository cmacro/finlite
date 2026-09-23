-- v2 -> v3 (compatible with v0+): Add bank_flows.raw_json to keep the original row (appendix A: 对方账号 etc. preserved for audit)

-- 原始行备查（业务文档 §10 可追溯 / 附录 A「对方账号→存 raw」）。
-- 与 order_flows.raw_json 对齐：整行原样存 JSON，便于事后核对与定位原文件+行号。
ALTER TABLE bank_flows ADD COLUMN raw_json TEXT NOT NULL DEFAULT '{}';
