package templates

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/client"
	"github.com/fusuycorp/pikpik/pkg/crypto"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
	"github.com/fusuycorp/pikpik/pkg/store"
)

func TestGenerateToken_Formats(t *testing.T) {
	// 1. hex_32
	hexToken, err := GenerateToken("hex_32")
	if err != nil {
		t.Fatalf("unexpected error generating hex_32: %v", err)
	}
	if len(hexToken) != 32 {
		t.Errorf("expected 32 characters for hex_32, got %d ('%s')", len(hexToken), hexToken)
	}
	decodedHex, err := hex.DecodeString(hexToken)
	if err != nil || len(decodedHex) != 16 {
		t.Errorf("failed to decode valid hex_32: %v", err)
	}

	// 2. pass_16
	passToken, err := GenerateToken("pass_16")
	if err != nil {
		t.Fatalf("unexpected error generating pass_16: %v", err)
	}
	if len(passToken) != 16 {
		t.Errorf("expected 16 characters for pass_16, got %d ('%s')", len(passToken), passToken)
	}

	// 3. base64_32
	b64Token, err := GenerateToken("base64_32")
	if err != nil {
		t.Fatalf("unexpected error generating base64_32: %v", err)
	}
	decodedB64, err := base64.StdEncoding.DecodeString(b64Token)
	if err != nil || len(decodedB64) != 32 {
		t.Errorf("failed to decode valid base64_32 (expected 32 bytes): %v", err)
	}
}

func TestDeployer_DeployPocketBase_WithStore(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer st.Close()

	cat := DefaultCatalog()
	deployer := NewDeployer(cat, st, nil)
	deployer.SetVolumeRoot("/var/lib/pikpik/volumes")

	req := DeployTemplateRequest{
		Name:      "test-pocketbase-app",
		ProjectID: "default",
		StageID:   "production",
		Variables: map[string]string{},
	}

	resp, err := deployer.Deploy(ctx, "pocketbase", req)
	if err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	if resp.AppID == "" {
		t.Errorf("expected non-empty AppID")
	}
	if resp.Name != "test-pocketbase-app" {
		t.Errorf("expected name 'test-pocketbase-app', got '%s'", resp.Name)
	}
	if resp.TemplateID != "pocketbase" {
		t.Errorf("expected template 'pocketbase', got '%s'", resp.TemplateID)
	}
	if resp.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", resp.Status)
	}
	if len(resp.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(resp.Volumes))
	}
	expectedVolPath := "/var/lib/pikpik/volumes/" + resp.AppID + "_pb_data"
	if resp.Volumes[0] != expectedVolPath {
		t.Errorf("expected volume '%s', got '%s'", expectedVolPath, resp.Volumes[0])
	}
	if resp.ResolvedVariables["POCKETBASE_ENCRYPTION_KEY"] != "[REDACTED]" {
		t.Errorf("expected redacted key '[REDACTED]', got '%s'", resp.ResolvedVariables["POCKETBASE_ENCRYPTION_KEY"])
	}

	// Verify SQLite Store Registration
	svc, err := st.Services().GetByID(ctx, resp.AppID)
	if err != nil || svc == nil {
		t.Fatalf("expected service in store: %v", err)
	}
	if svc.Name != "test-pocketbase-app" {
		t.Errorf("expected service name 'test-pocketbase-app', got '%s'", svc.Name)
	}
	if svc.ContainerPort != 8090 {
		t.Errorf("expected container port 8090, got %d", svc.ContainerPort)
	}

	// Verify Store Env Vars
	envs, err := st.EnvVars().ListByResource(ctx, store.TierService, resp.AppID)
	if err != nil || len(envs) == 0 {
		t.Fatalf("expected env vars in store: %v", err)
	}
	foundKey := false
	for _, env := range envs {
		if env.Key == "POCKETBASE_ENCRYPTION_KEY" {
			foundKey = true
			if len(env.ValueEncrypted) != 32 {
				t.Errorf("expected 32-char value, got '%s'", env.ValueEncrypted)
			}
		}
	}
	if !foundKey {
		t.Errorf("expected POCKETBASE_ENCRYPTION_KEY in store env vars")
	}

	// Verify Store Volumes
	vols, err := st.Volumes().ListByService(ctx, resp.AppID)
	if err != nil || len(vols) != 1 {
		t.Fatalf("expected 1 volume in store, got %d: %v", len(vols), err)
	}
	if vols[0].MountPath != "/pb_data" {
		t.Errorf("expected mount path '/pb_data', got '%s'", vols[0].MountPath)
	}
	if vols[0].HostPath != expectedVolPath {
		t.Errorf("expected host path '%s', got '%s'", expectedVolPath, vols[0].HostPath)
	}

	// Verify Store Deployments
	deps, err := st.Deployments().ListByService(ctx, resp.AppID, 10)
	if err != nil || len(deps) != 1 {
		t.Fatalf("expected 1 deployment record, got %d: %v", len(deps), err)
	}
	if deps[0].Status != "healthy" {
		t.Errorf("expected deployment status 'healthy', got '%s'", deps[0].Status)
	}
}

