package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlMachineStore struct {
	db dbExecutor
}

func (s *sqlMachineStore) Create(ctx context.Context, m *ManagedMachine) error {
	if m.ID == "" {
		m.ID = NewID("mch")
	}
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	if m.UpdatedAt.IsZero() {
		m.UpdatedAt = now
	}
	if m.Status == "" {
		m.Status = "offline"
	}
	if m.Role == "" {
		m.Role = "worker"
	}

	query := `
	INSERT INTO managed_machines (
		id, hostname, role, public_ip, private_ip,
		os_kernel, cpu_arch, docker_version, agent_version,
		status, last_seen, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		m.ID, m.Hostname, m.Role, m.PublicIP, m.PrivateIP,
		m.OSKernel, m.CPUArch, m.DockerVersion, m.AgentVersion,
		m.Status, m.LastSeen, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		if IsDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return fmt.Errorf("store: failed to create managed machine: %w", err)
	}
	return nil
}

func (s *sqlMachineStore) GetByID(ctx context.Context, id string) (*ManagedMachine, error) {
	query := `
	SELECT id, hostname, role, public_ip, private_ip,
	       os_kernel, cpu_arch, docker_version, agent_version,
	       status, last_seen, created_at, updated_at
	FROM managed_machines
	WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)
	return scanMachine(row)
}

func (s *sqlMachineStore) GetByHostname(ctx context.Context, hostname string) (*ManagedMachine, error) {
	query := `
	SELECT id, hostname, role, public_ip, private_ip,
	       os_kernel, cpu_arch, docker_version, agent_version,
	       status, last_seen, created_at, updated_at
	FROM managed_machines
	WHERE hostname = ?
	LIMIT 1`

	row := s.db.QueryRowContext(ctx, query, hostname)
	return scanMachine(row)
}

func (s *sqlMachineStore) List(ctx context.Context) ([]*ManagedMachine, error) {
	query := `
	SELECT id, hostname, role, public_ip, private_ip,
	       os_kernel, cpu_arch, docker_version, agent_version,
	       status, last_seen, created_at, updated_at
	FROM managed_machines
	ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list managed machines: %w", err)
	}
	defer rows.Close()

	var machines []*ManagedMachine
	for rows.Next() {
		m, err := scanMachineRow(rows)
		if err != nil {
			return nil, err
		}
		machines = append(machines, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: error iterating machines: %w", err)
	}
	return machines, nil
}

func (s *sqlMachineStore) Update(ctx context.Context, m *ManagedMachine) error {
	m.UpdatedAt = time.Now().UTC()

	query := `
	UPDATE managed_machines SET
		hostname = ?,
		role = ?,
		public_ip = ?,
		private_ip = ?,
		os_kernel = ?,
		cpu_arch = ?,
		docker_version = ?,
		agent_version = ?,
		status = ?,
		last_seen = ?,
		updated_at = ?
	WHERE id = ?`

	res, err := s.db.ExecContext(ctx, query,
		m.Hostname, m.Role, m.PublicIP, m.PrivateIP,
		m.OSKernel, m.CPUArch, m.DockerVersion, m.AgentVersion,
		m.Status, m.LastSeen, m.UpdatedAt, m.ID,
	)
	if err != nil {
		return fmt.Errorf("store: failed to update managed machine: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlMachineStore) Upsert(ctx context.Context, m *ManagedMachine) error {
	if m.ID == "" {
		m.ID = NewID("mch")
	}
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	if m.Status == "" {
		m.Status = "online"
	}
	if m.Role == "" {
		m.Role = "worker"
	}

	query := `
	INSERT INTO managed_machines (
		id, hostname, role, public_ip, private_ip,
		os_kernel, cpu_arch, docker_version, agent_version,
		status, last_seen, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		hostname = excluded.hostname,
		role = excluded.role,
		public_ip = CASE WHEN excluded.public_ip != '' THEN excluded.public_ip ELSE managed_machines.public_ip END,
		private_ip = CASE WHEN excluded.private_ip != '' THEN excluded.private_ip ELSE managed_machines.private_ip END,
		os_kernel = CASE WHEN excluded.os_kernel != '' THEN excluded.os_kernel ELSE managed_machines.os_kernel END,
		cpu_arch = CASE WHEN excluded.cpu_arch != '' THEN excluded.cpu_arch ELSE managed_machines.cpu_arch END,
		docker_version = CASE WHEN excluded.docker_version != '' THEN excluded.docker_version ELSE managed_machines.docker_version END,
		agent_version = CASE WHEN excluded.agent_version != '' THEN excluded.agent_version ELSE managed_machines.agent_version END,
		status = excluded.status,
		last_seen = excluded.last_seen,
		updated_at = excluded.updated_at`

	_, err := s.db.ExecContext(ctx, query,
		m.ID, m.Hostname, m.Role, m.PublicIP, m.PrivateIP,
		m.OSKernel, m.CPUArch, m.DockerVersion, m.AgentVersion,
		m.Status, m.LastSeen, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to upsert managed machine: %w", err)
	}
	return nil
}

func (s *sqlMachineStore) UpdateStatus(ctx context.Context, id string, status string, lastSeen time.Time) error {
	now := time.Now().UTC()
	query := `
	UPDATE managed_machines SET
		status = ?,
		last_seen = ?,
		updated_at = ?
	WHERE id = ?`

	res, err := s.db.ExecContext(ctx, query, status, lastSeen, now, id)
	if err != nil {
		return fmt.Errorf("store: failed to update machine status: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlMachineStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM managed_machines WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete managed machine: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: failed to get rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func scanMachine(row *sql.Row) (*ManagedMachine, error) {
	var m ManagedMachine
	var lastSeen sql.NullTime

	err := row.Scan(
		&m.ID, &m.Hostname, &m.Role, &m.PublicIP, &m.PrivateIP,
		&m.OSKernel, &m.CPUArch, &m.DockerVersion, &m.AgentVersion,
		&m.Status, &lastSeen, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get managed machine: %w", err)
	}
	if lastSeen.Valid {
		m.LastSeen = &lastSeen.Time
	}
	return &m, nil
}

func scanMachineRow(rows *sql.Rows) (*ManagedMachine, error) {
	var m ManagedMachine
	var lastSeen sql.NullTime

	err := rows.Scan(
		&m.ID, &m.Hostname, &m.Role, &m.PublicIP, &m.PrivateIP,
		&m.OSKernel, &m.CPUArch, &m.DockerVersion, &m.AgentVersion,
		&m.Status, &lastSeen, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("store: failed to scan managed machine row: %w", err)
	}
	if lastSeen.Valid {
		m.LastSeen = &lastSeen.Time
	}
	return &m, nil
}
