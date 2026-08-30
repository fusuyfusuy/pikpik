package auth

import (
	"context"
	"errors"
	"time"

	"github.com/fusuycorp/pikpik/pkg/store"
)

var (
	// ErrInvalidCredentials is returned when email or password verification fails.
	ErrInvalidCredentials = errors.New("auth: invalid email or password")

	// ErrTokenExpired is returned when an API token has passed its expiration time.
	ErrTokenExpired = errors.New("auth: api token has expired")

	// ErrTokenNotFound is returned when an API token is not recognized.
	ErrTokenNotFound = errors.New("auth: token not recognized")

	// ErrInsufficientScope is returned when an API token lacks the requested scope.
	ErrInsufficientScope = errors.New("auth: insufficient permissions for operation")

	// ErrAdminAlreadyExists is returned when bootstrapping an admin on a non-empty system.
	ErrAdminAlreadyExists = errors.New("auth: initial admin already configured")

	// ErrUserNotFound is returned when a user record is not found.
	ErrUserNotFound = errors.New("auth: user not found")
)

// GeneratedToken contains the persisted metadata and the one-time raw secret token string.
type GeneratedToken struct {
	Token     *store.APIToken `json:"token"`
	RawSecret string          `json:"raw_secret"` // Displayed only ONCE to user
}

// AuthService defines identity management, authentication, and API token operations.
type AuthService interface {
	// BootstrapAdmin creates the initial system owner if no users exist.
	BootstrapAdmin(ctx context.Context, email, password string) (*store.User, error)

	// AuthenticateUser verifies user credentials via Argon2id.
	AuthenticateUser(ctx context.Context, email, password string) (*store.User, error)

	// CreateAPIToken generates a new cryptographically secure pik_live_ token.
	CreateAPIToken(ctx context.Context, userID, name string, scopes []string, expiresAt *time.Time) (*GeneratedToken, error)

	// ValidateAPIToken performs constant-time lookup and checks expiration and scopes.
	ValidateAPIToken(ctx context.Context, rawSecret string, requiredScope string) (*store.APIToken, error)

	// HashPassword hashes a password string with Argon2id parameters.
	HashPassword(password string) (string, error)

	// VerifyPassword compares a plaintext password against an Argon2id PHC string.
	VerifyPassword(password, encodedHash string) (bool, error)
}