func TestDeployer_UserVariableOverrides(t *testing.T) {
	ctx := context.Background()
	deployer := NewDeployer(nil, nil, nil)

	customKey := "0123456789abcdef0123456789abcdef"
	req := DeployTemplateRequest{
		Name: "my-custom-n8n",
		Variables: map[string]string{
			"N8N_ENCRYPTION_KEY": customKey,
			"GENERIC_TIMEZONE":   "America/New_York",
		},
	}

	resp, err := deployer.Deploy(ctx, "n8n", req)
	if err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	if resp.ResolvedVariables["N8N_ENCRYPTION_KEY"] != "[REDACTED]" {
		t.Errorf("expected '[REDACTED]', got '%s'", resp.ResolvedVariables["N8N_ENCRYPTION_KEY"])
	}
	if resp.ResolvedVariables["GENERIC_TIMEZONE"] != "America/New_York" {
		t.Errorf("expected 'America/New_York', got '%s'", resp.ResolvedVariables["GENERIC_TIMEZONE"])
	}
	if resp.ResolvedVariables["WEBHOOK_URL"] != "http://localhost:5678/" {
		t.Errorf("expected default webhook url, got '%s'", resp.ResolvedVariables["WEBHOOK_URL"])
	}
}

func TestDeployer_InvalidTemplate(t *testing.T) {
	ctx := context.Background()
	deployer := NewDeployer(nil, nil, nil)

	_, err := deployer.Deploy(ctx, "nonexistent-template-12345", DeployTemplateRequest{})
	if err == nil {
		t.Fatalf("expected error for nonexistent template, got nil")
	}
}

func TestDeployer_ServiceTopologicalOrder(t *testing.T) {
	deployer := NewDeployer(nil, nil, nil)

	services := []TemplateService{
		{Name: "app", DependsOn: []string{"redis", "db"}},
		{Name: "db"},
		{Name: "redis"},
	}

	ordered, err := deployer.resolveServiceOrder(services)
	if err != nil {
		t.Fatalf("topological sort failed: %v", err)
	}

	if len(ordered) != 3 {
		t.Fatalf("expected 3 ordered services, got %d", len(ordered))
	}
	if ordered[2].Name != "app" {
		t.Errorf("expected 'app' to be last in order, got '%s'", ordered[2].Name)
	}

	// Cyclic dependency test
	cyclic := []TemplateService{
		{Name: "svc-a", DependsOn: []string{"svc-b"}},
		{Name: "svc-b", DependsOn: []string{"svc-a"}},
	}
	_, err = deployer.resolveServiceOrder(cyclic)
	if err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Errorf("expected cyclic dependency error, got %v", err)
	}
}

