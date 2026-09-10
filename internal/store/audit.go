package store

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// WriteAudit 写入 audit_logs。调用方传入 old/new JSON 字符串（可为空）。
// 汇总修改/审核/凭证动作必须记账（业务文档 §10）。
func WriteAudit(tx *sql.Tx, entityType, entityID, action, oldValues, newValues, operator, remark string) error {
	_, err := tx.Exec(
		`INSERT INTO audit_logs (id, entity_type, entity_id, action, old_values, new_values, operator, operated_at, remark)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), entityType, entityID, action,
		nullIfEmpty(oldValues), nullIfEmpty(newValues),
		nullIfEmpty(operator), time.Now().Format(time.RFC3339), nullIfEmpty(remark),
	)
	return err
}
