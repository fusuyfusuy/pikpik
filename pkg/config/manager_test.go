package config_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/config"
	"github.com/fusuycorp/pikpik/pkg/crypto"
	"github.com/fusuycorp/pikpik/pkg/store"
)

func TestConfigManager_4TierCascadingResolution(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config_test.db"))
	if err != nil {
		t.Fatalf("Failed to open store: %v", err)
	}
	defer st.Close()

	vault, err := crypto.NewAESVault("master-secret-key-32-chars-length!")
	if err != nil {
		t.Fatalf("Failed to create vault: %v", err)
	}

	mgr := config.NewConfigManager(st, vault)

	orgID := "org_1"
	projID := "proj_1"
	stageID := "stage_1"
	svcID := "svc_1"

	// Tier 1: Organization
	_ = st.EnvVars().Set(ctx, &store.EnvVar{
		ScopeTier:  store.TierOrg,
		ResourceID: orgID,
		Key:        "SENTRY_DSN",
		ValueEncrypted: "sentry.io/123",
		IsSecret:   false,
	})
	_ = st.EnvVars().Set(ctx, &store.EnvVar{
		ScopeTier:  store.TierOrg,
		ResourceID: orgID,
		Key:        "LOG_LEVEL",
		ValueEncrypted: "info",
		IsSecret:   false,
	})

	// Tier 2: Project (overrides LOG_LEVEL)
	_ = st.EnvVars().Set(ctx, &store.EnvVar{
		ScopeTier:  store.TierProject,
		ResourceID: projID,
		Key:        "LOG_LEVEL",
		ValueEncrypted: "debug",
		IsSecret:   false,
	})
	_ = st.EnvVars().Set(ctx, &store.EnvVar{
		ScopeTier:  store.TierProject,
		ResourceID: projID,
		Key:        "POSTGRES_HOST",
		ValueEncrypted: "pg-cluster",
		IsSecret:   false,
	})

	// Tier 3: Stage
	_ = st.EnvVars().Set(ctx, &store.EnvVar{
		ScopeTier:  store.TierStage,
		ResourceID: stageID,
		Key:        "NODE_ENV",
		ValueEncrypted: "production",
		IsSecret:   false,
	})

	// Tier 4: Service (overrides LOG_LEVEL, defines encrypted secret DB_PASS, references other vars)
	encPass, err := vault.EncryptString(ctx, "super-secret-password")
	if err != nil {
		t.Fatalf("Failed to encrypt pass: %v", err)
	}

	_ = st.EnvVars().Set(ctx, &store.EnvVar{
		ScopeTier:      store.TierService,
		ResourceID:     svcID,
		Key:            "DB_PASS",
		ValueEncrypted: encPass,
		IsSecret:       true,
	})
	_ = st.EnvVars().Set(ctx, &store.EnvVar{
		ScopeTier:      store.TierService,
		ResourceID:     svcID,
		Key:            "LOG_LEVEL",
		ValueEncrypted: "trace",
		IsSecret:       false,
	})
	_ = st.EnvVars().Set(ctx, &store.EnvVar{
		ScopeTier:      store.TierService,
		ResourceID:     svcID,
		Key:            "DATABASE_URL",
		ValueEncrypted: "postgres://user:${DB_PASS}@${POSTGRES_HOST}:5432/db",
		IsSecret:       false,
	})

	// Resolve
	resolved, err := mgr.ResolveHierarchy(ctx, orgID, projID, stageID, svcID)
	if err != nil {
		t.Fatalf("ResolveHierarchy failed: %v", err)
	}

	// Verify overrides
	if resolved.Variables["LOG_LEVEL"] != "trace" {
		t.Errorf("LOG_LEVEL override failed: got %q, want 'trace'", resolved.Variables["LOG_LEVEL"])
	}
	if resolved.Variables["SENTRY_DSN"] != "sentry.io/123" {
		t.Errorf("Org-level SENTRY_DSN missing: got %q", resolved.Variables["SENTRY_DSN"])
	}
	if resolved.Variables["NODE_ENV"] != "production" {
		t.Errorf("Stage-level NODE_ENV missing: got %q", resolved.Variables["NODE_ENV"])
	}
	if resolved.Variables["DB_PASS"] != "super-secret-password" {
		t.Errorf("Decrypted DB_PASS mismatch: got %q", resolved.Variables["DB_PASS"])
	}

	expectedDBURL := "postgres://user:super-secret-password@pg-cluster:5432/db"
	if resolved.Variables["DATABASE_URL"] != expectedDBURL {
		t.Errorf("DATABASE_URL mismatch: got %q, want %q", resolved.Variables["DATABASE_URL"], expectedDBURL)
	}

	// Test Secret Masker
	masker := mgr.BuildMasker(resolved)
	maskedLog := masker.Mask("Connecting using super-secret-password on pg-cluster")
	expectedLog := "Connecting using [REDACTED] on pg-cluster"
	if maskedLog != expectedLog {
		t.Errorf("Masker output mismatch: got %q, want %q", maskedLog, expectedLog)
	}
}

func TestConfigManager_NonSecretWithV1Prefix(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "config_v1_test.db"))
	if err != nil {
		t.Fatalf("Failed to open store: %v", err)
	}
	defer st.Close()

	vault, err := crypto.NewAESVault("master-secret-key-32-chars-length!")
	if err != nil {
		t.Fatalf("Failed to create vault: %v", err)
	}

	mgr := config.NewConfigManager(st, vault)
	svcID := "svc_v1_test"

	// Non-secret variable with v1: prefix (e.g. API version or schema identifier)
	_ = st.EnvVars().Set(ctx, &store.EnvVar{
		ScopeTier:      store.TierService,
		ResourceID:     svcID,
		Key:            "API_VERSION",
		ValueEncrypted: "v1:alpha:2026",
		IsSecret:       false,
	})

	resolved, err := mgr.ResolveHierarchy(ctx, "", "", "", svcID)
	if err != nil {
		t.Fatalf("ResolveHierarchy failed for non-secret with v1: prefix: %v", err)
	}

	if resolved.Variables["API_VERSION"] != "v1:alpha:2026" {
		t.Errorf("Expected 'v1:alpha:2026', got %q", resolved.Variables["API_VERSION"])
	}
	if resolved.Secrets["API_VERSION"] != false {
		t.Errorf("Expected API_VERSION IsSecret=false, got true")
	}
}

