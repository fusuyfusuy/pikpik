package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlDeploymentStore struct {
	db dbExecutor
}

func (s *sqlDeploymentStore) Create(ctx context.Context, d *Deployment) error {
	if d.ID == "" {
		d.ID = NewID("dep")
	}
	if d.StartedAt.IsZero() {
		d.StartedAt = time.Now().UTC()
	}
	if d.Status == "" {
		d.Status = "queued"
	}
	if d.CommitSHA == "" && d.LastCommitSHA != "" {
		d.CommitSHA = d.LastCommitSHA
	}
	if d.LastCommitSHA == "" && d.CommitSHA != "" {
		d.LastCommitSHA = d.CommitSHA
	}

	query := `
	INSERT INTO deployments (
		id, service_id, image_tag, commit_sha, last_commit_sha, last_commit_message, last_commit_author, status, logs_summary, initiated_by, started_at, finished_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		d.ID, d.ServiceID, d.ImageTag, d.CommitSHA, d.LastCommitSHA, d.LastCommitMessage, d.LastCommitAuthor, d.Status,
		d.LogsSummary, d.InitiatedBy, d.StartedAt, d.FinishedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to create deployment: %w", err)
	}
	return nil
}

func (s *sqlDeploymentStore) GetByID(ctx context.Context, id string) (*Deployment, error) {
	query := `
	SELECT id, service_id, image_tag, commit_sha, last_commit_sha, last_commit_message, last_commit_author, status, logs_summary, initiated_by, started_at, finished_at
	FROM deployments WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)

	var d Deployment
	var commitSHA, lastCommitSHA, lastCommitMsg, lastCommitAuthor, logsSummary sql.NullString
	var finishedAt sql.NullTime

	err := row.Scan(
		&d.ID, &d.ServiceID, &d.ImageTag, &commitSHA, &lastCommitSHA, &lastCommitMsg, &lastCommitAuthor, &d.Status,
		&logsSummary, &d.InitiatedBy, &d.StartedAt, &finishedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get deployment by id: %w", err)
	}

	d.CommitSHA = commitSHA.String
	d.LastCommitSHA = lastCommitSHA.String
	if d.LastCommitSHA == "" && d.CommitSHA != "" {
		d.LastCommitSHA = d.CommitSHA
	}
	if d.CommitSHA == "" && d.LastCommitSHA != "" {
		d.CommitSHA = d.LastCommitSHA
	}
	d.LastCommitMessage = lastCommitMsg.String
	d.LastCommitAuthor = lastCommitAuthor.String
	d.LogsSummary = logsSummary.String
	if finishedAt.Valid {
		d.FinishedAt = &finishedAt.Time
	}
	return &d, nil
}

func (s *sqlDeploymentStore) ListByService(ctx context.Context, serviceID string, limit int) ([]*Deployment, error) {
	if limit <= 0 {
		limit = 50
	}

	query := `
	SELECT id, service_id, image_tag, commit_sha, last_commit_sha, last_commit_message, last_commit_author, status, logs_summary, initiated_by, started_at, finished_at
	FROM deployments WHERE service_id = ? ORDER BY started_at DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, serviceID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list deployments: %w", err)
	}
	defer rows.Close()

	var list []*Deployment
	for rows.Next() {
		var d Deployment
		var commitSHA, lastCommitSHA, lastCommitMsg, lastCommitAuthor, logsSummary sql.NullString
		var finishedAt sql.NullTime

		err := rows.Scan(
			&d.ID, &d.ServiceID, &d.ImageTag, &commitSHA, &lastCommitSHA, &lastCommitMsg, &lastCommitAuthor, &d.Status,
			&logsSummary, &d.InitiatedBy, &d.StartedAt, &finishedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan deployment row: %w", err)
		}

		d.CommitSHA = commitSHA.String
		d.LastCommitSHA = lastCommitSHA.String
		if d.LastCommitSHA == "" && d.CommitSHA != "" {
			d.LastCommitSHA = d.CommitSHA
		}
		if d.CommitSHA == "" && d.LastCommitSHA != "" {
			d.CommitSHA = d.LastCommitSHA
		}
		d.LastCommitMessage = lastCommitMsg.String
		d.LastCommitAuthor = lastCommitAuthor.String
		d.LogsSummary = logsSummary.String
		if finishedAt.Valid {
			d.FinishedAt = &finishedAt.Time
		}
		list = append(list, &d)
	}
	return list, rows.Err()
}

func (s *sqlDeploymentStore) UpdateStatus(ctx context.Context, id string, status string, finishedAt *time.Time, logsSummary *string) error {
	query := `UPDATE deployments SET status = ?, finished_at = COALESCE(?, finished_at), logs_summary = COALESCE(?, logs_summary) WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, status, finishedAt, logsSummary, id)
	if err != nil {
		return fmt.Errorf("store: failed to update deployment status: %w", err)
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
