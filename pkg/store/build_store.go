package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// BuildStore defines CRUD operations for git build execution records.
type BuildStore interface {
	Create(ctx context.Context, b *Build) error
	GetByID(ctx context.Context, id string) (*Build, error)
	ListByService(ctx context.Context, serviceID string, limit int) ([]*Build, error)
	UpdateStatus(ctx context.Context, id string, status string, finishedAt *time.Time, durationMS int64, errorMessage string, imageTag string) error
	Delete(ctx context.Context, id string) error
}

// GitHubInstallationStore defines CRUD operations for GitHub App installations.
type GitHubInstallationStore interface {
	Create(ctx context.Context, inst *GitHubInstallation) error
	GetByID(ctx context.Context, id string) (*GitHubInstallation, error)
	GetByInstallationID(ctx context.Context, installationID int64) (*GitHubInstallation, error)
	ListByOrg(ctx context.Context, orgID string) ([]*GitHubInstallation, error)
	Delete(ctx context.Context, id string) error
	DeleteByInstallationID(ctx context.Context, installationID int64) error
}

type sqlBuildStore struct {
	db dbExecutor
}

func (s *sqlBuildStore) Create(ctx context.Context, b *Build) error {
	if b.ID == "" {
		b.ID = NewID("bld")
	}
	if b.StartedAt.IsZero() {
		b.StartedAt = time.Now().UTC()
	}
	if b.Status == "" {
		b.Status = "queued"
	}

	var depID sql.NullString
	if b.DeploymentID != "" {
		depID = sql.NullString{String: b.DeploymentID, Valid: true}
	}

	query := `
	INSERT INTO builds (
		id, service_id, deployment_id, repo_url, branch, commit_sha,
		commit_message, author, status, logs_path, image_tag,
		error_message, duration_ms, started_at, finished_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		b.ID, b.ServiceID, depID, b.RepoURL, b.Branch, b.CommitSHA,
		b.CommitMessage, b.Author, b.Status, b.LogsPath, b.ImageTag,
		b.ErrorMessage, b.DurationMS, b.StartedAt, b.FinishedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to create build: %w", err)
	}
	return nil
}

func (s *sqlBuildStore) GetByID(ctx context.Context, id string) (*Build, error) {
	query := `
	SELECT id, service_id, deployment_id, repo_url, branch, commit_sha,
	       commit_message, author, status, logs_path, image_tag,
	       error_message, duration_ms, started_at, finished_at
	FROM builds WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)

	var b Build
	var depID, commitMsg, author, logsPath, imageTag, errMsg sql.NullString
	var finishedAt sql.NullTime

	err := row.Scan(
		&b.ID, &b.ServiceID, &depID, &b.RepoURL, &b.Branch, &b.CommitSHA,
		&commitMsg, &author, &b.Status, &logsPath, &imageTag,
		&errMsg, &b.DurationMS, &b.StartedAt, &finishedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get build by id: %w", err)
	}

	b.DeploymentID = depID.String
	b.CommitMessage = commitMsg.String
	b.Author = author.String
	b.LogsPath = logsPath.String
	b.ImageTag = imageTag.String
	b.ErrorMessage = errMsg.String
	if finishedAt.Valid {
		b.FinishedAt = &finishedAt.Time
	}

	return &b, nil
}

func (s *sqlBuildStore) ListByService(ctx context.Context, serviceID string, limit int) ([]*Build, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
	SELECT id, service_id, deployment_id, repo_url, branch, commit_sha,
	       commit_message, author, status, logs_path, image_tag,
	       error_message, duration_ms, started_at, finished_at
	FROM builds WHERE service_id = ? ORDER BY started_at DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, serviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list builds: %w", err)
	}
	defer rows.Close()

	var list []*Build
	for rows.Next() {
		var b Build
		var depID, commitMsg, author, logsPath, imageTag, errMsg sql.NullString
		var finishedAt sql.NullTime

		err := rows.Scan(
			&b.ID, &b.ServiceID, &depID, &b.RepoURL, &b.Branch, &b.CommitSHA,
			&commitMsg, &author, &b.Status, &logsPath, &imageTag,
			&errMsg, &b.DurationMS, &b.StartedAt, &finishedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan build row: %w", err)
		}

		b.DeploymentID = depID.String
		b.CommitMessage = commitMsg.String
		b.Author = author.String
		b.LogsPath = logsPath.String
		b.ImageTag = imageTag.String
		b.ErrorMessage = errMsg.String
		if finishedAt.Valid {
			b.FinishedAt = &finishedAt.Time
		}
		list = append(list, &b)
	}

	return list, rows.Err()
}

