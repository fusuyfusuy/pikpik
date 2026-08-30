package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

type sqlUserStore struct {
	db dbExecutor
}

func (s *sqlUserStore) Create(ctx context.Context, user *User) error {
	if user.ID == "" {
		user.ID = NewID("usr")
	}
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = now
	}
	if user.Role == "" {
		user.Role = "owner"
	}
	if user.SessionVersion == 0 {
		user.SessionVersion = 1
	}

	totpEnabled := 0
	if user.TOTPEnabled {
		totpEnabled = 1
	}

	query := `
	INSERT INTO users (
		id, email, password_hash, role, totp_secret, totp_enabled, session_version, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := s.db.ExecContext(ctx, query,
		user.ID, user.Email, user.PasswordHash, user.Role, user.TOTPSecret,
		totpEnabled, user.SessionVersion, user.CreatedAt, user.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: failed to create user: %w", err)
	}
	return nil
}

func (s *sqlUserStore) GetByID(ctx context.Context, id string) (*User, error) {
	query := `
	SELECT id, email, password_hash, role, totp_secret, totp_enabled, session_version, created_at, updated_at
	FROM users WHERE id = ?`

	row := s.db.QueryRowContext(ctx, query, id)

	var user User
	var totpEnabled int
	var totpSecret sql.NullString

	err := row.Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role,
		&totpSecret, &totpEnabled, &user.SessionVersion,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get user by id: %w", err)
	}

	user.TOTPSecret = totpSecret.String
	user.TOTPEnabled = totpEnabled == 1
	return &user, nil
}

func (s *sqlUserStore) GetByEmail(ctx context.Context, email string) (*User, error) {
	query := `
	SELECT id, email, password_hash, role, totp_secret, totp_enabled, session_version, created_at, updated_at
	FROM users WHERE email = ? COLLATE NOCASE`

	row := s.db.QueryRowContext(ctx, query, email)

	var user User
	var totpEnabled int
	var totpSecret sql.NullString

	err := row.Scan(
		&user.ID, &user.Email, &user.PasswordHash, &user.Role,
		&totpSecret, &totpEnabled, &user.SessionVersion,
		&user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: failed to get user by email: %w", err)
	}

	user.TOTPSecret = totpSecret.String
	user.TOTPEnabled = totpEnabled == 1
	return &user, nil
}

func (s *sqlUserStore) UpdatePassword(ctx context.Context, id string, passwordHash string, bumpSession bool) error {
	now := time.Now().UTC()
	var query string
	if bumpSession {
		query = `UPDATE users SET password_hash = ?, session_version = session_version + 1, updated_at = ? WHERE id = ?`
	} else {
		query = `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`
	}

	res, err := s.db.ExecContext(ctx, query, passwordHash, now, id)
	if err != nil {
		return fmt.Errorf("store: failed to update user password: %w", err)
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

func (s *sqlUserStore) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: failed to count users: %w", err)
	}
	return count, nil
}

func (s *sqlUserStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM users WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("store: failed to delete user: %w", err)
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
