package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlSessionStore struct {
	db dbExecutor
}

func (s *sqlSessionStore) Create(ctx context.Context, session *Session) error {
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now().UTC()
	}

	query := `INSERT INTO sessions (id, user_id, ip_address, user_agent, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query, session.ID, session.UserID, session.IPAddress, session.UserAgent, session.ExpiresAt, session.CreatedAt)
	if err != nil {
		return fmt.Errorf("store: failed to create session: %w", err)
	}
	return nil
}

func (s *sqlSessionStore) GetByID(ctx context.Context, id string) (*Session, error) {
	query := `SELECT id, user_id, ip_address, user_agent, expires_at, created_at FROM sessions WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)

	var session Session
	err := row.Scan(&session.ID, &session.UserID, &session.IPAddress, &session.UserAgent, &session.ExpiresAt, &session.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get session: %w", err)
	}
	return &session, nil
}

func (s *sqlSessionStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM sessions WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete session: %w", err)
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

func (s *sqlSessionStore) DeleteByUserID(ctx context.Context, userID string) error {
	query := `DELETE FROM sessions WHERE user_id = ?`
	_, err := s.db.ExecContext(ctx, query, userID)
	if err != nil {
		return fmt.Errorf("store: failed to delete user sessions: %w", err)
	}
	return nil
}

func (s *sqlSessionStore) CleanExpired(ctx context.Context) (int64, error) {
	query := `DELETE FROM sessions WHERE expires_at <= ?`
	res, err := s.db.ExecContext(ctx, query, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("store: failed to clean expired sessions: %w", err)
	}
	return res.RowsAffected()
}
