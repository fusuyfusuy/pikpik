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

	var list []*Volume
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
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
