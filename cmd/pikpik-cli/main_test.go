package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/api"
)

// 1. Test CLI Atomic Config Persistence and Permissions
func TestConfigManager_AtomicPersistence(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pikpik-cli-test-*")
	if err != nil {
		t.Fatalf("temp dir creation failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "config.json")
	cm := &ConfigManager{configPath: configPath}

	cfg := &Config{
		Version:        1,
		CurrentContext: "prod",
		Contexts: map[string]Context{
			"prod": {
				ServerURL: "https://pikpik.example.com",
				Token:     "pik_live_secret_token_12345",
			},
		},
	}

	if err := cm.Save(cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Verify file permissions
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("failed to stat config file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 file permissions, got %o", info.Mode().Perm())
	}

	// Verify reload
	loaded, err := cm.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if loaded.CurrentContext != "prod" || loaded.Contexts["prod"].Token != "pik_live_secret_token_12345" {
		t.Errorf("loaded config does not match saved config: %+v", loaded)
	}
}

// 2. Test Multi-Context Resolution
func TestConfigManager_ContextResolution(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pikpik-cli-ctx-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cm := &ConfigManager{configPath: filepath.Join(tempDir, "config.json")}
	cfg := &Config{
		Version:        1,
		CurrentContext: "staging",
		Contexts: map[string]Context{
			"prod": {
				ServerURL: "https://prod.pikpik.dev",
				Token:     "pik_live_prod_token",
			},
			"staging": {
				ServerURL: "https://staging.pikpik.dev",
				Token:     "pik_live_staging_token",
			},
		},
	}

	_ = cm.Save(cfg)

	ctx, name, err := cm.GetActiveContext(cfg)
	if err != nil {
		t.Fatalf("get active context failed: %v", err)
	}
	if name != "staging" || ctx.ServerURL != "https://staging.pikpik.dev" {
		t.Fatalf("expected staging context, got %s (%s)", name, ctx.ServerURL)
	}
}

// 3. Test APIClient REST calls against test server
func TestAPIClient_Endpoints(t *testing.T) {
	ctrl := api.NewDefaultController(api.ControllerDependencies{})
	gw := api.NewAPIGateway(ctrl, nil, nil)
	server := httptest.NewServer(gw)
	defer server.Close()

	client := NewAPIClient(Context{
		ServerURL:      server.URL,
		Token:          "pik_live_testclienttoken",
		TimeoutSeconds: 5,
	})

	// 1. Create App
	app, err := client.CreateApp(context.Background(), api.CreateAppRequest{
		Name:     "cli-app",
		Image:    "redis:alpine",
		Replicas: 1,
	})
	if err != nil {
		t.Fatalf("client create app failed: %v", err)
	}
	if app.Name != "cli-app" {
		t.Errorf("expected app name cli-app, got %s", app.Name)
	}

	// 2. List Apps
	apps, err := client.ListApps(context.Background())
	if err != nil {
		t.Fatalf("client list apps failed: %v", err)
	}
	if len(apps) == 0 {
		t.Fatalf("expected at least 1 app")
	}

	// 3. Deploy App
	if err := client.DeployApp(context.Background(), app.ID, "redis:7.4"); err != nil {
		t.Fatalf("client deploy app failed: %v", err)
	}

	// 4. List Nodes
	nodes, err := client.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("client list nodes failed: %v", err)
	}
	if len(nodes) == 0 {
		t.Fatalf("expected at least 1 node")
	}

	// 5. Create Database
	db, err := client.CreateDatabase(context.Background(), api.CreateDatabaseRequest{
		Name:   "cli-db",
		Engine: "postgres",
	})
	if err != nil {
		t.Fatalf("client create database failed: %v", err)
	}
	if db.Name != "cli-db" {
		t.Errorf("expected db name cli-db, got %s", db.Name)
	}

	// 6. Create Backup
	bk, err := client.CreateBackup(context.Background(), db.ID)
	if err != nil {
		t.Fatalf("client create backup failed: %v", err)
	}
	if bk.ServiceID != db.ID {
		t.Errorf("expected service ID %s, got %s", db.ID, bk.ServiceID)
	}

	// 7. Prune System
	prune, err := client.PruneSystem(context.Background(), true, false)
	if err != nil {
		t.Fatalf("client prune failed: %v", err)
	}
	if prune == nil {
		t.Fatalf("expected non-nil prune result")
	}

	// 8. Stacks CLI Client Methods
	stk, err := client.CreateStack(context.Background(), api.CreateStackRequest{
		Name: "cli-stack",
		ComposeYAML: "version: '3.8'\nservices:\n  app:\n    image: nginx:alpine\n",
	})
	if err != nil {
		t.Fatalf("client create stack failed: %v", err)
	}
	if stk.Name != "cli-stack" {
		t.Errorf("expected stack name cli-stack, got %s", stk.Name)
	}

	stks, err := client.ListStacks(context.Background())
	if err != nil || len(stks) == 0 {
		t.Fatalf("client list stacks failed: %v, count: %d", err, len(stks))
	}

	if err := client.DeployStack(context.Background(), stk.ID); err != nil {
		t.Fatalf("client deploy stack failed: %v", err)
	}
	if err := client.RestartStack(context.Background(), stk.ID); err != nil {
		t.Fatalf("client restart stack failed: %v", err)
	}
	if err := client.StopStack(context.Background(), stk.ID); err != nil {
		t.Fatalf("client stop stack failed: %v", err)
	}
	if err := client.DeleteStack(context.Background(), stk.ID); err != nil {
		t.Fatalf("client delete stack failed: %v", err)
	}

	// 9. Networks CLI Client Methods
	net, err := client.CreateNetwork(context.Background(), api.CreateNetworkRequest{
		Name: "cli-net",
	})
	if err != nil {
		t.Fatalf("client create network failed: %v", err)
	}
	if net.Name != "cli-net" {
		t.Errorf("expected network name cli-net, got %s", net.Name)
	}

	nets, err := client.ListNetworks(context.Background(), "")
	if err != nil || len(nets) == 0 {
		t.Fatalf("client list networks failed: %v", err)
	}

	if _, err := client.PruneNetworks(context.Background(), ""); err != nil {
		t.Fatalf("client prune networks failed: %v", err)
	}
	if err := client.DeleteNetwork(context.Background(), net.ID); err != nil {
		t.Fatalf("client delete network failed: %v", err)
	}

	// 10. Volumes CLI Client Methods
	vol, err := client.CreateVolume(context.Background(), api.CreateVolumeRequest{
		Name: "cli-vol",
	})
	if err != nil {
		t.Fatalf("client create volume failed: %v", err)
	}
	if vol.Name != "cli-vol" {
		t.Errorf("expected volume name cli-vol, got %s", vol.Name)
	}

	vols, err := client.ListVolumes(context.Background(), "")
	if err != nil || len(vols) == 0 {
		t.Fatalf("client list volumes failed: %v", err)
	}

	if _, err := client.PruneVolumes(context.Background(), ""); err != nil {
		t.Fatalf("client prune volumes failed: %v", err)
	}
	if err := client.DeleteVolume(context.Background(), vol.ID); err != nil {
		t.Fatalf("client delete volume failed: %v", err)
	}

	// 11. Ingress / Domain Methods
	dom, err := client.BindDomain(context.Background(), api.BindDomainRequest{
		AppID:   app.ID,
		Domain:  "test.customdomain.org",
		AutoTLS: true,
	})
	if err != nil {
		t.Fatalf("client bind domain failed: %v", err)
	}
	if dom.Domain != "test.customdomain.org" {
		t.Errorf("expected domain test.customdomain.org, got %s", dom.Domain)
	}

	domains, err := client.ListDomains(context.Background())
	if err != nil || len(domains) == 0 {
		t.Fatalf("client list domains failed: %v", err)
	}

	if err := client.ReconcileIngress(context.Background()); err != nil {
		t.Fatalf("client reconcile ingress failed: %v", err)
	}

	if _, err := client.GetCaddyConfig(context.Background()); err != nil {
		t.Fatalf("client get caddy config failed: %v", err)
	}

	if err := client.DeleteDomain(context.Background(), dom.ID); err != nil {
		t.Fatalf("client delete domain failed: %v", err)
	}

	// 12. Registry Methods
	st, err := client.GetRegistryStatus(context.Background())
	if err != nil || st == nil {
		t.Fatalf("client get registry status failed: %v", err)
	}

	cat, err := client.ListRepositories(context.Background())
	if err != nil || cat == nil {
		t.Fatalf("client list repositories failed: %v", err)
	}

	creds, err := client.GetRegistryCredentials(context.Background(), "")
	if err != nil {
		t.Fatalf("client get registry creds failed: %v", err)
	}
	_ = creds

	if err := client.GarbageCollectRegistry(context.Background()); err != nil {
		t.Fatalf("client gc registry failed: %v", err)
	}

	// 13. Backup Schedules Methods
	enabled := true
	sch, err := client.CreateBackupSchedule(context.Background(), api.CreateBackupScheduleRequest{
		ServiceID:      db.ID,
		DatabaseType:   "postgres",
		Engine:         "postgres",
		CronExpr:       "0 2 * * *",
		CronExpression: "0 2 * * *",
		RetentionDaily: 7,
		RetentionDays:  7,
		IsEnabled:      &enabled,
	})
	if err != nil {
		t.Fatalf("client create backup schedule failed: %v", err)
	}
	if sch.ServiceID != db.ID {
		t.Errorf("expected service ID %s, got %s", db.ID, sch.ServiceID)
	}

	schedules, err := client.ListBackupSchedules(context.Background(), db.ID)
	if err != nil || len(schedules) == 0 {
		t.Fatalf("client list backup schedules failed: %v", err)
	}

	if err := client.DeleteBackupSchedule(context.Background(), sch.ID); err != nil {
		t.Fatalf("client delete backup schedule failed: %v", err)
	}
}

func TestFormatBytesInt(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{500, "500 B"},
		{1024, "1.0 KiB"},
		{1048576, "1.0 MiB"},
		{1073741824, "1.0 GiB"},
	}

	for _, tc := range tests {
		got := formatBytesInt(tc.input)
		if got != tc.expected {
			t.Errorf("formatBytesInt(%d) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

// 4. Test Init Command
func TestCLI_Init(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pikpik-init-test-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	origWd, _ := os.Getwd()
	_ = os.Chdir(tempDir)
	defer func() { _ = os.Chdir(origWd) }()

	runInit([]string{"--name", "test-project", "--port", "4000", "--domain", "project.test"})

	data, err := os.ReadFile(".pikpik.yml")
	if err != nil {
		t.Fatalf("failed to read generated .pikpik.yml: %v", err)
	}

	var parsed map[string]any
	_ = json.Unmarshal(data, &parsed)
	content := string(data)
	if !contains(content, "test-project") || !contains(content, "4000") || !contains(content, "project.test") {
		t.Errorf("unexpected .pikpik.yml content:\n%s", content)
	}
}

func contains(s, substr string) bool {
	return filepath.Base(s) == substr || (len(s) >= len(substr) && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
