package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlScheduleStore struct {
	db dbExecutor
}

const scheduleSelectCols = `
	id, service_id, cron_expr, engine, database_name, username, password_encrypted,
	s3_bucket, s3_endpoint, s3_region, s3_access_key, s3_secret_key_encrypted,
	retention_hourly, retention_daily, retention_weekly, retention_monthly, max_backups,
	compression, is_enabled, last_run_at, next_run_at, created_at, updated_at
`

func (s *sqlScheduleStore) scanSchedule(row interface {
	Scan(dest ...any) error
}) (*BackupSchedule, error) {
	var sch BackupSchedule
	var isEnabled int
	var lastRunAt, nextRunAt sql.NullTime

	err := row.Scan(
		&sch.ID,
		&sch.ServiceID,
		&sch.CronExpr,
		&sch.Engine,
		&sch.DatabaseName,
		&sch.Username,
		&sch.PasswordEncrypted,
		&sch.S3Bucket,
		&sch.S3Endpoint,
		&sch.S3Region,
		&sch.S3AccessKey,
		&sch.S3SecretKeyEncrypted,
		&sch.RetentionHourly,
		&sch.RetentionDaily,
		&sch.RetentionWeekly,
		&sch.RetentionMonthly,
		&sch.MaxBackups,
		&sch.Compression,
		&isEnabled,
		&lastRunAt,
		&nextRunAt,
		&sch.CreatedAt,
		&sch.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to scan backup schedule: %w", err)
	}

	sch.IsEnabled = isEnabled == 1
	if lastRunAt.Valid {
		sch.LastRunAt = &lastRunAt.Time
	}
	if nextRunAt.Valid {
		sch.NextRunAt = &nextRunAt.Time
	}

	return &sch, nil
}

