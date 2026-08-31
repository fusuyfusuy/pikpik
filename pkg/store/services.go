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
	if svc.Tags == nil {
		svc.Tags = []string{}
	}
	if svc.RuntimeMode == "" {
		svc.RuntimeMode = "standalone"
	}

	domainsJSON, err := json.Marshal(svc.DomainNames)
	if err != nil {
		return fmt.Errorf("store: failed to marshal domain names: %w", err)
	}

	tagsJSON, err := json.Marshal(svc.Tags)
	if err != nil {
		return fmt.Errorf("store: failed to marshal tags: %w", err)
	}

	query := `
	INSERT INTO services (
		id, project_id, stage_id, name, slug, type, image, replicas, container_port, domain_names, tags, runtime_mode, compose_yaml, deploy_token_hash, status, git_repo_url, git_branch, build_strategy, dockerfile_path, publish_directory, last_commit_sha, last_commit_message, last_commit_author, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = s.db.ExecContext(ctx, query,
		svc.ID, svc.ProjectID, svc.StageID, svc.Name, svc.Slug, svc.Type,
		svc.Image, svc.Replicas, svc.ContainerPort, string(domainsJSON), string(tagsJSON),
		svc.RuntimeMode, svc.ComposeYAML,
		svc.DeployTokenHash, svc.Status, svc.GitRepoURL, svc.GitBranch,
		svc.BuildStrategy, svc.DockerfilePath, svc.PublishDirectory,
		svc.LastCommitSHA, svc.LastCommitMessage, svc.LastCommitAuthor,
		svc.CreatedAt, svc.UpdatedAt,
	)
	if err != nil {
		if IsDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return fmt.Errorf("store: failed to create service: %w", err)
	}
	return nil
}

func (s *sqlServiceStore) GetByID(ctx context.Context, id string) (*Service, error) {
	query := `
	SELECT id, project_id, stage_id, name, slug, type, image, replicas, container_port, domain_names, tags, runtime_mode, compose_yaml, deploy_token_hash, status, git_repo_url, git_branch, build_strategy, dockerfile_path, publish_directory, last_commit_sha, last_commit_message, last_commit_author, created_at, updated_at
	FROM services WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanService(row)
}

func (s *sqlServiceStore) GetBySlug(ctx context.Context, projectID, stageID, slug string) (*Service, error) {
	query := `
	SELECT id, project_id, stage_id, name, slug, type, image, replicas, container_port, domain_names, tags, runtime_mode, compose_yaml, deploy_token_hash, status, git_repo_url, git_branch, build_strategy, dockerfile_path, publish_directory, last_commit_sha, last_commit_message, last_commit_author, created_at, updated_at
	FROM services WHERE project_id = ? AND stage_id = ? AND slug = ?`

	row := s.db.QueryRowContext(ctx, query, projectID, stageID, slug)
	return s.scanService(row)
}

