package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

type sqlProjectStore struct {
	db      dbExecutor
	writeMu *sync.Mutex
}

func (s *sqlProjectStore) Create(ctx context.Context, p *Project) error {
	if p.ID == "" {
		p.ID = NewID("prj")
	}
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}

	query := `
	INSERT INTO projects (id, org_id, name, slug, description, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query, p.ID, p.OrgID, p.Name, p.Slug, p.Description, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		return fmt.Errorf("store: failed to create project: %w", err)
	}
	return nil
}

func (s *sqlProjectStore) GetByID(ctx context.Context, id string) (*Project, error) {
	query := `SELECT id, org_id, name, slug, description, created_at, updated_at FROM projects WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var p Project
	var desc sql.NullString
	err := row.Scan(&p.ID, &p.OrgID, &p.Name, &p.Slug, &desc, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get project by id: %w", err)
	}
	p.Description = desc.String
	return &p, nil
}

func (s *sqlProjectStore) GetBySlug(ctx context.Context, slug string) (*Project, error) {
	query := `SELECT id, org_id, name, slug, description, created_at, updated_at FROM projects WHERE slug = ?`
	row := s.db.QueryRowContext(ctx, query, slug)

	var p Project
	var desc sql.NullString
	err := row.Scan(&p.ID, &p.OrgID, &p.Name, &p.Slug, &desc, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get project by slug: %w", err)
	}
	p.Description = desc.String
	return &p, nil
}

func (s *sqlProjectStore) List(ctx context.Context, orgID string) ([]*Project, error) {
	var query string
	var rows *sql.Rows
	var err error

	if orgID != "" {
		query = `SELECT id, org_id, name, slug, description, created_at, updated_at FROM projects WHERE org_id = ? ORDER BY name ASC`
		rows, err = s.db.QueryContext(ctx, query, orgID)
	} else {
		query = `SELECT id, org_id, name, slug, description, created_at, updated_at FROM projects ORDER BY name ASC`
		rows, err = s.db.QueryContext(ctx, query)
	}

	if err != nil {
		return nil, fmt.Errorf("store: failed to list projects: %w", err)
	}
	defer rows.Close()

	var projects []*Project
	for rows.Next() {
		var p Project
		var desc sql.NullString
		if err := rows.Scan(&p.ID, &p.OrgID, &p.Name, &p.Slug, &desc, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: failed to scan project: %w", err)
		}
		p.Description = desc.String
		projects = append(projects, &p)
	}
	return projects, rows.Err()
}

func (s *sqlProjectStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM projects WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete project: %w", err)
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
