package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlNetworkStore struct {
	db dbExecutor
}

func (s *sqlNetworkStore) Create(ctx context.Context, net *ManagedNetwork) error {
	if net.ID == "" {
		net.ID = NewID("net")
	}
	now := time.Now().UTC()
	if net.CreatedAt.IsZero() {
		net.CreatedAt = now
	}
	if net.UpdatedAt.IsZero() {
		net.UpdatedAt = now
	}
	if net.Driver == "" {
		net.Driver = "bridge"
	}
	if net.Scope == "" {
		net.Scope = "project"
	}

	isExt := 0
	if net.IsExternal {
		isExt = 1
	}

	query := `
	INSERT INTO managed_networks (id, project_id, name, driver, scope, is_external, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		net.ID, net.ProjectID, net.Name, net.Driver, net.Scope, isExt, net.CreatedAt, net.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to create managed network: %w", err)
	}
	return nil
}

func (s *sqlNetworkStore) GetByID(ctx context.Context, id string) (*ManagedNetwork, error) {
	query := `SELECT id, project_id, name, driver, scope, is_external, created_at, updated_at FROM managed_networks WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var n ManagedNetwork
	var isExt int
	err := row.Scan(&n.ID, &n.ProjectID, &n.Name, &n.Driver, &n.Scope, &isExt, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get managed network by id: %w", err)
	}
	n.IsExternal = isExt == 1
	return &n, nil
}

func (s *sqlNetworkStore) GetByName(ctx context.Context, projectID, name string) (*ManagedNetwork, error) {
	query := `SELECT id, project_id, name, driver, scope, is_external, created_at, updated_at FROM managed_networks WHERE project_id = ? AND name = ?`
	row := s.db.QueryRowContext(ctx, query, projectID, name)

	var n ManagedNetwork
	var isExt int
	err := row.Scan(&n.ID, &n.ProjectID, &n.Name, &n.Driver, &n.Scope, &isExt, &n.CreatedAt, &n.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get managed network by name: %w", err)
	}
	n.IsExternal = isExt == 1
	return &n, nil
}

func (s *sqlNetworkStore) ListByProject(ctx context.Context, projectID string) ([]*ManagedNetwork, error) {
	query := `SELECT id, project_id, name, driver, scope, is_external, created_at, updated_at FROM managed_networks WHERE project_id = ? ORDER BY name ASC`
	rows, err := s.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list managed networks by project: %w", err)
	}
	defer rows.Close()

	var list []*ManagedNetwork
	for rows.Next() {
		var n ManagedNetwork
		var isExt int
		err := rows.Scan(&n.ID, &n.ProjectID, &n.Name, &n.Driver, &n.Scope, &isExt, &n.CreatedAt, &n.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan managed network: %w", err)
		}
		n.IsExternal = isExt == 1
		list = append(list, &n)
	}
	return list, rows.Err()
}

func (s *sqlNetworkStore) ListAll(ctx context.Context) ([]*ManagedNetwork, error) {
	query := `SELECT id, project_id, name, driver, scope, is_external, created_at, updated_at FROM managed_networks ORDER BY name ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list all managed networks: %w", err)
	}
	defer rows.Close()

	var list []*ManagedNetwork
	for rows.Next() {
		var n ManagedNetwork
		var isExt int
		err := rows.Scan(&n.ID, &n.ProjectID, &n.Name, &n.Driver, &n.Scope, &isExt, &n.CreatedAt, &n.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan managed network: %w", err)
		}
		n.IsExternal = isExt == 1
		list = append(list, &n)
	}
	return list, rows.Err()
}

func (s *sqlNetworkStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM managed_networks WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete managed network: %w", err)
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

func (s *sqlNetworkStore) DeleteByName(ctx context.Context, projectID, name string) error {
	query := `DELETE FROM managed_networks WHERE project_id = ? AND name = ?`
	res, err := s.db.ExecContext(ctx, query, projectID, name)
	if err != nil {
		return fmt.Errorf("store: failed to delete managed network by name: %w", err)
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
