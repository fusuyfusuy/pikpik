package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type sqlServiceStore struct {
	db dbExecutor
}

func (s *sqlServiceStore) Create(ctx context.Context, svc *Service) error {
	if svc.ID == "" {
		svc.ID = NewID("svc")
	}
	now := time.Now().UTC()
	if svc.CreatedAt.IsZero() {
		svc.CreatedAt = now
	}
	if svc.UpdatedAt.IsZero() {
		svc.UpdatedAt = now
	}
	if svc.Replicas <= 0 {
		svc.Replicas = 1
	}
	if svc.Type == "" {
		svc.Type = "app"
	}
	if svc.Status == "" {
		svc.Status = "idle"
	}
	if svc.DomainNames == nil {
		svc.DomainNames = []string{}
	}

	domainsJSON, err := json.Marshal(svc.DomainNames)
	if err != nil {
		return fmt.Errorf("store: failed to marshal domain names: %w", err)
	}

	query := `
	INSERT INTO services (
		id, project_id, stage_id, name, slug, type, image, replicas, container_port, domain_names, deploy_token_hash, status, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = s.db.ExecContext(ctx, query,
		svc.ID, svc.ProjectID, svc.StageID, svc.Name, svc.Slug, svc.Type,
		svc.Image, svc.Replicas, svc.ContainerPort, string(domainsJSON),
		svc.DeployTokenHash, svc.Status, svc.CreatedAt, svc.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to create service: %w", err)
	}
	return nil
}

func (s *sqlServiceStore) GetByID(ctx context.Context, id string) (*Service, error) {
	query := `
	SELECT id, project_id, stage_id, name, slug, type, image, replicas, container_port, domain_names, deploy_token_hash, status, created_at, updated_at
	FROM services WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanService(row)
}

func (s *sqlServiceStore) GetBySlug(ctx context.Context, projectID, stageID, slug string) (*Service, error) {
	query := `
	SELECT id, project_id, stage_id, name, slug, type, image, replicas, container_port, domain_names, deploy_token_hash, status, created_at, updated_at
	FROM services WHERE project_id = ? AND stage_id = ? AND slug = ?`

	row := s.db.QueryRowContext(ctx, query, projectID, stageID, slug)
	return s.scanService(row)
}

func (s *sqlServiceStore) scanService(row *sql.Row) (*Service, error) {
	var svc Service
	var domainsJSON string
	var containerPort sql.NullInt64
	var deployTokenHash sql.NullString

	err := row.Scan(
		&svc.ID, &svc.ProjectID, &svc.StageID, &svc.Name, &svc.Slug, &svc.Type,
		&svc.Image, &svc.Replicas, &containerPort, &domainsJSON,
		&deployTokenHash, &svc.Status, &svc.CreatedAt, &svc.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to scan service: %w", err)
	}

	if containerPort.Valid {
		svc.ContainerPort = int(containerPort.Int64)
	}
	svc.DeployTokenHash = deployTokenHash.String

	if err := json.Unmarshal([]byte(domainsJSON), &svc.DomainNames); err != nil {
		svc.DomainNames = []string{}
	}
	return &svc, nil
}

func (s *sqlServiceStore) ListByStage(ctx context.Context, stageID string) ([]*Service, error) {
	query := `
	SELECT id, project_id, stage_id, name, slug, type, image, replicas, container_port, domain_names, deploy_token_hash, status, created_at, updated_at
	FROM services WHERE stage_id = ? ORDER BY name ASC`

	rows, err := s.db.QueryContext(ctx, query, stageID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list services: %w", err)
	}
	defer rows.Close()

	var services []*Service
	for rows.Next() {
		var svc Service
		var domainsJSON string
		var containerPort sql.NullInt64
		var deployTokenHash sql.NullString

		err := rows.Scan(
			&svc.ID, &svc.ProjectID, &svc.StageID, &svc.Name, &svc.Slug, &svc.Type,
			&svc.Image, &svc.Replicas, &containerPort, &domainsJSON,
			&deployTokenHash, &svc.Status, &svc.CreatedAt, &svc.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan service row: %w", err)
		}

		if containerPort.Valid {
			svc.ContainerPort = int(containerPort.Int64)
		}
		svc.DeployTokenHash = deployTokenHash.String

		if err := json.Unmarshal([]byte(domainsJSON), &svc.DomainNames); err != nil {
			svc.DomainNames = []string{}
		}
		services = append(services, &svc)
	}
	return services, rows.Err()
}

func (s *sqlServiceStore) UpdateStatus(ctx context.Context, id string, status string) error {
	now := time.Now().UTC()
	query := `UPDATE services SET status = ?, updated_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, status, now, id)
	if err != nil {
		return fmt.Errorf("store: failed to update service status: %w", err)
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

func (s *sqlServiceStore) Update(ctx context.Context, svc *Service) error {
	now := time.Now().UTC()
	svc.UpdatedAt = now

	domainsJSON, err := json.Marshal(svc.DomainNames)
	if err != nil {
		return fmt.Errorf("store: failed to marshal domain names: %w", err)
	}

	query := `
	UPDATE services SET
		name = ?, slug = ?, type = ?, image = ?, replicas = ?,
		container_port = ?, domain_names = ?, deploy_token_hash = ?, status = ?, updated_at = ?
	WHERE id = ?`

	res, err := s.db.ExecContext(ctx, query,
		svc.Name, svc.Slug, svc.Type, svc.Image, svc.Replicas,
		svc.ContainerPort, string(domainsJSON), svc.DeployTokenHash, svc.Status, svc.UpdatedAt, svc.ID,
	)
	if err != nil {
		return fmt.Errorf("store: failed to update service: %w", err)
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

func (s *sqlServiceStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM services WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete service: %w", err)
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