func (s *sqlBuildStore) UpdateStatus(ctx context.Context, id string, status string, finishedAt *time.Time, durationMS int64, errorMessage string, imageTag string) error {
	query := `
	UPDATE builds
	SET status = ?,
	    finished_at = COALESCE(?, finished_at),
	    duration_ms = CASE WHEN ? > 0 THEN ? ELSE duration_ms END,
	    error_message = CASE WHEN ? != '' THEN ? ELSE error_message END,
	    image_tag = CASE WHEN ? != '' THEN ? ELSE image_tag END
	WHERE id = ?`

	res, err := s.db.ExecContext(ctx, query,
		status,
		finishedAt,
		durationMS, durationMS,
		errorMessage, errorMessage,
		imageTag, imageTag,
		id,
	)
	if err != nil {
		return fmt.Errorf("store: failed to update build status: %w", err)
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

func (s *sqlBuildStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM builds WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete build: %w", err)
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

type sqlGitHubInstallationStore struct {
	db dbExecutor
}

func (s *sqlGitHubInstallationStore) Create(ctx context.Context, inst *GitHubInstallation) error {
	if inst.ID == "" {
		inst.ID = NewID("ghi")
	}
	now := time.Now().UTC()
	if inst.CreatedAt.IsZero() {
		inst.CreatedAt = now
	}
	if inst.UpdatedAt.IsZero() {
		inst.UpdatedAt = now
	}
	if inst.AccountType == "" {
		inst.AccountType = "User"
	}
	if inst.RepositorySelection == "" {
		inst.RepositorySelection = "all"
	}
	if inst.Permissions == "" {
		inst.Permissions = "{}"
	}

	query := `
	INSERT INTO github_installations (
		id, org_id, installation_id, account_name, account_type,
		repository_selection, permissions, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		inst.ID, inst.OrgID, inst.InstallationID, inst.AccountName,
		inst.AccountType, inst.RepositorySelection, inst.Permissions,
		inst.CreatedAt, inst.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to create github installation: %w", err)
	}
	return nil
}

func (s *sqlGitHubInstallationStore) GetByID(ctx context.Context, id string) (*GitHubInstallation, error) {
	query := `
	SELECT id, org_id, installation_id, account_name, account_type,
	       repository_selection, permissions, created_at, updated_at
	FROM github_installations WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)

	var inst GitHubInstallation
	err := row.Scan(
		&inst.ID, &inst.OrgID, &inst.InstallationID, &inst.AccountName,
		&inst.AccountType, &inst.RepositorySelection, &inst.Permissions,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get github installation by id: %w", err)
	}
	return &inst, nil
}

func (s *sqlGitHubInstallationStore) GetByInstallationID(ctx context.Context, installationID int64) (*GitHubInstallation, error) {
	query := `
	SELECT id, org_id, installation_id, account_name, account_type,
	       repository_selection, permissions, created_at, updated_at
	FROM github_installations WHERE installation_id = ?`

	row := s.db.QueryRowContext(ctx, query, installationID)

	var inst GitHubInstallation
	err := row.Scan(
		&inst.ID, &inst.OrgID, &inst.InstallationID, &inst.AccountName,
		&inst.AccountType, &inst.RepositorySelection, &inst.Permissions,
		&inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get github installation by installation id: %w", err)
	}
	return &inst, nil
}

func (s *sqlGitHubInstallationStore) ListByOrg(ctx context.Context, orgID string) ([]*GitHubInstallation, error) {
	query := `
	SELECT id, org_id, installation_id, account_name, account_type,
	       repository_selection, permissions, created_at, updated_at
	FROM github_installations WHERE org_id = ? ORDER BY account_name ASC`

	rows, err := s.db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list github installations: %w", err)
	}
	defer rows.Close()

	var list []*GitHubInstallation
	for rows.Next() {
		var inst GitHubInstallation
		err := rows.Scan(
			&inst.ID, &inst.OrgID, &inst.InstallationID, &inst.AccountName,
			&inst.AccountType, &inst.RepositorySelection, &inst.Permissions,
			&inst.CreatedAt, &inst.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan github installation row: %w", err)
		}
		list = append(list, &inst)
	}
	return list, rows.Err()
}

func (s *sqlGitHubInstallationStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM github_installations WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete github installation: %w", err)
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

func (s *sqlGitHubInstallationStore) DeleteByInstallationID(ctx context.Context, installationID int64) error {
	query := `DELETE FROM github_installations WHERE installation_id = ?`
	res, err := s.db.ExecContext(ctx, query, installationID)
	if err != nil {
		return fmt.Errorf("store: failed to delete github installation: %w", err)
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