func TestDeployer_SanitizeResolvedVariables(t *testing.T) {
	tpl := &Template{
		EnvVars: []TemplateEnvVar{
			{Key: "APP_NAME", IsSecret: false},
			{Key: "DATABASE_PASSWORD", IsSecret: true},
			{Key: "SECRET_TOKEN", AutoGenerate: "hex_32"},
			{Key: "PUBLIC_PORT", Default: "8080"},
		},
	}

	rawVars := map[string]string{
		"APP_NAME":          "my-app",
		"DATABASE_PASSWORD": "supersecretpassword",
		"SECRET_TOKEN":      "token12345",
		"PUBLIC_PORT":       "8080",
		"CUSTOM_API_KEY":    "key9999",
		"UNRELATED_CONFIG":  "config_val",
	}

	sanitized := sanitizeResolvedVariables(rawVars, tpl)

	if sanitized["APP_NAME"] != "my-app" {
		t.Errorf("expected APP_NAME 'my-app', got '%s'", sanitized["APP_NAME"])
	}
	if sanitized["PUBLIC_PORT"] != "8080" {
		t.Errorf("expected PUBLIC_PORT '8080', got '%s'", sanitized["PUBLIC_PORT"])
	}
	if sanitized["UNRELATED_CONFIG"] != "config_val" {
		t.Errorf("expected UNRELATED_CONFIG 'config_val', got '%s'", sanitized["UNRELATED_CONFIG"])
	}
	if sanitized["DATABASE_PASSWORD"] != "[REDACTED]" {
		t.Errorf("expected DATABASE_PASSWORD '[REDACTED]', got '%s'", sanitized["DATABASE_PASSWORD"])
	}
	if sanitized["SECRET_TOKEN"] != "[REDACTED]" {
		t.Errorf("expected SECRET_TOKEN '[REDACTED]', got '%s'", sanitized["SECRET_TOKEN"])
	}
	if sanitized["CUSTOM_API_KEY"] != "[REDACTED]" {
		t.Errorf("expected CUSTOM_API_KEY '[REDACTED]', got '%s'", sanitized["CUSTOM_API_KEY"])
	}
}

// Mock structures for Rollback testing
type testMockContainerManager struct {
	createCount int
	startCount  int
	stoppedCIDs []string
	removedCIDs []string
	failOnStart int
}

func (m *testMockContainerManager) Create(ctx context.Context, spec orchestration.ContainerSpec) (string, error) {
	m.createCount++
	return spec.ID, nil
}

func (m *testMockContainerManager) Start(ctx context.Context, containerID string) error {
	m.startCount++
	if m.failOnStart > 0 && m.startCount == m.failOnStart {
		return errors.New("simulated container start failure")
	}
	return nil
}

func (m *testMockContainerManager) Stop(ctx context.Context, containerID string, timeout time.Duration) error {
	m.stoppedCIDs = append(m.stoppedCIDs, containerID)
	return nil
}

func (m *testMockContainerManager) Remove(ctx context.Context, containerID string, force bool, removeVolumes bool) error {
	m.removedCIDs = append(m.removedCIDs, containerID)
	return nil
}

func (m *testMockContainerManager) Restart(ctx context.Context, containerID string, timeout time.Duration) error {
	return nil
}
func (m *testMockContainerManager) Inspect(ctx context.Context, containerID string) (*orchestration.ContainerStatus, error) {
	return nil, nil
}
func (m *testMockContainerManager) List(ctx context.Context, opts orchestration.ListOptions) ([]orchestration.ContainerStatus, error) {
	return nil, nil
}
func (m *testMockContainerManager) DeployWithRollingUpdate(ctx context.Context, spec orchestration.ContainerSpec, updateCfg orchestration.RollingUpdateConfig) (*orchestration.RollingUpdateResult, error) {
	return nil, nil
}

type testMockOrchestrator struct {
	containers *testMockContainerManager
}

func (m *testMockOrchestrator) Mode() orchestration.RuntimeMode { return orchestration.ModeStandalone }
func (m *testMockOrchestrator) Ping(ctx context.Context) error { return nil }
func (m *testMockOrchestrator) Close() error { return nil }
func (m *testMockOrchestrator) RawClient() client.CommonAPIClient { return nil }
func (m *testMockOrchestrator) Containers() orchestration.ContainerManager { return m.containers }
func (m *testMockOrchestrator) Swarm() orchestration.SwarmManager { return nil }
func (m *testMockOrchestrator) Stacks() orchestration.StackManager { return nil }
func (m *testMockOrchestrator) Logs() orchestration.LogStreamer { return nil }

