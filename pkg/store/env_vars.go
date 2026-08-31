package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlEnvVarStore struct {
	db dbExecutor
}

func (s *sqlEnvVarStore) Set(ctx context.Context, v *EnvVar) error {
	if v.ID == "" {
		v.ID = NewID("env")
	}
	now := time.Now().UTC()
	if v.CreatedAt.IsZero() {
		v.CreatedAt = now
	}
	if v.UpdatedAt.IsZero() {
		v.UpdatedAt = now
	}

	isSecret := 0
	if v.IsSecret {
		isSecret = 1
	}

	query := `
	INSERT INTO env_vars (
		id, scope_tier, resource_id, key, value_encrypted, is_secret, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(scope_tier, resource_id, key) DO UPDATE SET
		value_encrypted = excluded.value_encrypted,
		is_secret = excluded.is_secret,
		updated_at = excluded.updated_at`

	_, err := s.db.ExecContext(ctx, query,
		v.ID, string(v.ScopeTier), v.ResourceID, v.Key,
		v.ValueEncrypted, isSecret, v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to set env var: %w", err)
	}
	return nil
}

func (s *sqlEnvVarStore) Get(ctx context.Context, tier ScopeTier, resourceID, key string) (*EnvVar, error) {
	query := `
	SELECT id, scope_tier, resource_id, key, value_encrypted, is_secret, created_at, updated_at
	FROM env_vars WHERE scope_tier = ? AND resource_id = ? AND key = ?`

	row := s.db.QueryRowContext(ctx, query, string(tier), resourceID, key)

	var v EnvVar
	var scopeTier string
	var isSecret int

	err := row.Scan(&v.ID, &scopeTier, &v.ResourceID, &v.Key, &v.ValueEncrypted, &isSecret, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get env var: %w", err)
	}

	v.ScopeTier = ScopeTier(scopeTier)
	v.IsSecret = isSecret == 1
	return &v, nil
}

func (s *sqlEnvVarStore) ListByResource(ctx context.Context, tier ScopeTier, resourceID string) ([]*EnvVar, error) {
	query := `
	SELECT id, scope_tier, resource_id, key, value_encrypted, is_secret, created_at, updated_at
	FROM env_vars WHERE scope_tier = ? AND resource_id = ? ORDER BY key ASC`

	rows, err := s.db.QueryContext(ctx, query, string(tier), resourceID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list env vars: %w", err)
	}
	defer rows.Close()

	var list []*EnvVar
	for rows.Next() {
		var v EnvVar
		var scopeTier string
		var isSecret int

		err := rows.Scan(&v.ID, &scopeTier, &v.ResourceID, &v.Key, &v.ValueEncrypted, &isSecret, &v.CreatedAt, &v.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan env var row: %w", err)
		}

		v.ScopeTier = ScopeTier(scopeTier)
		v.IsSecret = isSecret == 1
		list = append(list, &v)
	}
	return list, rows.Err()
}

func (s *sqlEnvVarStore) Delete(ctx context.Context, tier ScopeTier, resourceID, key string) error {
	query := `DELETE FROM env_vars WHERE scope_tier = ? AND resource_id = ? AND key = ?`
	res, err := s.db.ExecContext(ctx, query, string(tier), resourceID, key)
	if err != nil {
		return fmt.Errorf("store: failed to delete env var: %w", err)
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

func (s *sqlEnvVarStore) DeleteByResource(ctx context.Context, tier ScopeTier, resourceID string) error {
	query := `DELETE FROM env_vars WHERE scope_tier = ? AND resource_id = ?`
	_, err := s.db.ExecContext(ctx, query, string(tier), resourceID)
	if err != nil {
		return fmt.Errorf("store: failed to delete env vars by resource: %w", err)
	}
	return nil
}

