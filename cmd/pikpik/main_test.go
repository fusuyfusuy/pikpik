package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseConfig(t *testing.T) {
	// 1. Long GNU flags
	argsLong := []string{
		"--listen", ":9090",
		"--admin-email", "test@pikpik.dev",
	}
	cfgLong := parseConfig(argsLong)

	if cfgLong.ListenAddr != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfgLong.ListenAddr)
	}
	if cfgLong.AdminEmail != "test@pikpik.dev" {
		t.Errorf("expected email test@pikpik.dev, got %s", cfgLong.AdminEmail)
	}

	// 2. Short UNIX flags
	argsShort := []string{
		"-l", ":9091",
		"-e", "short@pikpik.dev",
	}
	cfgShort := parseConfig(argsShort)

	if cfgShort.ListenAddr != ":9091" {
		t.Errorf("expected listen :9091, got %s", cfgShort.ListenAddr)
	}
	if cfgShort.AdminEmail != "short@pikpik.dev" {
		t.Errorf("expected email short@pikpik.dev, got %s", cfgShort.AdminEmail)
	}
}

func TestSetupUnifiedServer(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pikpik-server-test-*")
	if err != nil {
		t.Fatalf("temp dir creation failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "state.db")
	cfg := ServerConfig{
		ListenAddr:      ":0", // ephemeral port
		DBPath:          dbPath,
		DataDir:         tempDir,
		AdminEmail:      "admin@pikpik.test",
		AdminPassword:   "testpassword123!",
		EnrollmentToken: "test_enrollment_token",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, cleanup, err := setupUnifiedServer(ctx, cfg)
	if err != nil {
		t.Fatalf("setupUnifiedServer failed: %v", err)
	}
	defer cleanup()

	if server == nil {
		t.Fatalf("expected non-nil server")
	}

	// Verify server starts and responds
	go func() {
		_ = server.ListenAndServe()
	}()
	time.Sleep(50 * time.Millisecond)
	_ = server.Shutdown(context.Background())
}

func TestHealthCheckEndpoint(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pikpik-health-test-*")
	if err != nil {
		t.Fatalf("temp dir creation failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dbPath := filepath.Join(tempDir, "state.db")
	cfg := ServerConfig{
		ListenAddr: ":0",
		DBPath:     dbPath,
		DataDir:    tempDir,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, cleanup, err := setupUnifiedServer(ctx, cfg)
	if err != nil {
		t.Fatalf("setupUnifiedServer failed: %v", err)
	}
	defer cleanup()

	rec := &testResponseWriter{}
	req, _ := http.NewRequest("GET", "/healthz", nil)
	server.Handler.ServeHTTP(rec, req)

	if rec.statusCode != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.statusCode)
	}
}

func TestParseConfig_GeneratesSecretsWhenUnset(t *testing.T) {
	os.Unsetenv("PIKPIK_ADMIN_PASSWORD")
	os.Unsetenv("PIKPIK_ENROLLMENT_TOKEN")

	cfg := parseConfig([]string{"--listen", ":9099"})

	if cfg.AdminPassword == "" || cfg.AdminPassword == "pikpikAdmin123!" {
		t.Fatalf("expected a generated non-default admin password, got %q", cfg.AdminPassword)
	}
	if cfg.EnrollmentToken == "" || cfg.EnrollmentToken == "pik_node_enrollment_secret_token" {
		t.Fatalf("expected a generated non-default enrollment token, got %q", cfg.EnrollmentToken)
	}

	// Independent invocations must not reuse the same generated secrets.
	cfg2 := parseConfig([]string{"--listen", ":9098"})
	if cfg.AdminPassword == cfg2.AdminPassword {
		t.Fatalf("expected distinct generated admin passwords across invocations")
	}
	if cfg.EnrollmentToken == cfg2.EnrollmentToken {
		t.Fatalf("expected distinct generated enrollment tokens across invocations")
	}
}

func TestParseConfig_HonorsExplicitSecrets(t *testing.T) {
	// Explicit CLI flags must be honored unchanged.
	cfg := parseConfig([]string{"--admin-password", "explicit-pw-123", "--token", "explicit-token-456"})
	if cfg.AdminPassword != "explicit-pw-123" {
		t.Errorf("expected explicit admin password via flag to be honored, got %q", cfg.AdminPassword)
	}
	if cfg.EnrollmentToken != "explicit-token-456" {
		t.Errorf("expected explicit enrollment token via flag to be honored, got %q", cfg.EnrollmentToken)
	}

	// Explicit env vars must be honored unchanged.
	t.Setenv("PIKPIK_ADMIN_PASSWORD", "env-pw-789")
	t.Setenv("PIKPIK_ENROLLMENT_TOKEN", "env-token-789")
	cfgEnv := parseConfig(nil)
	if cfgEnv.AdminPassword != "env-pw-789" {
		t.Errorf("expected explicit admin password via env to be honored, got %q", cfgEnv.AdminPassword)
	}
	if cfgEnv.EnrollmentToken != "env-token-789" {
		t.Errorf("expected explicit enrollment token via env to be honored, got %q", cfgEnv.EnrollmentToken)
	}
}

func TestLoadOrCreateMasterKey_ExplicitEnvHonored(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pikpik-masterkey-env-test-*")
	if err != nil {
		t.Fatalf("temp dir creation failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	t.Setenv("PIKPIK_MASTER_KEY", "explicit-operator-supplied-master-key-32b")

	key, err := loadOrCreateMasterKey(tempDir)
	if err != nil {
		t.Fatalf("loadOrCreateMasterKey failed: %v", err)
	}
	if key != "explicit-operator-supplied-master-key-32b" {
		t.Fatalf("expected explicit PIKPIK_MASTER_KEY to be honored, got %q", key)
	}
	if _, err := os.Stat(filepath.Join(tempDir, masterKeyFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected no master key file to be written when an explicit key is supplied")
	}
}

func TestMasterKeyPersistsAcrossRestarts(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pikpik-masterkey-test-*")
	if err != nil {
		t.Fatalf("temp dir creation failed: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := ServerConfig{
		ListenAddr:      ":0",
		DBPath:          filepath.Join(tempDir, "state.db"),
		DataDir:         tempDir,
		AdminEmail:      "admin@pikpik.test",
		AdminPassword:   "testpassword123!",
		EnrollmentToken: "test_enrollment_token",
	}

	ctx := context.Background()

	_, cleanup1, err := setupUnifiedServer(ctx, cfg)
	if err != nil {
		t.Fatalf("first setupUnifiedServer failed: %v", err)
	}

	keyPath := filepath.Join(tempDir, masterKeyFileName)
	firstKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("expected master key file to be created on first boot: %v", err)
	}
	cleanup1()

	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("stat master key file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected master key file mode 0600, got %v", perm)
	}

	_, cleanup2, err := setupUnifiedServer(ctx, cfg)
	if err != nil {
		t.Fatalf("second setupUnifiedServer failed: %v", err)
	}
	defer cleanup2()

	secondKey, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed to re-read master key file: %v", err)
	}

	if string(firstKey) != string(secondKey) {
		t.Fatalf("expected master key to persist unchanged across restarts against the same data dir")
	}
}

type testResponseWriter struct {
	header     http.Header
	statusCode int
	body       []byte
}

func (w *testResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *testResponseWriter) Write(b []byte) (int, error) {
	w.body = append(w.body, b...)
	return len(b), nil
}

func (w *testResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}