func TestDeployer_RollbackOnPartialFailure(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_rollback.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer st.Close()

	// Multi-service template with 2 services
	multiSvcTpl := Template{
		ID:          "multi-test",
		Name:        "Multi-Service Test",
		Category:    CategoryProductivityDevTools,
		Description: "Stack with two services for rollback test",
		Services: []TemplateService{
			{Name: "backend", Image: "mock/backend:1.0"},
			{Name: "frontend", Image: "mock/frontend:1.0", DependsOn: []string{"backend"}},
		},
	}

	cat := &Catalog{
		templates: map[string]Template{
			"multi-test": multiSvcTpl,
		},
	}

	mockCM := &testMockContainerManager{
		failOnStart: 2, // Backend succeeds, Frontend fails to start
	}
	orch := &testMockOrchestrator{containers: mockCM}

	deployer := NewDeployer(cat, st, orch)
	deployer.SetVolumeRoot(tmpDir)

	req := DeployTemplateRequest{
		Name: "test-rollback-app",
	}

	resp, err := deployer.Deploy(ctx, "multi-test", req)
	if err == nil {
		t.Fatalf("expected deploy error on second service, got success: %v", resp)
	}

	// Verify rollback cleaned up the first started container
	if len(mockCM.stoppedCIDs) == 0 {
		t.Errorf("expected stopped containers during rollback, got 0")
	}
	if len(mockCM.removedCIDs) == 0 {
		t.Errorf("expected removed containers during rollback, got 0")
	}

	// Verify store was cleaned up (no orphaned service record)
	svcs, err := st.Services().ListByStage(ctx, "production")
	if err == nil && len(svcs) > 0 {
		t.Errorf("expected 0 services in store after rollback, found %d", len(svcs))
	}

	// Verify store env vars were cleaned up by rollback (no orphaned secrets)
	if resp != nil && resp.AppID != "" {
		envs, err := st.EnvVars().ListByResource(ctx, store.TierService, resp.AppID)
		if err == nil && len(envs) > 0 {
			t.Errorf("expected 0 env vars in store after rollback, found %d", len(envs))
		}
	}
}

func TestDeployer_DeployWithVaultEncryption(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer st.Close()
	cat := DefaultCatalog()

	vault, err := crypto.NewAESVault("master-secret-for-testing-vault-32b!")
	if err != nil {
		t.Fatalf("failed to create vault: %v", err)
	}

	mockCM := &testMockContainerManager{}
	orch := &testMockOrchestrator{containers: mockCM}

	deployer := NewDeployer(cat, st, orch, vault)
	deployer.SetVolumeRoot(tmpDir)

	req := DeployTemplateRequest{
		Name: "test-vault-pb",
	}

	resp, err := deployer.Deploy(ctx, "pocketbase", req)
	if err != nil {
		t.Fatalf("deploy failed: %v", err)
	}

	envs, err := st.EnvVars().ListByResource(ctx, store.TierService, resp.AppID)
	if err != nil || len(envs) == 0 {
		t.Fatalf("expected env vars in store: %v", err)
	}

	foundKey := false
	for _, env := range envs {
		if env.Key == "POCKETBASE_ENCRYPTION_KEY" {
			foundKey = true
			if !strings.HasPrefix(env.ValueEncrypted, "v1:") {
				t.Errorf("expected v1: prefix on encrypted secret, got: %s", env.ValueEncrypted)
			}
			decrypted, err := vault.DecryptString(ctx, env.ValueEncrypted)
			if err != nil {
				t.Fatalf("failed to decrypt secret: %v", err)
			}
			if len(decrypted) != 32 {
				t.Errorf("expected 32-char decrypted secret, got length %d (%s)", len(decrypted), decrypted)
			}
		}
	}
	if !foundKey {
		t.Errorf("expected POCKETBASE_ENCRYPTION_KEY in store env vars")
	}
}

