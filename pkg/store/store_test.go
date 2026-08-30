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

func TestStore_BackupSchedules(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	// 1. Setup required hierarchy
	org := &store.Organization{Name: "Org 1", Slug: "org-1"}
	if err := st.Organizations().Create(ctx, org); err != nil {
		t.Fatalf("Failed to create org: %v", err)
	}
	proj := &store.Project{OrgID: org.ID, Name: "Proj 1", Slug: "proj-1"}
	if err := st.Projects().Create(ctx, proj); err != nil {
		t.Fatalf("Failed to create project: %v", err)
	}
	stage := &store.Stage{ProjectID: proj.ID, Name: "Prod", Slug: "prod"}
	if err := st.Stages().Create(ctx, stage); err != nil {
		t.Fatalf("Failed to create stage: %v", err)
	}
	svc := &store.Service{
		ProjectID: proj.ID,
		StageID:   stage.ID,
		Name:      "Main DB",
		Slug:      "main-db",
		Type:      "database",
		Image:     "postgres:17-alpine",
	}
	if err := st.Services().Create(ctx, svc); err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// 2. Create Backup Schedule
	now := time.Now().UTC().Truncate(time.Second)
	nextRun := now.Add(1 * time.Hour)
	sch := &store.BackupSchedule{
		ServiceID:         svc.ID,
		CronExpr:          "0 * * * *",
		Engine:            "postgres:17",
		DatabaseName:      "production_db",
		Username:          "pguser",
		PasswordEncrypted: "encrypted_pass",
		S3Bucket:          "backups-bucket",
		S3Endpoint:        "https://r2.cloudflarestorage.com",
		S3Region:          "auto",
		RetentionHourly:   24,
		RetentionDaily:    7,
		RetentionWeekly:   4,
		RetentionMonthly:  12,
		MaxBackups:        50,
		Compression:       "gzip",
		IsEnabled:         true,
		NextRunAt:         &nextRun,
	}

	if err := st.Schedules().Create(ctx, sch); err != nil {
		t.Fatalf("Failed to create schedule: %v", err)
	}
	if sch.ID == "" {
		t.Fatalf("Expected generated ID for schedule")
	}

	// 3. GetByID
	fetched, err := st.Schedules().GetByID(ctx, sch.ID)
	if err != nil {
		t.Fatalf("Failed to get schedule by ID: %v", err)
	}
	if fetched.DatabaseName != "production_db" || !fetched.IsEnabled || fetched.NextRunAt == nil {
		t.Fatalf("Fetched schedule does not match: %+v", fetched)
	}

	// 4. ListByService
	list, err := st.Schedules().ListByService(ctx, svc.ID)
	if err != nil {
		t.Fatalf("Failed to list schedules by service: %v", err)
	}
	if len(list) != 1 || list[0].ID != sch.ID {
		t.Fatalf("Expected 1 schedule in list, got %d", len(list))
	}

	// 5. ListActive
	activeList, err := st.Schedules().ListActive(ctx)
	if err != nil {
		t.Fatalf("Failed to list active schedules: %v", err)
	}
	if len(activeList) != 1 {
		t.Fatalf("Expected 1 active schedule, got %d", len(activeList))
	}

	// 6. ListDue (NextRunAt in future should not be returned for now)
	dueList, err := st.Schedules().ListDue(ctx, now)
	if err != nil {
		t.Fatalf("Failed to list due schedules: %v", err)
	}
	if len(dueList) != 0 {
		t.Fatalf("Expected 0 due schedules before nextRun, got %d", len(dueList))
	}

	// ListDue with future time should return schedule
	dueListFuture, err := st.Schedules().ListDue(ctx, nextRun.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("Failed to list due schedules in future: %v", err)
	}
	if len(dueListFuture) != 1 {
		t.Fatalf("Expected 1 due schedule in future, got %d", len(dueListFuture))
	}

	// 7. UpdateRunTimes
	lastRun := now
	newNextRun := nextRun.Add(1 * time.Hour)
	if err := st.Schedules().UpdateRunTimes(ctx, sch.ID, lastRun, newNextRun); err != nil {
		t.Fatalf("Failed to update run times: %v", err)
	}

	updated, err := st.Schedules().GetByID(ctx, sch.ID)
	if err != nil {
		t.Fatalf("Failed to get updated schedule: %v", err)
	}
	if updated.LastRunAt == nil || updated.NextRunAt == nil {
		t.Fatalf("Expected non-nil LastRunAt and NextRunAt")
	}

	// 8. Update
	updated.CronExpr = "0 0 * * *"
	updated.RetentionDaily = 14
	if err := st.Schedules().Update(ctx, updated); err != nil {
		t.Fatalf("Failed to update schedule: %v", err)
	}

	reFetched, err := st.Schedules().GetByID(ctx, sch.ID)
	if err != nil {
		t.Fatalf("Failed to get refetched schedule: %v", err)
	}
	if reFetched.CronExpr != "0 0 * * *" || reFetched.RetentionDaily != 14 {
		t.Fatalf("Update not reflected: %+v", reFetched)
	}

	// 9. Delete
	if err := st.Schedules().Delete(ctx, sch.ID); err != nil {
		t.Fatalf("Failed to delete schedule: %v", err)
	}
	_, err = st.Schedules().GetByID(ctx, sch.ID)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Expected ErrNotFound after deletion, got: %v", err)
	}
}

