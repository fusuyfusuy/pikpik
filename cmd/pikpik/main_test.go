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