func TestDeployer_DeployMinIO_WithStore(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "minio_test.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer st.Close()

	cat := DefaultCatalog()
	deployer := NewDeployer(cat, st, nil)
	deployer.SetVolumeRoot(tmpDir)

	req := DeployTemplateRequest{
		Name: "my-minio-storage",
	}

	resp, err := deployer.Deploy(ctx, "minio", req)
	if err != nil {
		t.Fatalf("deploy minio failed: %v", err)
	}

	if resp.TemplateID != "minio" {
		t.Errorf("expected template 'minio', got '%s'", resp.TemplateID)
	}
	if resp.Category != CategoryStorage {
		t.Errorf("expected category '%s', got '%s'", CategoryStorage, resp.Category)
	}
	if resp.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", resp.Status)
	}
	if len(resp.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(resp.Volumes))
	}
	if resp.ResolvedVariables["MINIO_ROOT_USER"] != "minioadmin" {
		t.Errorf("expected MINIO_ROOT_USER 'minioadmin', got '%s'", resp.ResolvedVariables["MINIO_ROOT_USER"])
	}
	if resp.ResolvedVariables["MINIO_ROOT_PASSWORD"] != "[REDACTED]" {
		t.Errorf("expected MINIO_ROOT_PASSWORD to be redacted, got '%s'", resp.ResolvedVariables["MINIO_ROOT_PASSWORD"])
	}

	// Verify store
	svc, err := st.Services().GetByID(ctx, resp.AppID)
	if err != nil || svc == nil {
		t.Fatalf("service not found in store: %v", err)
	}
	if svc.ContainerPort != 9000 {
		t.Errorf("expected default container port 9000, got %d", svc.ContainerPort)
	}

	vols, err := st.Volumes().ListByService(ctx, resp.AppID)
	if err != nil || len(vols) != 1 {
		t.Fatalf("expected 1 volume in store, got %d", len(vols))
	}
	if vols[0].MountPath != "/data" {
		t.Errorf("expected volume mount path '/data', got '%s'", vols[0].MountPath)
	}
}

func TestDeployer_DeployMeilisearch_WithStore(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "meili_test.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer st.Close()

	cat := DefaultCatalog()
	deployer := NewDeployer(cat, st, nil)
	deployer.SetVolumeRoot(tmpDir)

	req := DeployTemplateRequest{
		Name: "my-meilisearch-db",
	}

	resp, err := deployer.Deploy(ctx, "meilisearch", req)
	if err != nil {
		t.Fatalf("deploy meilisearch failed: %v", err)
	}

	if resp.TemplateID != "meilisearch" {
		t.Errorf("expected template 'meilisearch', got '%s'", resp.TemplateID)
	}
	if resp.Category != CategoryDatabase {
		t.Errorf("expected category '%s', got '%s'", CategoryDatabase, resp.Category)
	}
	if resp.ResolvedVariables["MEILI_ENV"] != "production" {
		t.Errorf("expected MEILI_ENV 'production', got '%s'", resp.ResolvedVariables["MEILI_ENV"])
	}
	if resp.ResolvedVariables["MEILI_MASTER_KEY"] != "[REDACTED]" {
		t.Errorf("expected redacted master key, got '%s'", resp.ResolvedVariables["MEILI_MASTER_KEY"])
	}

	vols, err := st.Volumes().ListByService(ctx, resp.AppID)
	if err != nil || len(vols) != 1 {
		t.Fatalf("expected 1 volume in store, got %d", len(vols))
	}
	if vols[0].MountPath != "/meili_data" {
		t.Errorf("expected mount path '/meili_data', got '%s'", vols[0].MountPath)
	}
}