func (s *sqlServiceStore) scanService(row *sql.Row) (*Service, error) {
	var svc Service
	var domainsJSON string
	var tagsJSON sql.NullString
	var runtimeMode sql.NullString
	var composeYAML sql.NullString
	var containerPort sql.NullInt64
	var deployTokenHash sql.NullString
	var gitRepoURL sql.NullString
	var gitBranch sql.NullString
	var buildStrategy sql.NullString
	var dockerfilePath sql.NullString
	var publishDirectory sql.NullString
	var lastCommitSHA sql.NullString
	var lastCommitMessage sql.NullString
	var lastCommitAuthor sql.NullString

	err := row.Scan(
		&svc.ID, &svc.ProjectID, &svc.StageID, &svc.Name, &svc.Slug, &svc.Type,
		&svc.Image, &svc.Replicas, &containerPort, &domainsJSON, &tagsJSON,
		&runtimeMode, &composeYAML,
		&deployTokenHash, &svc.Status,
		&gitRepoURL, &gitBranch, &buildStrategy, &dockerfilePath, &publishDirectory,
		&lastCommitSHA, &lastCommitMessage, &lastCommitAuthor,
		&svc.CreatedAt, &svc.UpdatedAt,
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
	if runtimeMode.Valid && runtimeMode.String != "" {
		svc.RuntimeMode = runtimeMode.String
	} else {
		svc.RuntimeMode = "standalone"
	}
	svc.ComposeYAML = composeYAML.String
	svc.DeployTokenHash = deployTokenHash.String
	svc.GitRepoURL = gitRepoURL.String
	svc.GitBranch = gitBranch.String
	svc.BuildStrategy = buildStrategy.String
	svc.DockerfilePath = dockerfilePath.String
	svc.PublishDirectory = publishDirectory.String
	svc.LastCommitSHA = lastCommitSHA.String
	svc.LastCommitMessage = lastCommitMessage.String
	svc.LastCommitAuthor = lastCommitAuthor.String

	if err := json.Unmarshal([]byte(domainsJSON), &svc.DomainNames); err != nil {
		svc.DomainNames = []string{}
	}
	svc.Tags = []string{}
	if tagsJSON.Valid && tagsJSON.String != "" {
		_ = json.Unmarshal([]byte(tagsJSON.String), &svc.Tags)
	}
	return &svc, nil
}

func (s *sqlServiceStore) ListByStage(ctx context.Context, stageID string) ([]*Service, error) {
	query := `
	SELECT id, project_id, stage_id, name, slug, type, image, replicas, container_port, domain_names, tags, runtime_mode, compose_yaml, deploy_token_hash, status, git_repo_url, git_branch, build_strategy, dockerfile_path, publish_directory, last_commit_sha, last_commit_message, last_commit_author, created_at, updated_at
	FROM services WHERE stage_id = ? ORDER BY name ASC`

	return s.queryServices(ctx, query, stageID)
}

func (s *sqlServiceStore) ListByProject(ctx context.Context, projectID string) ([]*Service, error) {
	query := `
	SELECT id, project_id, stage_id, name, slug, type, image, replicas, container_port, domain_names, tags, runtime_mode, compose_yaml, deploy_token_hash, status, git_repo_url, git_branch, build_strategy, dockerfile_path, publish_directory, last_commit_sha, last_commit_message, last_commit_author, created_at, updated_at
	FROM services WHERE project_id = ? ORDER BY name ASC`

	return s.queryServices(ctx, query, projectID)
}

func (s *sqlServiceStore) ListAll(ctx context.Context) ([]*Service, error) {
	query := `
	SELECT id, project_id, stage_id, name, slug, type, image, replicas, container_port, domain_names, tags, runtime_mode, compose_yaml, deploy_token_hash, status, git_repo_url, git_branch, build_strategy, dockerfile_path, publish_directory, last_commit_sha, last_commit_message, last_commit_author, created_at, updated_at
	FROM services ORDER BY name ASC`

	return s.queryServices(ctx, query)
}

func (s *sqlServiceStore) queryServices(ctx context.Context, query string, args ...any) ([]*Service, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: failed to query services: %w", err)
	}
	defer rows.Close()

	services := make([]*Service, 0)
	for rows.Next() {
		var svc Service
		var domainsJSON string
		var tagsJSON sql.NullString
		var runtimeMode sql.NullString
		var composeYAML sql.NullString
		var containerPort sql.NullInt64
		var deployTokenHash sql.NullString
		var gitRepoURL sql.NullString
		var gitBranch sql.NullString
		var buildStrategy sql.NullString
		var dockerfilePath sql.NullString
		var publishDirectory sql.NullString
		var lastCommitSHA sql.NullString
		var lastCommitMessage sql.NullString
		var lastCommitAuthor sql.NullString

		err := rows.Scan(
			&svc.ID, &svc.ProjectID, &svc.StageID, &svc.Name, &svc.Slug, &svc.Type,
			&svc.Image, &svc.Replicas, &containerPort, &domainsJSON, &tagsJSON,
			&runtimeMode, &composeYAML,
			&deployTokenHash, &svc.Status,
			&gitRepoURL, &gitBranch, &buildStrategy, &dockerfilePath, &publishDirectory,
			&lastCommitSHA, &lastCommitMessage, &lastCommitAuthor,
			&svc.CreatedAt, &svc.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan service row: %w", err)
		}

		if containerPort.Valid {
			svc.ContainerPort = int(containerPort.Int64)
		}
		if runtimeMode.Valid && runtimeMode.String != "" {
			svc.RuntimeMode = runtimeMode.String
		} else {
			svc.RuntimeMode = "standalone"
		}
		svc.ComposeYAML = composeYAML.String
		svc.DeployTokenHash = deployTokenHash.String
		svc.GitRepoURL = gitRepoURL.String
		svc.GitBranch = gitBranch.String
		svc.BuildStrategy = buildStrategy.String
		svc.DockerfilePath = dockerfilePath.String
		svc.PublishDirectory = publishDirectory.String
		svc.LastCommitSHA = lastCommitSHA.String
		svc.LastCommitMessage = lastCommitMessage.String
		svc.LastCommitAuthor = lastCommitAuthor.String

		if err := json.Unmarshal([]byte(domainsJSON), &svc.DomainNames); err != nil {
			svc.DomainNames = []string{}
		}
		svc.Tags = []string{}
		if tagsJSON.Valid && tagsJSON.String != "" {
			_ = json.Unmarshal([]byte(tagsJSON.String), &svc.Tags)
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

func (s *sqlServiceStore) UpdateCommitMetadata(ctx context.Context, id string, sha, message, author string) error {
	now := time.Now().UTC()
	query := `UPDATE services SET last_commit_sha = ?, last_commit_message = ?, last_commit_author = ?, updated_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, sha, message, author, now, id)
	if err != nil {
		return fmt.Errorf("store: failed to update service commit metadata: %w", err)
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
	if svc.Tags == nil {
		svc.Tags = []string{}
	}
	tagsJSON, err := json.Marshal(svc.Tags)
	if err != nil {
		return fmt.Errorf("store: failed to marshal tags: %w", err)
	}
	if svc.RuntimeMode == "" {
		svc.RuntimeMode = "standalone"
	}

	query := `
	UPDATE services SET
		project_id = ?, stage_id = ?, name = ?, slug = ?, type = ?, image = ?, replicas = ?,
		container_port = ?, domain_names = ?, tags = ?, runtime_mode = ?, compose_yaml = ?, deploy_token_hash = ?, status = ?,
		git_repo_url = ?, git_branch = ?, build_strategy = ?, dockerfile_path = ?, publish_directory = ?,
		last_commit_sha = ?, last_commit_message = ?, last_commit_author = ?,
		updated_at = ?
	WHERE id = ?`

	res, err := s.db.ExecContext(ctx, query,
		svc.ProjectID, svc.StageID, svc.Name, svc.Slug, svc.Type, svc.Image, svc.Replicas,
		svc.ContainerPort, string(domainsJSON), string(tagsJSON), svc.RuntimeMode, svc.ComposeYAML, svc.DeployTokenHash, svc.Status,
		svc.GitRepoURL, svc.GitBranch, svc.BuildStrategy, svc.DockerfilePath, svc.PublishDirectory,
		svc.LastCommitSHA, svc.LastCommitMessage, svc.LastCommitAuthor,
		svc.UpdatedAt, svc.ID,
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
