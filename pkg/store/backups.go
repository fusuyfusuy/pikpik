package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlBackupStore struct {
	db dbExecutor
}

func (s *sqlBackupStore) CreateConfig(ctx context.Context, c *BackupConfig) error {
	if c.ID == "" {
		c.ID = NewID("bkp")
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = now
	}
	if c.RetentionDays <= 0 {
		c.RetentionDays = 30
	}

	isEnabled := 0
	if c.IsEnabled {
		isEnabled = 1
	}

	query := `
	INSERT INTO backup_configs (
		id, service_id, s3_endpoint, s3_bucket, s3_region, s3_access_key,
		s3_secret_key_encrypted, cron_expr, retention_days, is_enabled, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		c.ID, c.ServiceID, c.S3Endpoint, c.S3Bucket, c.S3Region, c.S3AccessKey,
		c.S3SecretKeyEncrypted, c.CronExpr, c.RetentionDays, isEnabled, c.CreatedAt, c.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to create backup config: %w", err)
	}
	return nil
}

func (s *sqlBackupStore) GetConfigByService(ctx context.Context, serviceID string) (*BackupConfig, error) {
	query := `
	SELECT id, service_id, s3_endpoint, s3_bucket, s3_region, s3_access_key,
		s3_secret_key_encrypted, cron_expr, retention_days, is_enabled, created_at, updated_at
	FROM backup_configs WHERE service_id = ?`

	row := s.db.QueryRowContext(ctx, query, serviceID)

	var c BackupConfig
	var isEnabled int

	err := row.Scan(
		&c.ID, &c.ServiceID, &c.S3Endpoint, &c.S3Bucket, &c.S3Region, &c.S3AccessKey,
		&c.S3SecretKeyEncrypted, &c.CronExpr, &c.RetentionDays, &isEnabled, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get backup config: %w", err)
	}

	c.IsEnabled = isEnabled == 1
	return &c, nil
}

func (s *sqlBackupStore) UpdateConfig(ctx context.Context, c *BackupConfig) error {
	now := time.Now().UTC()
	c.UpdatedAt = now

	isEnabled := 0
	if c.IsEnabled {
		isEnabled = 1
	}

	query := `
	UPDATE backup_configs SET
		s3_endpoint = ?, s3_bucket = ?, s3_region = ?, s3_access_key = ?,
		s3_secret_key_encrypted = ?, cron_expr = ?, retention_days = ?,
		is_enabled = ?, updated_at = ?
	WHERE service_id = ?`

	res, err := s.db.ExecContext(ctx, query,
		c.S3Endpoint, c.S3Bucket, c.S3Region, c.S3AccessKey,
		c.S3SecretKeyEncrypted, c.CronExpr, c.RetentionDays,
		isEnabled, c.UpdatedAt, c.ServiceID,
	)
	if err != nil {
		return fmt.Errorf("store: failed to update backup config: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlBackupStore) RecordExecution(ctx context.Context, exec *BackupExecution) error {
	if exec.ID == "" {
		exec.ID = NewID("bke")
	}
	if exec.CreatedAt.IsZero() {
		exec.CreatedAt = time.Now().UTC()
	}

	query := `
	INSERT INTO backup_executions (
		id, config_id, service_id, s3_key, bytes_streamed, duration_ms, status, error_message, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		exec.ID, exec.ConfigID, exec.ServiceID, exec.S3Key, exec.BytesStreamed,
		exec.DurationMS, exec.Status, exec.ErrorMessage, exec.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to record backup execution: %w", err)
	}
	return nil
}

func (s *sqlBackupStore) ListExecutions(ctx context.Context, serviceID string, limit int) ([]*BackupExecution, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
	SELECT id, config_id, service_id, s3_key, bytes_streamed, duration_ms, status, error_message, created_at
	FROM backup_executions WHERE service_id = ? ORDER BY created_at DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, serviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list backup executions: %w", err)
	}
	defer rows.Close()

	var list []*BackupExecution
	for rows.Next() {
		var exec BackupExecution
		var errMsg sql.NullString
		err := rows.Scan(
			&exec.ID, &exec.ConfigID, &exec.ServiceID, &exec.S3Key,
			&exec.BytesStreamed, &exec.DurationMS, &exec.Status,
			&errMsg, &exec.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan backup execution row: %w", err)
		}
		exec.ErrorMessage = errMsg.String
		list = append(list, &exec)
	}
	return list, rows.Err()
}
