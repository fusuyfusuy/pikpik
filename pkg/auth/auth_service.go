package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fusuycorp/pikpik/pkg/crypto"
	"github.com/fusuycorp/pikpik/pkg/store"
)

type authServiceImpl struct {
	store  store.Store
	hasher *crypto.Argon2Hasher
}

// NewAuthService creates a new AuthService instance.
func NewAuthService(st store.Store, hasher *crypto.Argon2Hasher) AuthService {
	if hasher == nil {
		hasher = crypto.DefaultArgon2Hasher()
	}
	return &authServiceImpl{
		store:  st,
		hasher: hasher,
	}
}

func (s *authServiceImpl) BootstrapAdmin(ctx context.Context, email, password string) (*store.User, error) {
	count, err := s.store.Users().Count(ctx)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to count existing users: %w", err)
	}
	if count > 0 {
		return nil, ErrAdminAlreadyExists
	}

	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("auth: failed to hash password: %w", err)
	}

	user := &store.User{
		ID:             store.NewID("usr"),
		Email:          email,
		PasswordHash:   hash,
		Role:           "owner",
		SessionVersion: 1,
	}

	if err := s.store.Users().Create(ctx, user); err != nil {
		return nil, fmt.Errorf("auth: failed to create admin user: %w", err)
	}

	return user, nil
}

func (s *authServiceImpl) AuthenticateUser(ctx context.Context, email, password string) (*store.User, error) {
	user, err := s.store.Users().GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("auth: failed to retrieve user: %w", err)
	}

	valid, err := s.hasher.Verify(password, user.PasswordHash)
	if err != nil || !valid {
		return nil, ErrInvalidCredentials
	}

	return user, nil
}

func (s *authServiceImpl) CreateAPIToken(
	ctx context.Context,
	userID, name string,
	scopes []string,
	expiresAt *time.Time,
) (*GeneratedToken, error) {
	// Verify user exists
	if _, err := s.store.Users().GetByID(ctx, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("auth: failed to find user for token: %w", err)
	}

	rawSecret, err := GenerateRawToken(DefaultTokenPrefix)
	if err != nil {
		return nil, err
	}

	tokenHash := HashToken(rawSecret)
	prefix := ExtractTokenPrefix(rawSecret)

	tok := &store.APIToken{
		ID:        store.NewID("tok"),
		UserID:    userID,
		Name:      name,
		Prefix:    prefix,
		TokenHash: tokenHash,
		Scopes:    scopes,
		ExpiresAt: expiresAt,
	}

	if err := s.store.APITokens().Create(ctx, tok); err != nil {
		return nil, fmt.Errorf("auth: failed to persist api token: %w", err)
	}

	return &GeneratedToken{
		Token:     tok,
		RawSecret: rawSecret,
	}, nil
}

func (s *authServiceImpl) ValidateAPIToken(
	ctx context.Context,
	rawSecret string,
	requiredScope string,
) (*store.APIToken, error) {
	tokenHash := HashToken(rawSecret)
	tok, err := s.store.APITokens().GetByHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrTokenNotFound
		}
		return nil, fmt.Errorf("auth: failed to query token: %w", err)
	}

	// Expiration check
	if tok.ExpiresAt != nil && time.Now().UTC().After(*tok.ExpiresAt) {
		return nil, ErrTokenExpired
	}

	// Scope verification
	if requiredScope != "" && !HasScope(tok.Scopes, requiredScope) {
		return nil, ErrInsufficientScope
	}

	// Update last_used_at asynchronously/inline
	_ = s.store.APITokens().TouchLastUsed(ctx, tok.ID)

	return tok, nil
}

func (s *authServiceImpl) HashPassword(password string) (string, error) {
	return s.hasher.Hash(password)
}

func (s *authServiceImpl) VerifyPassword(password, encodedHash string) (bool, error) {
	return s.hasher.Verify(password, encodedHash)
}
