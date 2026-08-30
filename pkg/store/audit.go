package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type sqlAuditStore struct {
	db dbExecutor
}

func (s *sqlAuditStore) Record(ctx context.Context, userID, action, resType, resID, metadataJSON, ip string) error {
	id := NewID("aud")
	now := time.Now().UTC()
	if metadataJSON == "" {
		metadataJSON = "{}"
	}

	query := `
	INSERT INTO audit_logs (id, user_id, action, resource_type, resource_id, metadata, ip_address, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query, id, userID, action, resType, resID, metadataJSON, ip, now)
	if err != nil {
		return fmt.Errorf("store: failed to record audit log: %w", err)
	}
	return nil
}

func (s *sqlAuditStore) ListByResource(ctx context.Context, resType, resID string, limit int) ([]*AuditLog, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
	SELECT id, user_id, action, resource_type, resource_id, metadata, ip_address, created_at
	FROM audit_logs WHERE resource_type = ? AND resource_id = ? ORDER BY created_at DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, resType, resID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list audit logs: %w", err)
	}
	defer rows.Close()

	var logs []*AuditLog
	for rows.Next() {
		var l AuditLog
		var userID sql.NullString
		err := rows.Scan(
			&l.ID, &userID, &l.Action, &l.ResourceType,
			&l.ResourceID, &l.Metadata, &l.IPAddress, &l.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan audit log row: %w", err)
		}
		l.UserID = userID.String
		logs = append(logs, &l)
	}
	return logs, rows.Err()
}
