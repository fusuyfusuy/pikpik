package templates

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"

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
	if len(resp.ResolvedVariables["POCKETBASE_ENCRYPTION_KEY"]) != 32 {
		t.Errorf("expected auto-generated 32-char hex key, got '%s'", resp.ResolvedVariables["POCKETBASE_ENCRYPTION_KEY"])
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
	if vols[0].MountPath != "/pb/pb_data" {
		t.Errorf("expected mount path '/pb/pb_data', got '%s'", vols[0].MountPath)
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

	if resp.ResolvedVariables["N8N_ENCRYPTION_KEY"] != customKey {
		t.Errorf("expected custom key '%s', got '%s'", customKey, resp.ResolvedVariables["N8N_ENCRYPTION_KEY"])
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
