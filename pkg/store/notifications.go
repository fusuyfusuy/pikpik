package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type sqlNotificationStore struct {
	db dbExecutor
}

func (s *sqlNotificationStore) Create(ctx context.Context, ch *NotificationChannel) error {
	if ch.ID == "" {
		ch.ID = NewID("ntf")
	}
	now := time.Now().UTC()
	if ch.CreatedAt.IsZero() {
		ch.CreatedAt = now
	}
	if ch.UpdatedAt.IsZero() {
		ch.UpdatedAt = now
	}
	if ch.Type == "" {
		ch.Type = "webhook"
	}
	if len(ch.Events) == 0 {
		ch.Events = []string{"deploy:failure", "deploy:success", "backup:failure", "backup:success"}
	}

	eventsJSON, err := json.Marshal(ch.Events)
	if err != nil {
		return fmt.Errorf("store: failed to marshal notification events: %w", err)
	}

	var projectID sql.NullString
	if ch.ProjectID != "" {
		projectID = sql.NullString{String: ch.ProjectID, Valid: true}
	}
	var authToken sql.NullString
	if ch.AuthToken != "" {
		authToken = sql.NullString{String: ch.AuthToken, Valid: true}
	}

	enabledInt := 0
	if ch.Enabled {
		enabledInt = 1
	}

	query := `
	INSERT INTO notification_channels (
		id, org_id, project_id, name, channel_type,
		target_url, auth_token, events, enabled,
		created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = s.db.ExecContext(ctx, query,
		ch.ID, ch.OrgID, projectID, ch.Name, ch.Type,
		ch.TargetURL, authToken, string(eventsJSON), enabledInt,
		ch.CreatedAt, ch.UpdatedAt,
	)
	if err != nil {
		if IsDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return fmt.Errorf("store: failed to create notification channel: %w", err)
	}
	return nil
}

func (s *sqlNotificationStore) GetByID(ctx context.Context, id string) (*NotificationChannel, error) {
	query := `
	SELECT id, org_id, project_id, name, channel_type,
	       target_url, auth_token, events, enabled,
	       created_at, updated_at
	FROM notification_channels
	WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanChannel(row)
}

func (s *sqlNotificationStore) ListByOrg(ctx context.Context, orgID string) ([]*NotificationChannel, error) {
	query := `
	SELECT id, org_id, project_id, name, channel_type,
	       target_url, auth_token, events, enabled,
	       created_at, updated_at
	FROM notification_channels
	WHERE org_id = ?
	ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list notification channels: %w", err)
	}
	defer rows.Close()

	return s.scanChannels(rows)
}

func (s *sqlNotificationStore) ListByProject(ctx context.Context, projectID string) ([]*NotificationChannel, error) {
	query := `
	SELECT id, org_id, project_id, name, channel_type,
	       target_url, auth_token, events, enabled,
	       created_at, updated_at
	FROM notification_channels
	WHERE project_id = ?
	ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list project notification channels: %w", err)
	}
	defer rows.Close()

	return s.scanChannels(rows)
}

func (s *sqlNotificationStore) ListForEvent(ctx context.Context, orgID, projectID, event string) ([]*NotificationChannel, error) {
	query := `
	SELECT id, org_id, project_id, name, channel_type,
	       target_url, auth_token, events, enabled,
	       created_at, updated_at
	FROM notification_channels
	WHERE enabled = 1 AND (org_id = ? OR project_id = ?)
	ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list notification channels for event: %w", err)
	}
	defer rows.Close()

	channels, err := s.scanChannels(rows)
	if err != nil {
		return nil, err
	}

	matched := make([]*NotificationChannel, 0)
	for _, ch := range channels {
		for _, e := range ch.Events {
			if e == "*" || e == event || strings.HasPrefix(event, strings.TrimSuffix(e, "*")) {
				matched = append(matched, ch)
				break
			}
		}
	}
	return matched, nil
}

func (s *sqlNotificationStore) Update(ctx context.Context, ch *NotificationChannel) error {
	ch.UpdatedAt = time.Now().UTC()
	eventsJSON, err := json.Marshal(ch.Events)
	if err != nil {
		return fmt.Errorf("store: failed to marshal notification events: %w", err)
	}

	var projectID sql.NullString
	if ch.ProjectID != "" {
		projectID = sql.NullString{String: ch.ProjectID, Valid: true}
	}
	var authToken sql.NullString
	if ch.AuthToken != "" {
		authToken = sql.NullString{String: ch.AuthToken, Valid: true}
	}

	enabledInt := 0
	if ch.Enabled {
		enabledInt = 1
	}

	query := `
	UPDATE notification_channels
	SET name = ?, channel_type = ?, target_url = ?, auth_token = ?,
	    events = ?, enabled = ?, project_id = ?, updated_at = ?
	WHERE id = ?`

	res, err := s.db.ExecContext(ctx, query,
		ch.Name, ch.Type, ch.TargetURL, authToken,
		string(eventsJSON), enabledInt, projectID, ch.UpdatedAt,
		ch.ID,
	)
	if err != nil {
		return fmt.Errorf("store: failed to update notification channel: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlNotificationStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM notification_channels WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete notification channel: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlNotificationStore) scanChannel(row interface{ Scan(dest ...any) error }) (*NotificationChannel, error) {
	var ch NotificationChannel
	var projectID, authToken sql.NullString
	var eventsJSON string
	var enabledInt int

	err := row.Scan(
		&ch.ID, &ch.OrgID, &projectID, &ch.Name, &ch.Type,
		&ch.TargetURL, &authToken, &eventsJSON, &enabledInt,
		&ch.CreatedAt, &ch.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to scan notification channel: %w", err)
	}

	if projectID.Valid {
		ch.ProjectID = projectID.String
	}
	if authToken.Valid {
		ch.AuthToken = authToken.String
	}
	ch.Enabled = enabledInt == 1

	if eventsJSON != "" {
		_ = json.Unmarshal([]byte(eventsJSON), &ch.Events)
	}
	if ch.Events == nil {
		ch.Events = []string{}
	}

	return &ch, nil
}

func (s *sqlNotificationStore) scanChannels(rows *sql.Rows) ([]*NotificationChannel, error) {
	list := make([]*NotificationChannel, 0)
	for rows.Next() {
		ch, err := s.scanChannel(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: row iteration error: %w", err)
	}
	return list, nil
}
