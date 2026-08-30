package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlStageStore struct {
	db dbExecutor
}

func (s *sqlStageStore) Create(ctx context.Context, stage *Stage) error {
	if stage.ID == "" {
		stage.ID = NewID("stg")
	}
	now := time.Now().UTC()
	if stage.CreatedAt.IsZero() {
		stage.CreatedAt = now
	}
	if stage.UpdatedAt.IsZero() {
		stage.UpdatedAt = now
	}

	query := `INSERT INTO stages (id, project_id, name, slug, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, stage.ID, stage.ProjectID, stage.Name, stage.Slug, stage.CreatedAt, stage.UpdatedAt)
	if err != nil {
		return fmt.Errorf("store: failed to create stage: %w", err)
	}
	return nil
}

func (s *sqlStageStore) GetByID(ctx context.Context, id string) (*Stage, error) {
	query := `SELECT id, project_id, name, slug, created_at, updated_at FROM stages WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var stage Stage
	err := row.Scan(&stage.ID, &stage.ProjectID, &stage.Name, &stage.Slug, &stage.CreatedAt, &stage.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get stage by id: %w", err)
	}
	return &stage, nil
}

func (s *sqlStageStore) GetBySlug(ctx context.Context, projectID, slug string) (*Stage, error) {
	query := `SELECT id, project_id, name, slug, created_at, updated_at FROM stages WHERE project_id = ? AND slug = ?`
	row := s.db.QueryRowContext(ctx, query, projectID, slug)

	var stage Stage
	err := row.Scan(&stage.ID, &stage.ProjectID, &stage.Name, &stage.Slug, &stage.CreatedAt, &stage.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get stage by slug: %w", err)
	}
	return &stage, nil
}

func (s *sqlStageStore) ListByProject(ctx context.Context, projectID string) ([]*Stage, error) {
	query := `SELECT id, project_id, name, slug, created_at, updated_at FROM stages WHERE project_id = ? ORDER BY name ASC`
	rows, err := s.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list stages: %w", err)
	}
	defer rows.Close()

	var stages []*Stage
	for rows.Next() {
		var stage Stage
		if err := rows.Scan(&stage.ID, &stage.ProjectID, &stage.Name, &stage.Slug, &stage.CreatedAt, &stage.UpdatedAt); err != nil {
			return nil, fmt.Errorf("store: failed to scan stage: %w", err)
		}
		stages = append(stages, &stage)
	}
	return stages, rows.Err()
}

func (s *sqlStageStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM stages WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete stage: %w", err)
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
