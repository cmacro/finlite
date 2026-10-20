-- v3 -> v4 (compatible with v0+): Add app_settings key/value table for local UI preferences (import center default raw directory)

-- 本地界面偏好（单账套、单机）：键值对存储，当前仅导入中心「默认路径导入」使用。
-- raw_dir 不存在时由 Service 回退到「库文件同目录 /raw」。
CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT NOT NULL PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
