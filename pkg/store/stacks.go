package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlStackStore struct {
	db dbExecutor
}

func (s *sqlStackStore) Create(ctx context.Context, stack *Stack) error {
	if stack.ID == "" {
		stack.ID = NewID("stk")
	}
	now := time.Now().UTC()
	if stack.CreatedAt.IsZero() {
		stack.CreatedAt = now
	}
	if stack.UpdatedAt.IsZero() {
		stack.UpdatedAt = now
	}
	if stack.Status == "" {
		stack.Status = "stopped"
	}

	query := `
	INSERT INTO stacks (id, project_id, name, compose_yaml, status, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		stack.ID, stack.ProjectID, stack.Name, stack.ComposeYAML, stack.Status, stack.CreatedAt, stack.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to create stack: %w", err)
	}
	return nil
}

func (s *sqlStackStore) GetByID(ctx context.Context, id string) (*Stack, error) {
	query := `SELECT id, project_id, name, compose_yaml, status, created_at, updated_at FROM stacks WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var st Stack
	err := row.Scan(&st.ID, &st.ProjectID, &st.Name, &st.ComposeYAML, &st.Status, &st.CreatedAt, &st.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get stack by id: %w", err)
	}
	return &st, nil
}

func (s *sqlStackStore) GetByName(ctx context.Context, projectID, name string) (*Stack, error) {
	query := `SELECT id, project_id, name, compose_yaml, status, created_at, updated_at FROM stacks WHERE project_id = ? AND name = ?`
	row := s.db.QueryRowContext(ctx, query, projectID, name)

	var st Stack
	err := row.Scan(&st.ID, &st.ProjectID, &st.Name, &st.ComposeYAML, &st.Status, &st.CreatedAt, &st.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get stack by name: %w", err)
	}
	return &st, nil
}

func (s *sqlStackStore) ListByProject(ctx context.Context, projectID string) ([]*Stack, error) {
	query := `SELECT id, project_id, name, compose_yaml, status, created_at, updated_at FROM stacks WHERE project_id = ? ORDER BY name ASC`
	rows, err := s.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list stacks by project: %w", err)
	}
	defer rows.Close()

	var list []*Stack
	for rows.Next() {
		var st Stack
		err := rows.Scan(&st.ID, &st.ProjectID, &st.Name, &st.ComposeYAML, &st.Status, &st.CreatedAt, &st.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan stack: %w", err)
		}
		list = append(list, &st)
	}
	return list, rows.Err()
}

func (s *sqlStackStore) ListAll(ctx context.Context) ([]*Stack, error) {
	query := `SELECT id, project_id, name, compose_yaml, status, created_at, updated_at FROM stacks ORDER BY name ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list all stacks: %w", err)
	}
	defer rows.Close()

	var list []*Stack
	for rows.Next() {
		var st Stack
		err := rows.Scan(&st.ID, &st.ProjectID, &st.Name, &st.ComposeYAML, &st.Status, &st.CreatedAt, &st.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan stack: %w", err)
		}
		list = append(list, &st)
	}
	return list, rows.Err()
}

func (s *sqlStackStore) Update(ctx context.Context, stack *Stack) error {
	stack.UpdatedAt = time.Now().UTC()
	query := `
	UPDATE stacks
	SET name = ?, compose_yaml = ?, status = ?, updated_at = ?
	WHERE id = ?`

	res, err := s.db.ExecContext(ctx, query, stack.Name, stack.ComposeYAML, stack.Status, stack.UpdatedAt, stack.ID)
	if err != nil {
		return fmt.Errorf("store: failed to update stack: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: failed to check rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStackStore) UpdateStatus(ctx context.Context, id string, status string) error {
	query := `UPDATE stacks SET status = ?, updated_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, status, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("store: failed to update stack status: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: failed to check rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStackStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM stacks WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete stack: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: failed to check rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
