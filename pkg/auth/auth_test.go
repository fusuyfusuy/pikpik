package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/crypto"
	"github.com/fusuycorp/pikpik/pkg/store"
)

func newTestAuth(t *testing.T) (auth.AuthService, store.Store) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "auth_test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})

	hasher := crypto.FastArgon2Hasher()
	svc := auth.NewAuthService(st, hasher)
	return svc, st
}

func TestAuth_BootstrapAndAuthenticate(t *testing.T) {
	svc, _ := newTestAuth(t)
	ctx := context.Background()

	adminEmail := "owner@pikpik.io"
	adminPass := "SuperSecretPassword123!"

	// 1. Bootstrap Admin
	user, err := svc.BootstrapAdmin(ctx, adminEmail, adminPass)
	if err != nil {
		t.Fatalf("BootstrapAdmin failed: %v", err)
	}
	if user.Email != adminEmail || user.Role != "owner" {
		t.Errorf("Unexpected admin user: %+v", user)
	}

	// 2. Prevent secondary bootstrap
	_, err = svc.BootstrapAdmin(ctx, "another@pikpik.io", "pass")
	if !errors.Is(err, auth.ErrAdminAlreadyExists) {
		t.Errorf("Expected ErrAdminAlreadyExists, got %v", err)
	}

	// 3. Authenticate with correct password
	authedUser, err := svc.AuthenticateUser(ctx, adminEmail, adminPass)
	if err != nil {
		t.Fatalf("AuthenticateUser failed: %v", err)
	}
	if authedUser.ID != user.ID {
		t.Errorf("User ID mismatch: got %s, want %s", authedUser.ID, user.ID)
	}

	// 4. Authenticate with wrong password
	_, err = svc.AuthenticateUser(ctx, adminEmail, "WrongPassword")
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("Expected ErrInvalidCredentials for wrong password, got %v", err)
	}

	// 5. Authenticate non-existent user
	_, err = svc.AuthenticateUser(ctx, "nonexistent@pikpik.io", adminPass)
	if !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("Expected ErrInvalidCredentials for non-existent user, got %v", err)
	}
}

func TestAuth_APITokenLifecycle(t *testing.T) {
	svc, _ := newTestAuth(t)
	ctx := context.Background()

	user, err := svc.BootstrapAdmin(ctx, "admin@pikpik.io", "password123")
	if err != nil {
		t.Fatalf("Failed to bootstrap admin: %v", err)
	}

	// 1. Create Token with scopes
	scopes := []string{"deploy:write", "project:read"}
	genTok, err := svc.CreateAPIToken(ctx, user.ID, "GitHub Actions", scopes, nil)
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}

	if genTok.RawSecret == "" || len(genTok.RawSecret) < 40 {
		t.Errorf("Invalid raw secret length: %d", len(genTok.RawSecret))
	}
	if genTok.Token.Prefix != genTok.RawSecret[:12] {
		t.Errorf("Prefix mismatch: got %s, want %s", genTok.Token.Prefix, genTok.RawSecret[:12])
	}

	// 2. Validate token with matching scope
	tok, err := svc.ValidateAPIToken(ctx, genTok.RawSecret, "deploy:write")
	if err != nil {
		t.Fatalf("ValidateAPIToken failed: %v", err)
	}
	if tok.ID != genTok.Token.ID {
		t.Errorf("Token ID mismatch: got %s, want %s", tok.ID, genTok.Token.ID)
	}

	// 3. Validate token with unauthorized scope
	_, err = svc.ValidateAPIToken(ctx, genTok.RawSecret, "admin:write")
	if !errors.Is(err, auth.ErrInsufficientScope) {
		t.Errorf("Expected ErrInsufficientScope, got %v", err)
	}

	// 4. Validate non-existent token
	_, err = svc.ValidateAPIToken(ctx, "pik_live_invalid_secret_token_1234567890", "deploy:write")
	if !errors.Is(err, auth.ErrTokenNotFound) {
		t.Errorf("Expected ErrTokenNotFound, got %v", err)
	}
}

func TestAuth_APITokenExpiration(t *testing.T) {
	svc, _ := newTestAuth(t)
	ctx := context.Background()

	user, _ := svc.BootstrapAdmin(ctx, "admin@pikpik.io", "password123")

	past := time.Now().UTC().Add(-1 * time.Hour)
	genTok, err := svc.CreateAPIToken(ctx, user.ID, "Expired Token", []string{"deploy:write"}, &past)
	if err != nil {
		t.Fatalf("CreateAPIToken failed: %v", err)
	}

	_, err = svc.ValidateAPIToken(ctx, genTok.RawSecret, "deploy:write")
	if !errors.Is(err, auth.ErrTokenExpired) {
		t.Errorf("Expected ErrTokenExpired, got %v", err)
	}
}

func TestAuth_ScopeMatching(t *testing.T) {
	tests := []struct {
		name     string
		scopes   []string
		required string
		expected bool
	}{
		{"exact match", []string{"deploy:write"}, "deploy:write", true},
		{"wildcard admin:*", []string{"admin:*"}, "deploy:write", true},
		{"global wildcard *", []string{"*"}, "anything:anything", true},
		{"resource wildcard project:*", []string{"project:*"}, "project:read", true},
		{"resource wildcard project:* mismatch", []string{"project:*"}, "service:read", false},
		{"no match", []string{"service:read"}, "service:write", false},
		{"empty required", []string{"service:read"}, "", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := auth.HasScope(tc.scopes, tc.required)
			if got != tc.expected {
				t.Errorf("HasScope(%v, %q) = %v, want %v", tc.scopes, tc.required, got, tc.expected)
			}
		})
	}
}