func TestDeployer_DeployVaultwarden_WithStore(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "vaultwarden_test.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer st.Close()

	cat := DefaultCatalog()
	deployer := NewDeployer(cat, st, nil)
	deployer.SetVolumeRoot(tmpDir)

	req := DeployTemplateRequest{
		Name: "my-vaultwarden",
	}

	resp, err := deployer.Deploy(ctx, "vaultwarden", req)
	if err != nil {
		t.Fatalf("deploy vaultwarden failed: %v", err)
	}

	if resp.TemplateID != "vaultwarden" {
		t.Errorf("expected template 'vaultwarden', got '%s'", resp.TemplateID)
	}
	if resp.Category != CategoryProductivity {
		t.Errorf("expected category '%s', got '%s'", CategoryProductivity, resp.Category)
	}
	if resp.ResolvedVariables["SIGNUPS_ALLOWED"] != "true" {
		t.Errorf("expected SIGNUPS_ALLOWED 'true', got '%s'", resp.ResolvedVariables["SIGNUPS_ALLOWED"])
	}

	vols, err := st.Volumes().ListByService(ctx, resp.AppID)
	if err != nil || len(vols) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(vols))
	}
	if vols[0].MountPath != "/data" {
		t.Errorf("expected mount path '/data', got '%s'", vols[0].MountPath)
	}
}

func TestDeployer_DeployGhost_WithStore(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "ghost_test.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer st.Close()

	cat := DefaultCatalog()
	deployer := NewDeployer(cat, st, nil)
	deployer.SetVolumeRoot(tmpDir)

	req := DeployTemplateRequest{
		Name: "my-ghost-blog",
	}

	resp, err := deployer.Deploy(ctx, "ghost", req)
	if err != nil {
		t.Fatalf("deploy ghost failed: %v", err)
	}

	if resp.TemplateID != "ghost" {
		t.Errorf("expected template 'ghost', got '%s'", resp.TemplateID)
	}
	if resp.Category != CategoryCMS {
		t.Errorf("expected category '%s', got '%s'", CategoryCMS, resp.Category)
	}
	if resp.ResolvedVariables["url"] != "http://localhost:2368" {
		t.Errorf("expected url 'http://localhost:2368', got '%s'", resp.ResolvedVariables["url"])
	}
	if resp.ResolvedVariables["NODE_ENV"] != "production" {
		t.Errorf("expected NODE_ENV 'production', got '%s'", resp.ResolvedVariables["NODE_ENV"])
	}

	vols, err := st.Volumes().ListByService(ctx, resp.AppID)
	if err != nil || len(vols) != 1 {
		t.Fatalf("expected 1 volume in store, got %d", len(vols))
	}
	if vols[0].MountPath != "/var/lib/ghost/content" {
		t.Errorf("expected mount path '/var/lib/ghost/content', got '%s'", vols[0].MountPath)
	}
}

func TestDeployer_DeployUptimeKuma_WithStore(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "kuma_test.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer st.Close()

	cat := DefaultCatalog()
	deployer := NewDeployer(cat, st, nil)
	deployer.SetVolumeRoot(tmpDir)

	req := DeployTemplateRequest{
		Name: "my-uptime-kuma",
	}

	resp, err := deployer.Deploy(ctx, "uptime-kuma", req)
	if err != nil {
		t.Fatalf("deploy uptime-kuma failed: %v", err)
	}

	if resp.TemplateID != "uptime-kuma" {
		t.Errorf("expected template 'uptime-kuma', got '%s'", resp.TemplateID)
	}
	if resp.Category != CategoryProductivity {
		t.Errorf("expected category '%s', got '%s'", CategoryProductivity, resp.Category)
	}
	if resp.ResolvedVariables["UPTIME_KUMA_PORT"] != "3001" {
		t.Errorf("expected port '3001', got '%s'", resp.ResolvedVariables["UPTIME_KUMA_PORT"])
	}

	svc, err := st.Services().GetByID(ctx, resp.AppID)
	if err != nil || svc == nil {
		t.Fatalf("expected service in store: %v", err)
	}
	if svc.ContainerPort != 3001 {
		t.Errorf("expected port 3001, got %d", svc.ContainerPort)
	}

	vols, err := st.Volumes().ListByService(ctx, resp.AppID)
	if err != nil || len(vols) != 1 {
		t.Fatalf("expected 1 volume in store, got %d", len(vols))
	}
	if vols[0].MountPath != "/app/data" {
		t.Errorf("expected mount path '/app/data', got '%s'", vols[0].MountPath)
	}
}

