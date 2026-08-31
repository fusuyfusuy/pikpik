package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/crypto"
	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/fusuycorp/pikpik/pkg/templates"
)

// TestParcel1_ST1_SecretEncryption verifies ST-1: secret write paths are encrypted with v1: prefix.
func TestParcel1_ST1_SecretEncryption(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	vault, err := crypto.NewAESVault("master-secret-for-testing-vault-32b!")
	if err != nil {
		t.Fatalf("failed to create vault: %v", err)
	}

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
		Vault: vault,
	})

	// 1. CreateApp with secrets in Env
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:  "test-secret-app",
		Image: "nginx:alpine",
		Env: map[string]string{
			"API_SECRET_KEY": "supersecretkey123",
			"PORT":           "8080",
		},
	})
	if err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	// Verify env vars in store
	storedVars, err := st.EnvVars().ListByResource(ctx, store.TierService, app.ID)
	if err != nil || len(storedVars) != 2 {
		t.Fatalf("expected 2 stored env vars, got %d (err: %v)", len(storedVars), err)
	}

	for _, v := range storedVars {
		if v.Key == "API_SECRET_KEY" {
			if !v.IsSecret {
				t.Errorf("expected IsSecret=true for API_SECRET_KEY")
			}
			if !strings.HasPrefix(v.ValueEncrypted, "v1:") {
				t.Errorf("expected ValueEncrypted to start with v1:, got %s", v.ValueEncrypted)
			}
			decrypted, err := vault.DecryptString(ctx, v.ValueEncrypted)
			if err != nil || decrypted != "supersecretkey123" {
				t.Errorf("failed to decrypt API_SECRET_KEY: %v, got %s", err, decrypted)
			}
		} else if v.Key == "PORT" {
			if v.IsSecret {
				t.Errorf("expected IsSecret=false for PORT")
			}
			if v.ValueEncrypted != "8080" {
				t.Errorf("expected plaintext PORT 8080, got %s", v.ValueEncrypted)
			}
		}
	}

	// 2. GetAppEnv decrypts secret values
	appEnv, err := ctrl.GetAppEnv(ctx, app.ID)
	if err != nil {
		t.Fatalf("GetAppEnv failed: %v", err)
	}
	if appEnv["API_SECRET_KEY"] != "supersecretkey123" {
		t.Errorf("expected decrypted secret in GetAppEnv, got %s", appEnv["API_SECRET_KEY"])
	}
	if appEnv["PORT"] != "8080" {
		t.Errorf("expected PORT in GetAppEnv, got %s", appEnv["PORT"])
	}

	// 3. SetAppEnv encrypts new secrets
	err = ctrl.SetAppEnv(ctx, app.ID, map[string]string{
		"DB_PASSWORD": "mypassword456",
		"APP_ENV":     "production",
	})
	if err != nil {
		t.Fatalf("SetAppEnv failed: %v", err)
	}

	storedVars, _ = st.EnvVars().ListByResource(ctx, store.TierService, app.ID)
	for _, v := range storedVars {
		if v.Key == "DB_PASSWORD" {
			if !strings.HasPrefix(v.ValueEncrypted, "v1:") {
				t.Errorf("expected v1: prefix on SetAppEnv secret, got %s", v.ValueEncrypted)
			}
			decrypted, err := vault.DecryptString(ctx, v.ValueEncrypted)
			if err != nil || decrypted != "mypassword456" {
				t.Errorf("failed to decrypt DB_PASSWORD: %v", err)
			}
		}
	}

	// 4. CreateDatabase encrypts DB_PASSWORD
	db, err := ctrl.CreateDatabase(ctx, &api.CreateDatabaseRequest{
		Name:     "app-postgres",
		Engine:   "postgres",
		Password: "dbmasterpassword999",
	})
	if err != nil {
		t.Fatalf("CreateDatabase failed: %v", err)
	}

	dbVars, err := st.EnvVars().ListByResource(ctx, store.TierService, db.ID)
	if err != nil || len(dbVars) == 0 {
		t.Fatalf("expected DB_PASSWORD in env_vars for db: %v", err)
	}
	for _, v := range dbVars {
		if v.Key == "DB_PASSWORD" {
			if !strings.HasPrefix(v.ValueEncrypted, "v1:") {
				t.Errorf("expected v1: prefix on DB_PASSWORD, got %s", v.ValueEncrypted)
			}
			decrypted, err := vault.DecryptString(ctx, v.ValueEncrypted)
			if err != nil || decrypted != "dbmasterpassword999" {
				t.Errorf("failed to decrypt DB_PASSWORD: %v, got %s", err, decrypted)
			}
		}
	}

	// 5. CreateBackupSchedule encrypts password and s3 secret key
	sch, err := ctrl.CreateBackupSchedule(ctx, &api.CreateBackupScheduleRequest{
		ServiceID:   db.ID,
		CronExpr:    "0 2 * * *",
		Engine:      "postgres",
		Password:    "scheduledbpass",
		S3SecretKey: "s3supersecretkey",
	})
	if err != nil {
		t.Fatalf("CreateBackupSchedule failed: %v", err)
	}
	if !strings.HasPrefix(sch.PasswordEncrypted, "v1:") {
		t.Errorf("expected v1: prefix on sch.PasswordEncrypted, got %s", sch.PasswordEncrypted)
	}
	if !strings.HasPrefix(sch.S3SecretKeyEncrypted, "v1:") {
		t.Errorf("expected v1: prefix on sch.S3SecretKeyEncrypted, got %s", sch.S3SecretKeyEncrypted)
	}

	// 6. UpdateBackupSchedule encrypts updated credentials
	updatedSch, err := ctrl.UpdateBackupSchedule(ctx, sch.ID, &api.UpdateBackupScheduleRequest{
		Password:    "newscheduledbpass",
		S3SecretKey: "news3secretkey",
	})
	if err != nil {
		t.Fatalf("UpdateBackupSchedule failed: %v", err)
	}
	if !strings.HasPrefix(updatedSch.PasswordEncrypted, "v1:") {
		t.Errorf("expected v1: prefix on updatedSch.PasswordEncrypted, got %s", updatedSch.PasswordEncrypted)
	}
	decPass, _ := vault.DecryptString(ctx, updatedSch.PasswordEncrypted)
	if decPass != "newscheduledbpass" {
		t.Errorf("expected decrypted password 'newscheduledbpass', got %s", decPass)
	}
}

