package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/store"
)

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st
}

func TestStore_MigrationsAndPing(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	if err := st.Ping(ctx); err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestStore_Organizations(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	org := &store.Organization{
		Name: "Acme Corp",
		Slug: "acme-corp",
	}

	if err := st.Organizations().Create(ctx, org); err != nil {
		t.Fatalf("Failed to create org: %v", err)
	}
	if org.ID == "" {
		t.Fatalf("Expected org ID to be generated")
	}

	// Fetch by ID
	fetched, err := st.Organizations().GetByID(ctx, org.ID)
	if err != nil {
		t.Fatalf("Failed to get org by id: %v", err)
	}
	if fetched.Slug != "acme-corp" {
		t.Errorf("Expected slug 'acme-corp', got %q", fetched.Slug)
	}

	// Fetch by Slug
	bySlug, err := st.Organizations().GetBySlug(ctx, "acme-corp")
	if err != nil {
		t.Fatalf("Failed to get org by slug: %v", err)
	}
	if bySlug.ID != org.ID {
		t.Errorf("Expected ID %s, got %s", org.ID, bySlug.ID)
	}

	// List
	list, err := st.Organizations().List(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("Expected 1 org in list, got %d (err: %v)", len(list), err)
	}

	// Delete
	if err := st.Organizations().Delete(ctx, org.ID); err != nil {
		t.Fatalf("Failed to delete org: %v", err)
	}

	_, err = st.Organizations().GetByID(ctx, org.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Expected ErrNotFound after delete, got %v", err)
	}
}

func TestStore_UsersAndSessions(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	count, err := st.Users().Count(ctx)
	if err != nil || count != 0 {
		t.Fatalf("Expected 0 users initially, got %d (err: %v)", count, err)
	}

	user := &store.User{
		Email:        "admin@pikpik.io",
		PasswordHash: "$argon2id$v=19$...",
		Role:         "owner",
	}

	if err := st.Users().Create(ctx, user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	count, err = st.Users().Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("Expected 1 user, got %d", count)
	}

	// Get by Email (case-insensitive)
	byEmail, err := st.Users().GetByEmail(ctx, "ADMIN@PIKPIK.IO")
	if err != nil {
		t.Fatalf("Failed to get user by email: %v", err)
	}
	if byEmail.ID != user.ID {
		t.Errorf("Expected user ID %s, got %s", user.ID, byEmail.ID)
	}

	// Update Password
	newHash := "$argon2id$new_hash"
	if err := st.Users().UpdatePassword(ctx, user.ID, newHash, true); err != nil {
		t.Fatalf("Failed to update password: %v", err)
	}
	updatedUser, _ := st.Users().GetByID(ctx, user.ID)
	if updatedUser.PasswordHash != newHash || updatedUser.SessionVersion != 2 {
		t.Errorf("Password update verification failed: %+v", updatedUser)
	}

	// Session
	sess := &store.Session{
		ID:        "sess_hash_123",
		UserID:    user.ID,
		IPAddress: "127.0.0.1",
		UserAgent: "TestAgent/1.0",
		ExpiresAt: time.Now().UTC().Add(1 * time.Hour),
	}
	if err := st.Sessions().Create(ctx, sess); err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	fetchedSess, err := st.Sessions().GetByID(ctx, sess.ID)
	if err != nil || fetchedSess.UserID != user.ID {
		t.Fatalf("Failed to get session: %v", err)
	}

	if err := st.Sessions().Delete(ctx, sess.ID); err != nil {
		t.Fatalf("Failed to delete session: %v", err)
	}
}

func TestStore_APITokens(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	user := &store.User{Email: "tok_user@pikpik.io", PasswordHash: "hash"}
	_ = st.Users().Create(ctx, user)

	tok := &store.APIToken{
		UserID:    user.ID,
		Name:      "CI/CD Token",
		Prefix:    "pik_live_ab12",
		TokenHash: "sha256_hash_value_12345",
		Scopes:    []string{"deploy:write", "services:read"},
	}

	if err := st.APITokens().Create(ctx, tok); err != nil {
		t.Fatalf("Failed to create api token: %v", err)
	}

	byHash, err := st.APITokens().GetByHash(ctx, tok.TokenHash)
	if err != nil {
		t.Fatalf("Failed to get token by hash: %v", err)
	}
	if len(byHash.Scopes) != 2 || byHash.Scopes[0] != "deploy:write" {
		t.Errorf("Unexpected token scopes: %v", byHash.Scopes)
	}

	if err := st.APITokens().TouchLastUsed(ctx, tok.ID); err != nil {
		t.Fatalf("Failed to touch last used: %v", err)
	}

	list, err := st.APITokens().ListByUser(ctx, user.ID)
	if err != nil || len(list) != 1 {
		t.Fatalf("Expected 1 token for user, got %d (err: %v)", len(list), err)
	}
}

func TestStore_ProjectStageServiceAndEnvVars(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	org := &store.Organization{Name: "Org 1", Slug: "org-1"}
	_ = st.Organizations().Create(ctx, org)

	prj := &store.Project{OrgID: org.ID, Name: "E-Commerce", Slug: "e-comm"}
	if err := st.Projects().Create(ctx, prj); err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}

	stage := &store.Stage{ProjectID: prj.ID, Name: "Production", Slug: "production"}
	if err := st.Stages().Create(ctx, stage); err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}

	svc := &store.Service{
		ProjectID:     prj.ID,
		StageID:       stage.ID,
		Name:          "Auth API",
		Slug:          "auth-api",
		Type:          "app",
		Image:         "pikpik/auth:v1.0",
		ContainerPort: 8080,
		DomainNames:   []string{"auth.pikpik.io"},
	}
	if err := st.Services().Create(ctx, svc); err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	bySlug, err := st.Services().GetBySlug(ctx, prj.ID, stage.ID, "auth-api")
	if err != nil || bySlug.ID != svc.ID {
		t.Fatalf("Failed to get service by slug: %v", err)
	}

	// Env Vars (Upsert test)
	envVar := &store.EnvVar{
		ScopeTier:      store.TierService,
		ResourceID:     svc.ID,
		Key:            "DB_PASSWORD",
		ValueEncrypted: "v1:aaa:bbb:ccc",
		IsSecret:       true,
	}
	if err := st.EnvVars().Set(ctx, envVar); err != nil {
		t.Fatalf("Failed to set env var: %v", err)
	}

	// Update the same env var
	envVar.ValueEncrypted = "v1:xxx:yyy:zzz"
	if err := st.EnvVars().Set(ctx, envVar); err != nil {
		t.Fatalf("Failed to upsert env var: %v", err)
	}

	fetchedEnv, err := st.EnvVars().Get(ctx, store.TierService, svc.ID, "DB_PASSWORD")
	if err != nil {
		t.Fatalf("Failed to get env var: %v", err)
	}
	if fetchedEnv.ValueEncrypted != "v1:xxx:yyy:zzz" {
		t.Errorf("Expected updated value 'v1:xxx:yyy:zzz', got %q", fetchedEnv.ValueEncrypted)
	}
}

func TestStore_WithTx(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	org := &store.Organization{Name: "Tx Org", Slug: "tx-org"}

	// Test rollback on error
	err := st.WithTx(ctx, func(tx store.Store) error {
		if err := tx.Organizations().Create(ctx, org); err != nil {
			return err
		}
		return errors.New("simulated failure")
	})
	if err == nil {
		t.Fatalf("Expected transaction error, got nil")
	}

	// Verify org was not committed
	_, err = st.Organizations().GetBySlug(ctx, "tx-org")
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("Expected ErrNotFound after rollback, got: %v", err)
	}

	// Test successful commit
	err = st.WithTx(ctx, func(tx store.Store) error {
		return tx.Organizations().Create(ctx, org)
	})
	if err != nil {
		t.Fatalf("Expected successful tx commit, got: %v", err)
	}

	found, err := st.Organizations().GetBySlug(ctx, "tx-org")
	if err != nil || found.Name != "Tx Org" {
		t.Fatalf("Expected org to be found after commit: %v", err)
	}
}
