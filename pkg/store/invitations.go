package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type sqlInvitationStore struct {
	db dbExecutor
}

func (s *sqlInvitationStore) Create(ctx context.Context, inv *TeamInvitation) error {
	if inv.ID == "" {
		return errors.New("store: invitation id cannot be empty")
	}
	if inv.OrgID == "" {
		inv.OrgID = "org_default"
	}
	if inv.Email == "" {
		return errors.New("store: invitation email cannot be empty")
	}
	if inv.Role == "" {
		inv.Role = "developer"
	}
	if inv.TokenHash == "" {
		return errors.New("store: invitation token hash cannot be empty")
	}
	if inv.ExpiresAt.IsZero() {
		inv.ExpiresAt = time.Now().UTC().Add(7 * 24 * time.Hour) // 7 days default
	}
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = time.Now().UTC()
	}

	query := `
		INSERT INTO team_invitations (id, org_id, email, role, token_hash, invited_by, expires_at, accepted_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		inv.ID, inv.OrgID, inv.Email, inv.Role, inv.TokenHash, inv.InvitedBy,
		inv.ExpiresAt, inv.AcceptedAt, inv.CreatedAt,
	)
	if err != nil {
		if IsDuplicateKeyError(err) {
			return ErrDuplicateKey
		}
		return err
	}
	return nil
}

func (s *sqlInvitationStore) GetByID(ctx context.Context, id string) (*TeamInvitation, error) {
	query := `
		SELECT id, org_id, email, role, token_hash, invited_by, expires_at, accepted_at, created_at
		FROM team_invitations
		WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)
	return scanInvitation(row)
}

func (s *sqlInvitationStore) GetByTokenHash(ctx context.Context, tokenHash string) (*TeamInvitation, error) {
	query := `
		SELECT id, org_id, email, role, token_hash, invited_by, expires_at, accepted_at, created_at
		FROM team_invitations
		WHERE token_hash = ?
	`
	row := s.db.QueryRowContext(ctx, query, tokenHash)
	return scanInvitation(row)
}

func (s *sqlInvitationStore) ListByOrg(ctx context.Context, orgID string) ([]*TeamInvitation, error) {
	if orgID == "" {
		orgID = "org_default"
	}
	query := `
		SELECT id, org_id, email, role, token_hash, invited_by, expires_at, accepted_at, created_at
		FROM team_invitations
		WHERE org_id = ?
		ORDER BY created_at DESC
	`
	rows, err := s.db.QueryContext(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]*TeamInvitation, 0)
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, inv)
	}
	return result, rows.Err()
}

func (s *sqlInvitationStore) MarkAccepted(ctx context.Context, id string, acceptedAt time.Time) error {
	query := `UPDATE team_invitations SET accepted_at = ? WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, acceptedAt, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlInvitationStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM team_invitations WHERE id = ?`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInvitation(s rowScanner) (*TeamInvitation, error) {
	var inv TeamInvitation
	var acceptedAt sql.NullTime
	err := s.Scan(
		&inv.ID, &inv.OrgID, &inv.Email, &inv.Role, &inv.TokenHash,
		&inv.InvitedBy, &inv.ExpiresAt, &acceptedAt, &inv.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if acceptedAt.Valid {
		inv.AcceptedAt = &acceptedAt.Time
	}
	return &inv, nil
}
