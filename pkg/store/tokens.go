package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

type sqlAPITokenStore struct {
	db      dbExecutor
	writeMu *sync.Mutex
}

func (s *sqlAPITokenStore) Create(ctx context.Context, token *APIToken) error {
	if token.ID == "" {
		token.ID = NewID("tok")
	}
	now := time.Now().UTC()
	if token.CreatedAt.IsZero() {
		token.CreatedAt = now
	}
	if token.Scopes == nil {
		token.Scopes = []string{}
	}

	scopesJSON, err := json.Marshal(token.Scopes)
	if err != nil {
		return fmt.Errorf("store: failed to marshal token scopes: %w", err)
	}

	query := `
	INSERT INTO api_tokens (
		id, user_id, name, prefix, token_hash, scopes, last_used_at, expires_at, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err = s.db.ExecContext(ctx, query,
		token.ID, token.UserID, token.Name, token.Prefix, token.TokenHash,
		string(scopesJSON), token.LastUsedAt, token.ExpiresAt, token.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to create api token: %w", err)
	}
	return nil
}

func (s *sqlAPITokenStore) GetByID(ctx context.Context, id string) (*APIToken, error) {
	query := `
	SELECT id, user_id, name, prefix, token_hash, scopes, last_used_at, expires_at, created_at
	FROM api_tokens WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)
	return s.scanToken(row)
}

func (s *sqlAPITokenStore) GetByHash(ctx context.Context, tokenHash string) (*APIToken, error) {
	query := `
	SELECT id, user_id, name, prefix, token_hash, scopes, last_used_at, expires_at, created_at
	FROM api_tokens WHERE token_hash = ?`

	row := s.db.QueryRowContext(ctx, query, tokenHash)
	return s.scanToken(row)
}

func (s *sqlAPITokenStore) scanToken(row *sql.Row) (*APIToken, error) {
	var token APIToken
	var scopesJSON string

	err := row.Scan(
		&token.ID, &token.UserID, &token.Name, &token.Prefix,
		&token.TokenHash, &scopesJSON, &token.LastUsedAt, &token.ExpiresAt, &token.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to scan token: %w", err)
	}

	if err := json.Unmarshal([]byte(scopesJSON), &token.Scopes); err != nil {
		token.Scopes = []string{}
	}
	return &token, nil
}

func (s *sqlAPITokenStore) ListByUser(ctx context.Context, userID string) ([]*APIToken, error) {
	query := `
	SELECT id, user_id, name, prefix, token_hash, scopes, last_used_at, expires_at, created_at
	FROM api_tokens WHERE user_id = ? ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("store: failed to list user tokens: %w", err)
	}
	defer rows.Close()

	var tokens []*APIToken
	for rows.Next() {
		var token APIToken
		var scopesJSON string

		err := rows.Scan(
			&token.ID, &token.UserID, &token.Name, &token.Prefix,
			&token.TokenHash, &scopesJSON, &token.LastUsedAt, &token.ExpiresAt, &token.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("store: failed to scan token row: %w", err)
		}

		if err := json.Unmarshal([]byte(scopesJSON), &token.Scopes); err != nil {
			token.Scopes = []string{}
		}
		tokens = append(tokens, &token)
	}
	return tokens, rows.Err()
}

func (s *sqlAPITokenStore) TouchLastUsed(ctx context.Context, id string) error {
	now := time.Now().UTC()
	query := `UPDATE api_tokens SET last_used_at = ? WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query, now, id)
	if err != nil {
		return fmt.Errorf("store: failed to touch token last_used_at: %w", err)
	}
	return nil
}

func (s *sqlAPITokenStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM api_tokens WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete token: %w", err)
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