// TestParcel1_ST2_AuditLogging verifies ST-2: audit logs are recorded on mutating actions.
func TestParcel1_ST2_AuditLogging(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	hasher := crypto.FastArgon2Hasher()
	authSvc := auth.NewAuthService(st, hasher)

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store:       st,
		AuthService: authSvc,
	})

	// Perform mutating operations
	org, err := ctrl.CreateOrganization(ctx, &api.CreateOrgRequest{Name: "Audit Org"})
	if err != nil {
		t.Fatalf("CreateOrganization failed: %v", err)
	}

	prj, err := ctrl.CreateProject(ctx, &api.CreateProjectRequest{OrgID: org.ID, Name: "Audit Project"})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	_, _ = ctrl.UpdateProject(ctx, prj.ID, &api.UpdateProjectRequest{Name: "Updated Project"})

	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{ProjectID: prj.ID, Name: "Audit App", Image: "nginx:latest"})
	if err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	_ = ctrl.SetAppEnv(ctx, app.ID, map[string]string{"KEY": "VAL"})
	_ = ctrl.RestartApp(ctx, app.ID)
	_ = ctrl.StopApp(ctx, app.ID)
	_ = ctrl.StartApp(ctx, app.ID)
	_ = ctrl.DeleteApp(ctx, app.ID)

	stack, _ := ctrl.CreateStack(ctx, &api.CreateStackRequest{ProjectID: prj.ID, Name: "audit-stack", ComposeYAML: "version: '3.8'"})
	if stack != nil {
		_ = ctrl.DeleteStack(ctx, stack.ID)
	}

	net, _ := ctrl.CreateNetwork(ctx, &api.CreateNetworkRequest{ProjectID: prj.ID, Name: "audit-net"})
	if net != nil {
		_ = ctrl.DeleteNetwork(ctx, net.ID)
	}

	vol, _ := ctrl.CreateVolume(ctx, &api.CreateVolumeRequest{ProjectID: prj.ID, Name: "audit-vol"})
	if vol != nil {
		_ = ctrl.DeleteVolume(ctx, vol.ID)
	}

	db, _ := ctrl.CreateDatabase(ctx, &api.CreateDatabaseRequest{Name: "audit-db", Engine: "postgres"})
	if db != nil {
		_ = ctrl.DeleteDatabase(ctx, db.ID)
	}

	_ = ctrl.DeleteProject(ctx, prj.ID)

	// Verify audit logs were written
	rows, err := st.DB().QueryContext(ctx, "SELECT action FROM audit_logs")
	if err != nil {
		t.Fatalf("Querying audit_logs failed: %v", err)
	}
	defer rows.Close()

	var actions []string
	for rows.Next() {
		var act string
		if err := rows.Scan(&act); err == nil {
			actions = append(actions, act)
		}
	}
	if len(actions) < 10 {
		t.Fatalf("expected at least 10 audit log entries, got %d", len(actions))
	}

	actionSet := make(map[string]bool)
	for _, act := range actions {
		actionSet[act] = true
	}

	expectedActions := []string{
		"org:create",
		"project:create",
		"project:update",
		"project:delete",
		"app:create",
		"app:set_env",
		"app:restart",
		"app:stop",
		"app:start",
		"app:delete",
		"stack:create",
		"stack:delete",
		"network:create",
		"network:delete",
		"volume:create",
		"volume:delete",
		"database:create",
		"database:delete",
	}

	for _, act := range expectedActions {
		if !actionSet[act] {
			t.Errorf("missing expected audit action: %s", act)
		}
	}
}

