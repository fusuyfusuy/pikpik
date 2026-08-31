package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/templates"
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

	// 14. Template Methods
	tpls, err := client.ListTemplates(context.Background(), "", "")
	if err != nil || len(tpls) < 22 {
		t.Fatalf("client list templates failed: %v (found %d)", err, len(tpls))
	}

	minioTpl, err := client.GetTemplate(context.Background(), "minio")
	if err != nil || minioTpl.ID != "minio" {
		t.Fatalf("client get minio template failed: %v", err)
	}
	if minioTpl.Category != "Storage" {
		t.Errorf("expected category Storage, got %s", minioTpl.Category)
	}

	kumaTpl, err := client.GetTemplate(context.Background(), "uptime-kuma")
	if err != nil || kumaTpl.ID != "uptime-kuma" {
		t.Fatalf("client get uptime-kuma template failed: %v", err)
	}
	if kumaTpl.DefaultPort != 3001 {
		t.Errorf("expected port 3001, got %d", kumaTpl.DefaultPort)
	}

	deployedPb, err := client.DeployTemplate(context.Background(), "pocketbase", templates.DeployTemplateRequest{
		Name: "cli-test-pb",
	})
	if err != nil || deployedPb.TemplateID != "pocketbase" {
		t.Fatalf("client deploy template failed: %v", err)
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

// 5. Test CLI App Traffic and Canary Management
func TestAPIClient_AppTraffic(t *testing.T) {
	ctrl := api.NewDefaultController(api.ControllerDependencies{})
	gw := api.NewAPIGateway(ctrl, nil, nil)
	server := httptest.NewServer(gw)
	defer server.Close()

	client := NewAPIClient(Context{
		ServerURL:      server.URL,
		Token:          "pik_live_testclienttoken",
		TimeoutSeconds: 5,
	})

	ctx := context.Background()
	app, err := client.CreateApp(ctx, api.CreateAppRequest{
		Name:     "traffic-cli-app",
		Image:    "web:v1",
		Replicas: 1,
	})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	// 1. Get default traffic
	traffic, err := client.GetAppTraffic(ctx, app.ID)
	if err != nil {
		t.Fatalf("failed to get default app traffic: %v", err)
	}
	if len(traffic.Splits) != 1 || traffic.Splits[0].Weight != 100 {
		t.Errorf("expected default 100%% split, got %+v", traffic.Splits)
	}

	// 2. Set 90/10 Canary Split
	updated, err := client.SetAppTraffic(ctx, app.ID, api.SetTrafficSplitRequest{
		Splits: []api.UpstreamWeight{
			{Upstream: "web-v1:8080", Weight: 90},
			{Upstream: "web-v2:8080", Weight: 10},
		},
	})
	if err != nil {
		t.Fatalf("failed to set 90/10 traffic split: %v", err)
	}
	if len(updated.Splits) != 2 || updated.Splits[0].Weight != 90 || updated.Splits[1].Weight != 10 {
		t.Errorf("expected 90/10 split, got %+v", updated.Splits)
	}

	// 3. Reset traffic to 100% stable
	reset, err := client.SetAppTraffic(ctx, app.ID, api.SetTrafficSplitRequest{
		Reset: true,
	})
	if err != nil {
		t.Fatalf("failed to reset app traffic: %v", err)
	}
	if len(reset.Splits) != 1 || reset.Splits[0].Weight != 100 {
		t.Errorf("expected reset to 100%% split, got %+v", reset.Splits)
	}
}

// 6. Test parseUpstreamWeight helper
func TestParseUpstreamWeight(t *testing.T) {
	tests := []struct {
		input       string
		wantTarget  string
		wantWeight  int
		expectError bool
	}{
		{"blue:3000:90", "blue:3000", 90, false},
		{"canary:10", "canary", 10, false},
		{"blue:3000=90", "blue:3000", 90, false},
		{"canary=10", "canary", 10, false},
		{"legacy:0", "legacy", 0, false},
		{"app:100", "app", 100, false},
		{"app:-5", "", 0, true},
		{"", "", 0, true},
		{"=50", "", 0, true},
		{"invalid", "", 0, true},
	}

	for _, tc := range tests {
		target, weight, err := parseUpstreamWeight(tc.input)
		if tc.expectError {
			if err == nil {
				t.Errorf("parseUpstreamWeight(%q) expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseUpstreamWeight(%q) unexpected error: %v", tc.input, err)
			}
			if target != tc.wantTarget || weight != tc.wantWeight {
				t.Errorf("parseUpstreamWeight(%q) = (%q, %d), want (%q, %d)", tc.input, target, weight, tc.wantTarget, tc.wantWeight)
			}
		}
	}
}

// 7. Test Notification Channel Client API Methods
func TestAPIClient_Notifications(t *testing.T) {
	ctrl := api.NewDefaultController(api.ControllerDependencies{})
	gw := api.NewAPIGateway(ctrl, nil, nil)
	ts := httptest.NewServer(gw)
	defer ts.Close()

	client := NewAPIClient(Context{
		ServerURL: ts.URL,
		Token:     "test-token",
	})
	ctx := context.Background()

	// 1. Create notification channel
	ch, err := client.CreateNotificationChannel(ctx, api.CreateNotificationChannelRequest{
		Name:      "Discord Deploy Alerts",
		Type:      "discord",
		TargetURL: "https://discord.com/api/webhooks/123/abc",
		Events:    []string{"deploy:failure", "deploy:success"},
	})
	if err != nil {
		t.Fatalf("failed to create notification channel: %v", err)
	}
	if ch.Name != "Discord Deploy Alerts" || ch.Type != "discord" {
		t.Fatalf("unexpected created channel: %+v", ch)
	}

	// 2. List notification channels
	list, err := client.ListNotificationChannels(ctx, "")
	if err != nil {
		t.Fatalf("failed to list notification channels: %v", err)
	}
	if len(list) != 1 || list[0].ID != ch.ID {
		t.Fatalf("unexpected channels list: %+v", list)
	}

	// 3. Delete notification channel
	if err := client.DeleteNotificationChannel(ctx, ch.ID); err != nil {
		t.Fatalf("failed to delete notification channel: %v", err)
	}

	// 4. Verify empty list
	afterList, err := client.ListNotificationChannels(ctx, "")
	if err != nil {
		t.Fatalf("failed to list notification channels: %v", err)
	}
	if len(afterList) != 0 {
		t.Fatalf("expected 0 channels after deletion, got %d", len(afterList))
	}
}