func (s *sqlScheduleStore) Create(ctx context.Context, sch *BackupSchedule) error {
	if sch.ID == "" {
		sch.ID = NewID("sch")
	}
	now := time.Now().UTC()
	if sch.CreatedAt.IsZero() {
		sch.CreatedAt = now
	}
	if sch.UpdatedAt.IsZero() {
		sch.UpdatedAt = now
	}
	if sch.Compression == "" {
		sch.Compression = "gzip"
	}
	if sch.MaxBackups <= 0 {
		sch.MaxBackups = 30
	}

	isEnabled := 0
	if sch.IsEnabled {
		isEnabled = 1
	}

	query := `
	INSERT INTO backup_schedules (
		id, service_id, cron_expr, engine, database_name, username, password_encrypted,
		s3_bucket, s3_endpoint, s3_region, s3_access_key, s3_secret_key_encrypted,
		retention_hourly, retention_daily, retention_weekly, retention_monthly, max_backups,
		compression, is_enabled, last_run_at, next_run_at, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		sch.ID,
		sch.ServiceID,
		sch.CronExpr,
		sch.Engine,
		sch.DatabaseName,
		sch.Username,
		sch.PasswordEncrypted,
		sch.S3Bucket,
		sch.S3Endpoint,
		sch.S3Region,
		sch.S3AccessKey,
		sch.S3SecretKeyEncrypted,
		sch.RetentionHourly,
		sch.RetentionDaily,
		sch.RetentionWeekly,
		sch.RetentionMonthly,
		sch.MaxBackups,
		sch.Compression,
		isEnabled,
		sch.LastRunAt,
		sch.NextRunAt,
		sch.CreatedAt,
		sch.UpdatedAt,
	)
	if err != nil {
		if IsDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return fmt.Errorf("store: failed to create backup schedule: %w", err)
	}
	return nil
}

func (s *sqlScheduleStore) GetByID(ctx context.Context, id string) (*BackupSchedule, error) {
	query := fmt.Sprintf("SELECT %s FROM backup_schedules WHERE id = ?", scheduleSelectCols)
	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanSchedule(row)
}

func (s *sqlScheduleStore) ListByService(ctx context.Context, serviceID string) ([]*BackupSchedule, error) {
	query := fmt.Sprintf("SELECT %s FROM backup_schedules WHERE service_id = ? ORDER BY created_at DESC", scheduleSelectCols)
	rows, err := s.db.QueryContext(ctx, query, serviceID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list backup schedules by service: %w", err)
	}
	defer rows.Close()

	var list []*BackupSchedule
	for rows.Next() {
		sch, err := s.scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sch)
	}
	return list, rows.Err()
}

func (s *sqlScheduleStore) ListActive(ctx context.Context) ([]*BackupSchedule, error) {
	query := fmt.Sprintf("SELECT %s FROM backup_schedules WHERE is_enabled = 1 ORDER BY created_at ASC", scheduleSelectCols)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list active backup schedules: %w", err)
	}
	defer rows.Close()

	var list []*BackupSchedule
	for rows.Next() {
		sch, err := s.scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sch)
	}
	return list, rows.Err()
}

func (s *sqlScheduleStore) ListDue(ctx context.Context, now time.Time) ([]*BackupSchedule, error) {
	query := fmt.Sprintf(`SELECT %s FROM backup_schedules WHERE is_enabled = 1 AND (next_run_at IS NULL OR next_run_at <= ?) ORDER BY next_run_at ASC`, scheduleSelectCols)
	rows, err := s.db.QueryContext(ctx, query, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("store: failed to list due backup schedules: %w", err)
	}
	defer rows.Close()

	var list []*BackupSchedule
	for rows.Next() {
		sch, err := s.scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, sch)
	}
	return list, rows.Err()
}

func (s *sqlScheduleStore) Update(ctx context.Context, sch *BackupSchedule) error {
	sch.UpdatedAt = time.Now().UTC()
	isEnabled := 0
	if sch.IsEnabled {
		isEnabled = 1
	}

	query := `
	UPDATE backup_schedules SET
		service_id = ?,
		cron_expr = ?,
		engine = ?,
		database_name = ?,
		username = ?,
		password_encrypted = ?,
		s3_bucket = ?,
		s3_endpoint = ?,
		s3_region = ?,
		s3_access_key = ?,
		s3_secret_key_encrypted = ?,
		retention_hourly = ?,
		retention_daily = ?,
		retention_weekly = ?,
		retention_monthly = ?,
		max_backups = ?,
		compression = ?,
		is_enabled = ?,
		last_run_at = ?,
		next_run_at = ?,
		updated_at = ?
	WHERE id = ?`

	res, err := s.db.ExecContext(ctx, query,
		sch.ServiceID,
		sch.CronExpr,
		sch.Engine,
		sch.DatabaseName,
		sch.Username,
		sch.PasswordEncrypted,
		sch.S3Bucket,
		sch.S3Endpoint,
		sch.S3Region,
		sch.S3AccessKey,
		sch.S3SecretKeyEncrypted,
		sch.RetentionHourly,
		sch.RetentionDaily,
		sch.RetentionWeekly,
		sch.RetentionMonthly,
		sch.MaxBackups,
		sch.Compression,
		isEnabled,
		sch.LastRunAt,
		sch.NextRunAt,
		sch.UpdatedAt,
		sch.ID,
	)
	if err != nil {
		return fmt.Errorf("store: failed to update backup schedule: %w", err)
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

func (s *sqlScheduleStore) UpdateRunTimes(ctx context.Context, id string, lastRun, nextRun time.Time) error {
	now := time.Now().UTC()
	query := `UPDATE backup_schedules SET last_run_at = ?, next_run_at = ?, updated_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, lastRun.UTC(), nextRun.UTC(), now, id)
	if err != nil {
		return fmt.Errorf("store: failed to update backup schedule run times: %w", err)
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

func (s *sqlScheduleStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM backup_schedules WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete backup schedule: %w", err)
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
