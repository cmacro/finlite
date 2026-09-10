package services

import (
	"context"
	"fmt"

	"finlite/internal/store"
)

// AuditService 审计日志查询。
type AuditService struct {
	DB *store.DB
}

func NewAuditService(db *store.DB) *AuditService { return &AuditService{DB: db} }

// ListAuditLogs 按实体查审计日志（倒序）。
func (s *AuditService) ListAuditLogs(ctx context.Context, entityType, entityID string) ([]AuditLog, error) {
	rows, err := s.DB.Pool().QueryContext(ctx,
		`SELECT id, entity_type, entity_id, action,
			COALESCE(old_values,''), COALESCE(new_values,''), COALESCE(operator,''),
			operated_at, COALESCE(remark,'')
		 FROM audit_logs WHERE entity_type=? AND entity_id=? ORDER BY operated_at DESC LIMIT 200`,
		entityType, entityID)
	if err != nil {
		return nil, fmt.Errorf("查询审计日志失败：%v", err)
	}
	defer rows.Close()
	var out []AuditLog
	for rows.Next() {
		var a AuditLog
		if err := rows.Scan(&a.ID, &a.EntityType, &a.EntityID, &a.Action,
			&a.OldValues, &a.NewValues, &a.Operator, &a.OperatedAt, &a.Remark); err != nil {
			return nil, fmt.Errorf("读取审计日志失败：%v", err)
		}
		out = append(out, a)
	}
	if out == nil {
		out = []AuditLog{}
	}
	return out, rows.Err()
}