// TestParcel1_ST3_LoginTokenExpiration verifies ST-3: Login mints token with 30-day expiration.
func TestParcel1_ST3_LoginTokenExpiration(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	hasher := crypto.FastArgon2Hasher()
	authSvc := auth.NewAuthService(st, hasher)

	// Bootstrap root admin
	_, err = authSvc.BootstrapAdmin(ctx, "admin@example.com", "SecurePassword123!")
	if err != nil {
		t.Fatalf("bootstrap root user failed: %v", err)
	}

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store:       st,
		AuthService: authSvc,
	})

	loginResp, err := ctrl.Login(ctx, "admin@example.com", "SecurePassword123!")
	if err != nil {
		t.Fatalf("Login failed: %v", err)
	}

	if loginResp.Token == "" {
		t.Fatalf("expected non-empty token")
	}

	// Verify ExpiresAt is ~30 days in future
	timeUntilExpiry := time.Until(loginResp.ExpiresAt)
	if timeUntilExpiry < 29*24*time.Hour || timeUntilExpiry > 31*24*time.Hour {
		t.Errorf("expected ~30 day token expiration, got duration: %v (expires at %v)", timeUntilExpiry, loginResp.ExpiresAt)
	}
}

// TestParcel1_ST4_AuthMiddleware_FailClosed verifies ST-4: AuthMiddleware fails closed on user-lookup error.
func TestParcel1_ST4_AuthMiddleware_FailClosed(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	hasher := crypto.FastArgon2Hasher()
	authSvc := auth.NewAuthService(st, hasher)

	// 1. Create a user and token
	user, err := authSvc.BootstrapAdmin(ctx, "operator@example.com", "OperatorPass123!")
	if err != nil {
		t.Fatalf("bootstrap user failed: %v", err)
	}

	tokGen, err := authSvc.CreateAPIToken(ctx, user.ID, "Test Token", []string{"*"}, nil)
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}

	mw := api.AuthMiddleware(authSvc, st, api.RoleDeveloper)
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Test with valid token and existing user -> 200 OK
	req := httptest.NewRequest("GET", "/api/v1/apps", nil)
	req.Header.Set("Authorization", "Bearer "+tokGen.RawSecret)
	rec := httptest.NewRecorder()
	mw(dummyHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for valid user token, got %d: %s", rec.Code, rec.Body.String())
	}

	// Delete user from DB directly (simulating orphaned API token / deleted user)
	_ = st.Users().Delete(ctx, user.ID)

	// Test with valid token but non-existent/deleted user -> MUST FAIL CLOSED (401 Unauthorized, never RoleAdmin / 200)
	req = httptest.NewRequest("GET", "/api/v1/apps", nil)
	req.Header.Set("Authorization", "Bearer "+tokGen.RawSecret)
	rec = httptest.NewRecorder()
	mw(dummyHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY VIOLATION: expected 401 Unauthorized for token with deleted user, got %d: %s", rec.Code, rec.Body.String())
	}

	// Test with invalid token -> 401 Unauthorized
	req = httptest.NewRequest("GET", "/api/v1/apps", nil)
	req.Header.Set("Authorization", "Bearer pik_live_invalidtokensecret")
	rec = httptest.NewRecorder()
	mw(dummyHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for invalid token, got %d", rec.Code)
	}

	// Test with missing token -> 401 Unauthorized
	req = httptest.NewRequest("GET", "/api/v1/apps", nil)
	rec = httptest.NewRecorder()
	mw(dummyHandler).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing token, got %d", rec.Code)
	}
}

