package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type sqlMembershipStore struct {
	db dbExecutor
}

func (s *sqlMembershipStore) Set(ctx context.Context, m *ProjectMembership) error {
	if m.ID == "" {
		return errors.New("store: membership id cannot be empty")
	}
	if m.ProjectID == "" {
		return errors.New("store: project_id cannot be empty")
	}
	if m.UserID == "" {
		return errors.New("store: user_id cannot be empty")
	}
	if m.Role == "" {
		m.Role = "developer"
	}
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now

	query := `
		INSERT INTO project_memberships (id, project_id, user_id, role, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(project_id, user_id) DO UPDATE SET
			role = excluded.role,
			updated_at = excluded.updated_at
	`
	_, err := s.db.ExecContext(ctx, query,
		m.ID, m.ProjectID, m.UserID, m.Role, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		if IsDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return err
	}
	return nil
}

func (s *sqlMembershipStore) Get(ctx context.Context, projectID, userID string) (*ProjectMembership, error) {
	query := `
		SELECT id, project_id, user_id, role, created_at, updated_at
		FROM project_memberships
		WHERE project_id = ? AND user_id = ?
	`
	row := s.db.QueryRowContext(ctx, query, projectID, userID)
	return scanMembership(row)
}

func (s *sqlMembershipStore) ListByProject(ctx context.Context, projectID string) ([]*ProjectMembership, error) {
	query := `
		SELECT id, project_id, user_id, role, created_at, updated_at
		FROM project_memberships
		WHERE project_id = ?
		ORDER BY created_at ASC
	`
	rows, err := s.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*ProjectMembership, 0)
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *sqlMembershipStore) ListByUser(ctx context.Context, userID string) ([]*ProjectMembership, error) {
	query := `
		SELECT id, project_id, user_id, role, created_at, updated_at
		FROM project_memberships
		WHERE user_id = ?
		ORDER BY created_at ASC
	`
	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*ProjectMembership, 0)
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *sqlMembershipStore) Delete(ctx context.Context, projectID, userID string) error {
	query := `DELETE FROM project_memberships WHERE project_id = ? AND user_id = ?`
	res, err := s.db.ExecContext(ctx, query, projectID, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func scanMembership(s rowScanner) (*ProjectMembership, error) {
	var m ProjectMembership
	err := s.Scan(
		&m.ID, &m.ProjectID, &m.UserID, &m.Role, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}
