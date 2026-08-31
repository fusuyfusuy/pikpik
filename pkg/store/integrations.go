package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type sqlIntegrationStore struct {
	db dbExecutor
}

func (s *sqlIntegrationStore) Create(ctx context.Context, it *Integration) error {
	if it.ID == "" {
		return errors.New("store: integration id cannot be empty")
	}
	if it.OrgID == "" {
		it.OrgID = "org_default"
	}
	if it.Name == "" {
		return errors.New("store: integration name cannot be empty")
	}
	if it.Type == "" {
		return errors.New("store: integration type cannot be empty")
	}
	if it.Status == "" {
		it.Status = "active"
	}
	if it.ConfigJSON == "" {
		it.ConfigJSON = "{}"
	}
	now := time.Now().UTC()
	if it.CreatedAt.IsZero() {
		it.CreatedAt = now
	}
	it.UpdatedAt = now

	query := `
		INSERT INTO integrations (id, org_id, name, type, credentials_encrypted, config_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		it.ID, it.OrgID, it.Name, it.Type, it.CredentialsEncrypted,
		it.ConfigJSON, it.Status, it.CreatedAt, it.UpdatedAt,
	)
	if err != nil {
		if IsDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return err
	}
	return nil
}

func (s *sqlIntegrationStore) GetByID(ctx context.Context, id string) (*Integration, error) {
	query := `
		SELECT id, org_id, name, type, credentials_encrypted, config_json, status, created_at, updated_at
		FROM integrations
		WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)
	return scanIntegration(row)
}

func (s *sqlIntegrationStore) ListByOrg(ctx context.Context, orgID string) ([]*Integration, error) {
	if orgID == "" {
		orgID = "org_default"
	}
	query := `
		SELECT id, org_id, name, type, credentials_encrypted, config_json, status, created_at, updated_at
		FROM integrations
		WHERE org_id = ?
		ORDER BY created_at DESC
	`
	rows, err := s.db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*Integration, 0)
	for rows.Next() {
		it, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, it)
	}
	return result, rows.Err()
}

func (s *sqlIntegrationStore) ListByType(ctx context.Context, orgID, intType string) ([]*Integration, error) {
	if orgID == "" {
		orgID = "org_default"
	}
	query := `
		SELECT id, org_id, name, type, credentials_encrypted, config_json, status, created_at, updated_at
		FROM integrations
		WHERE org_id = ? AND type = ?
		ORDER BY created_at DESC
	`
	rows, err := s.db.QueryContext(ctx, query, orgID, intType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*Integration, 0)
	for rows.Next() {
		it, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, it)
	}
	return result, rows.Err()
}

func (s *sqlIntegrationStore) Update(ctx context.Context, it *Integration) error {
	it.UpdatedAt = time.Now().UTC()
	query := `
		UPDATE integrations
		SET name = ?, credentials_encrypted = ?, config_json = ?, status = ?, updated_at = ?
		WHERE id = ?
	`
	res, err := s.db.ExecContext(ctx, query,
		it.Name, it.CredentialsEncrypted, it.ConfigJSON, it.Status, it.UpdatedAt, it.ID,
	)
	if err != nil {
		if IsDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlIntegrationStore) UpdateStatus(ctx context.Context, id string, status string) error {
	query := `UPDATE integrations SET status = ?, updated_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, status, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlIntegrationStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM integrations WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanIntegration(s rowScanner) (*Integration, error) {
	var it Integration
	err := s.Scan(
		&it.ID, &it.OrgID, &it.Name, &it.Type, &it.CredentialsEncrypted,
		&it.ConfigJSON, &it.Status, &it.CreatedAt, &it.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &it, nil
}