func TestDeployer_DeployPlausible_WithStore(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "plausible_test.db")

	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open sqlite store: %v", err)
	}
	defer st.Close()

	cat := DefaultCatalog()
	deployer := NewDeployer(cat, st, nil)
	deployer.SetVolumeRoot(tmpDir)

	req := DeployTemplateRequest{
		Name: "my-plausible-analytics",
	}

	resp, err := deployer.Deploy(ctx, "plausible", req)
	if err != nil {
		t.Fatalf("deploy plausible failed: %v", err)
	}

	if resp.TemplateID != "plausible" {
		t.Errorf("expected template 'plausible', got '%s'", resp.TemplateID)
	}
	if resp.Category != CategoryAnalytics {
		t.Errorf("expected category '%s', got '%s'", CategoryAnalytics, resp.Category)
	}
	if resp.ResolvedVariables["BASE_URL"] != "http://localhost:8000" {
		t.Errorf("expected BASE_URL 'http://localhost:8000', got '%s'", resp.ResolvedVariables["BASE_URL"])
	}
	if resp.ResolvedVariables["SECRET_KEY_BASE"] != "[REDACTED]" {
		t.Errorf("expected redacted SECRET_KEY_BASE, got '%s'", resp.ResolvedVariables["SECRET_KEY_BASE"])
	}

	vols, err := st.Volumes().ListByService(ctx, resp.AppID)
	if err != nil || len(vols) != 1 {
		t.Fatalf("expected 1 volume in store, got %d", len(vols))
	}
	if vols[0].MountPath != "/var/lib/plausible" {
		t.Errorf("expected mount path '/var/lib/plausible', got '%s'", vols[0].MountPath)
	}
}

func TestDeployer_AllCuratedRecipesInstantiate(t *testing.T) {
	ctx := context.Background()
	cat := DefaultCatalog()
	all := cat.ListTemplates("")

	for _, tpl := range all {
		t.Run(tpl.ID, func(t *testing.T) {
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")
			st, err := store.Open(dbPath)
			if err != nil {
				t.Fatalf("failed to open sqlite store: %v", err)
			}
			defer st.Close()

			deployer := NewDeployer(cat, st, nil)
			deployer.SetVolumeRoot(tmpDir)

			req := DeployTemplateRequest{
				Name:                "test-" + tpl.ID,
				AutoGenerateMissing: true,
			}

			resp, err := deployer.Deploy(ctx, tpl.ID, req)
			if err != nil {
				t.Fatalf("template %s failed to instantiate: %v", tpl.ID, err)
			}

			if resp.AppID == "" {
				t.Errorf("template %s returned empty AppID", tpl.ID)
			}
			if resp.TemplateID != tpl.ID {
				t.Errorf("template %s mismatch: got %s", tpl.ID, resp.TemplateID)
			}
			if resp.Category != tpl.Category {
				t.Errorf("template %s category mismatch: expected %s, got %s", tpl.ID, tpl.Category, resp.Category)
			}
			if resp.Status != "running" {
				t.Errorf("template %s status: expected running, got %s", tpl.ID, resp.Status)
			}
			if len(resp.Volumes) != len(tpl.Volumes) {
				t.Errorf("template %s volumes count mismatch: expected %d, got %d", tpl.ID, len(tpl.Volumes), len(resp.Volumes))
			}

			// Verify store service record
			svc, err := st.Services().GetByID(ctx, resp.AppID)
			if err != nil || svc == nil {
				t.Fatalf("template %s service not saved in store: %v", tpl.ID, err)
			}
			if svc.ContainerPort != tpl.DefaultPort {
				t.Errorf("template %s port mismatch: expected %d, got %d", tpl.ID, tpl.DefaultPort, svc.ContainerPort)
			}
		})
	}
}


