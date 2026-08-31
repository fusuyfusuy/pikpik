package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlVolumeStore struct {
	db dbExecutor
}

func (s *sqlVolumeStore) Create(ctx context.Context, v *Volume) error {
	if v.ID == "" {
		v.ID = NewID("vol")
	}
	now := time.Now().UTC()
	if v.CreatedAt.IsZero() {
		v.CreatedAt = now
	}
	if v.UpdatedAt.IsZero() {
		v.UpdatedAt = now
	}

	query := `
	INSERT INTO volumes (
		id, project_id, service_id, name, slug, mount_path, type, host_path, config_content_encrypted, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		v.ID, v.ProjectID, v.ServiceID, v.Name, v.Slug, v.MountPath,
		v.Type, v.HostPath, v.ConfigContentEncrypted, v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		if IsDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return fmt.Errorf("store: failed to create volume: %w", err)
	}
	return nil
}

func (s *sqlVolumeStore) GetByID(ctx context.Context, id string) (*Volume, error) {
	query := `
	SELECT id, project_id, service_id, name, slug, mount_path, type, host_path, config_content_encrypted, created_at, updated_at
	FROM volumes WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)

	var v Volume
	var hostPath, configEnc sql.NullString
	err := row.Scan(
		&v.ID, &v.ProjectID, &v.ServiceID, &v.Name, &v.Slug,
		&v.MountPath, &v.Type, &hostPath, &configEnc, &v.CreatedAt, &v.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get volume by id: %w", err)
	}

	v.HostPath = hostPath.String
	v.ConfigContentEncrypted = configEnc.String
	return &v, nil
}

func (s *sqlVolumeStore) ListByService(ctx context.Context, serviceID string) ([]*Volume, error) {
	query := `
	SELECT id, project_id, service_id, name, slug, mount_path, type, host_path, config_content_encrypted, created_at, updated_at
	FROM volumes WHERE service_id = ? ORDER BY name ASC`

	rows, err := s.db.QueryContext(ctx, query, serviceID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list volumes: %w", err)
	}
	defer rows.Close()

	list := make([]*Volume, 0)
	for rows.Next() {
		var v Volume
		var hostPath, configEnc sql.NullString
		err := rows.Scan(
			&v.ID, &v.ProjectID, &v.ServiceID, &v.Name, &v.Slug,
			&v.MountPath, &v.Type, &hostPath, &configEnc, &v.CreatedAt, &v.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan volume row: %w", err)
		}
		v.HostPath = hostPath.String
		v.ConfigContentEncrypted = configEnc.String
		list = append(list, &v)
	}
	return list, rows.Err()
}

func (s *sqlVolumeStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM volumes WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete volume: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: failed to check deleted rows: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

// Managed Volume operations

func (s *sqlVolumeStore) CreateManaged(ctx context.Context, v *ManagedVolume) error {
	if v.ID == "" {
		v.ID = NewID("vol")
	}
	now := time.Now().UTC()
	if v.CreatedAt.IsZero() {
		v.CreatedAt = now
	}
	if v.UpdatedAt.IsZero() {
		v.UpdatedAt = now
	}
	if v.Driver == "" {
		v.Driver = "local"
	}

	query := `
	INSERT INTO managed_volumes (id, project_id, name, driver, size_bytes, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		v.ID, v.ProjectID, v.Name, v.Driver, v.SizeBytes, v.CreatedAt, v.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to create managed volume: %w", err)
	}
	return nil
}

func (s *sqlVolumeStore) GetManagedByID(ctx context.Context, id string) (*ManagedVolume, error) {
	query := `SELECT id, project_id, name, driver, size_bytes, created_at, updated_at FROM managed_volumes WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var v ManagedVolume
	err := row.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Driver, &v.SizeBytes, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get managed volume by id: %w", err)
	}
	return &v, nil
}

func (s *sqlVolumeStore) GetManagedByName(ctx context.Context, projectID, name string) (*ManagedVolume, error) {
	query := `SELECT id, project_id, name, driver, size_bytes, created_at, updated_at FROM managed_volumes WHERE project_id = ? AND name = ?`
	row := s.db.QueryRowContext(ctx, query, projectID, name)

	var v ManagedVolume
	err := row.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Driver, &v.SizeBytes, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get managed volume by name: %w", err)
	}
	return &v, nil
}

func (s *sqlVolumeStore) ListManagedByProject(ctx context.Context, projectID string) ([]*ManagedVolume, error) {
	query := `SELECT id, project_id, name, driver, size_bytes, created_at, updated_at FROM managed_volumes WHERE project_id = ? ORDER BY name ASC`
	rows, err := s.db.QueryContext(ctx, query, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list managed volumes by project: %w", err)
	}
	defer rows.Close()

	list := make([]*ManagedVolume, 0)
	for rows.Next() {
		var v ManagedVolume
		err := rows.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Driver, &v.SizeBytes, &v.CreatedAt, &v.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan managed volume: %w", err)
		}
		list = append(list, &v)
	}
	return list, rows.Err()
}

func (s *sqlVolumeStore) ListAllManaged(ctx context.Context) ([]*ManagedVolume, error) {
	query := `SELECT id, project_id, name, driver, size_bytes, created_at, updated_at FROM managed_volumes ORDER BY name ASC`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list all managed volumes: %w", err)
	}
	defer rows.Close()

	list := make([]*ManagedVolume, 0)
	for rows.Next() {
		var v ManagedVolume
		err := rows.Scan(&v.ID, &v.ProjectID, &v.Name, &v.Driver, &v.SizeBytes, &v.CreatedAt, &v.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan managed volume: %w", err)
		}
		list = append(list, &v)
	}
	return list, rows.Err()
}

func (s *sqlVolumeStore) DeleteManaged(ctx context.Context, id string) error {
	query := `DELETE FROM managed_volumes WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete managed volume: %w", err)
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

func (s *sqlVolumeStore) DeleteManagedByName(ctx context.Context, projectID, name string) error {
	query := `DELETE FROM managed_volumes WHERE project_id = ? AND name = ?`
	res, err := s.db.ExecContext(ctx, query, projectID, name)
	if err != nil {
		return fmt.Errorf("store: failed to delete managed volume by name: %w", err)
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

