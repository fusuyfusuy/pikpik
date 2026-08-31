package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlOrgStore struct {
	db dbExecutor
}

func (s *sqlOrgStore) Create(ctx context.Context, org *Organization) error {
	if org.ID == "" {
		org.ID = NewID("org")
	}
	now := time.Now().UTC()
	if org.CreatedAt.IsZero() {
		org.CreatedAt = now
	}
	if org.UpdatedAt.IsZero() {
		org.UpdatedAt = now
	}

	query := `INSERT INTO organizations (id, name, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, org.ID, org.Name, org.Slug, org.CreatedAt, org.UpdatedAt)
	if err != nil {
		if IsDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return fmt.Errorf("store: failed to create org: %w", err)
	}
	return nil
}

func (s *sqlOrgStore) GetByID(ctx context.Context, id string) (*Organization, error) {
	query := `SELECT id, name, slug, created_at, updated_at FROM organizations WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var org Organization
	err := row.Scan(&org.ID, &org.Name, &org.Slug, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get org by id: %w", err)
	}
	return &org, nil
}

func (s *sqlOrgStore) GetBySlug(ctx context.Context, slug string) (*Organization, error) {
	query := `SELECT id, name, slug, created_at, updated_at FROM organizations WHERE slug = ?`
	row := s.db.QueryRowContext(ctx, query, slug)

	var org Organization
	err := row.Scan(&org.ID, &org.Name, &org.Slug, &org.CreatedAt, &org.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get org by slug: %w", err)
	}
	return &org, nil
}

func (s *sqlOrgStore) List(ctx context.Context) ([]*Organization, error) {
	query := `SELECT id, name, slug, created_at, updated_at FROM organizations ORDER BY name ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list orgs: %w", err)
	}
	defer rows.Close()

	var list []*Organization
	for rows.Next() {
		var org Organization
		if err := rows.Scan(&org.ID, &org.Name, &org.Slug, &org.CreatedAt, &org.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: failed to scan org: %w", err)
		}
		list = append(list, &org)
	}
	return list, rows.Err()
}

func (s *sqlOrgStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM organizations WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete org: %w", err)
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