// TestParcel1_ST6_DeleteByResource_Controller verifies ST-6: resource deletion cleans up environment variables.
func TestParcel1_ST6_DeleteByResource_Controller(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
	})

	// 1. Create App with Env
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:  "delete-cleanup-app",
		Image: "nginx:latest",
		Env: map[string]string{
			"VAR1": "VAL1",
			"VAR2": "VAL2",
		},
	})
	if err != nil {
		t.Fatalf("CreateApp failed: %v", err)
	}

	// Verify env vars exist
	envs, _ := st.EnvVars().ListByResource(ctx, store.TierService, app.ID)
	if len(envs) != 2 {
		t.Fatalf("expected 2 env vars before deletion, got %d", len(envs))
	}

	// Delete App
	err = ctrl.DeleteApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("DeleteApp failed: %v", err)
	}

	// Verify env vars were purged
	envs, _ = st.EnvVars().ListByResource(ctx, store.TierService, app.ID)
	if len(envs) != 0 {
		t.Errorf("expected 0 env vars after DeleteApp, got %d (orphaned env vars)", len(envs))
	}

	// 2. Create Project and Project Env Vars
	prj, err := ctrl.CreateProject(ctx, &api.CreateProjectRequest{
		Name: "delete-cleanup-project",
	})
	if err != nil {
		t.Fatalf("CreateProject failed: %v", err)
	}

	_ = st.EnvVars().Set(ctx, &store.EnvVar{
		ID:             store.NewID("env"),
		ScopeTier:      store.TierProject,
		ResourceID:     prj.ID,
		Key:            "PROJECT_VAR",
		ValueEncrypted: "val",
	})

	prjEnvs, _ := st.EnvVars().ListByResource(ctx, store.TierProject, prj.ID)
	if len(prjEnvs) != 1 {
		t.Fatalf("expected 1 project env var before deletion, got %d", len(prjEnvs))
	}

	// Delete Project
	err = ctrl.DeleteProject(ctx, prj.ID)
	if err != nil {
		t.Fatalf("DeleteProject failed: %v", err)
	}

	prjEnvs, _ = st.EnvVars().ListByResource(ctx, store.TierProject, prj.ID)
	if len(prjEnvs) != 0 {
		t.Errorf("expected 0 project env vars after DeleteProject, got %d", len(prjEnvs))
	}
}

// TestParcel1_TemplateDeployer_VaultPassThrough verifies deployer correctly passes vault.
func TestParcel1_TemplateDeployer_VaultPassThrough(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	vault, err := crypto.NewAESVault("master-secret-for-testing-vault-32b!")
	if err != nil {
		t.Fatalf("failed to create vault: %v", err)
	}

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
		Vault: vault,
	})

	resp, err := ctrl.DeployTemplate(ctx, "pocketbase", &templates.DeployTemplateRequest{
		Name: "pb-vault-test",
	})
	if err != nil {
		t.Fatalf("DeployTemplate failed: %v", err)
	}

	// Verify env vars created by deployer carry v1: encrypted secrets
	envs, err := st.EnvVars().ListByResource(ctx, store.TierService, resp.AppID)
	if err != nil || len(envs) == 0 {
		t.Fatalf("expected env vars for deployed template: %v", err)
	}

	foundSecret := false
	for _, env := range envs {
		if env.Key == "POCKETBASE_ENCRYPTION_KEY" {
			foundSecret = true
			if !strings.HasPrefix(env.ValueEncrypted, "v1:") {
				t.Errorf("expected v1: prefix on template secret, got %s", env.ValueEncrypted)
			}
			dec, err := vault.DecryptString(ctx, env.ValueEncrypted)
			if err != nil || len(dec) != 32 {
				t.Errorf("failed to decrypt template secret: %v, got %s", err, dec)
			}
		}
	}
	if !foundSecret {
		t.Errorf("expected POCKETBASE_ENCRYPTION_KEY in store env vars")
	}
}
