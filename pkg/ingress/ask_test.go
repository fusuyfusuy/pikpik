package ingress_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/ingress"
	"github.com/fusuycorp/pikpik/pkg/store"
)

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open test store: %v", err)
	}
	t.Cleanup(func() {
		_ = st.Close()
	})
	return st
}

// TestOnDemandTLS_AskEndpoint_SecurityWhitelist verifies DoS/Rate-Limit protection.
func TestOnDemandTLS_AskEndpoint_SecurityWhitelist(t *testing.T) {
	mockDB := map[string]bool{
		"allowed.customdomain.com": true,
		"tenant-app.pikpik.dev":    true,
	}

	validator := ingress.DomainValidatorFunc(func(ctx context.Context, domain string) (bool, error) {
		return mockDB[domain], nil
	})

	server := httptest.NewServer(ingress.NewAskHandler(validator))
	defer server.Close()

	// 1. Whitelisted domain test
	respAllowed, err := http.Get(server.URL + "?domain=allowed.customdomain.com")
	if err != nil || respAllowed.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for allowed domain, got status: %v", respAllowed.StatusCode)
	}

	// 2. Unregistered/Attacker domain test
	respBlocked, err := http.Get(server.URL + "?domain=attacker-domain-12345.com")
	if err != nil || respBlocked.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for unwhitelisted domain, got status: %v", respBlocked.StatusCode)
	}

	// 3. Empty query parameter
	respEmpty, err := http.Get(server.URL + "?domain=")
	if err != nil || respEmpty.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for empty domain, got status: %v", respEmpty.StatusCode)
	}

	// 4. Missing query parameter
	respMissing, err := http.Get(server.URL)
	if err != nil || respMissing.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for missing domain param, got status: %v", respMissing.StatusCode)
	}

	// 5. Nil validator handler
	nilHandlerServer := httptest.NewServer(ingress.NewAskHandler(nil))
	defer nilHandlerServer.Close()

	respNil, err := http.Get(nilHandlerServer.URL + "?domain=allowed.customdomain.com")
	if err != nil || respNil.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for nil validator, got status: %v", respNil.StatusCode)
	}
}

func TestMapDomainValidator(t *testing.T) {
	v := ingress.NewMapDomainValidator([]string{"App.Example.com", "api.pikpik.dev"})
	ctx := context.Background()

	ok, err := v.VerifyDomain(ctx, "app.example.com")
	if err != nil || !ok {
		t.Errorf("expected app.example.com to be verified, got ok=%v, err=%v", ok, err)
	}

	ok, err = v.VerifyDomain(ctx, "API.PIKPIK.DEV")
	if err != nil || !ok {
		t.Errorf("expected case-insensitive match for api.pikpik.dev")
	}

	ok, err = v.VerifyDomain(ctx, "unknown.com")
	if err != nil || ok {
		t.Errorf("expected unknown.com to fail verification")
	}
}

func TestStoreDomainValidator(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	org := &store.Organization{Name: "Org 1", Slug: "org-1"}
	if err := st.Organizations().Create(ctx, org); err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	proj := &store.Project{OrgID: org.ID, Name: "Proj 1", Slug: "proj-1"}
	if err := st.Projects().Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}

	stage := &store.Stage{ProjectID: proj.ID, Name: "Prod", Slug: "prod"}
	if err := st.Stages().Create(ctx, stage); err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	svc := &store.Service{
		ProjectID:     proj.ID,
		StageID:       stage.ID,
		Name:          "Frontend",
		Slug:          "frontend",
		Type:          "app",
		Image:         "nginx:alpine",
		ContainerPort: 80,
		DomainNames:   []string{"app.pikpik.io", "dashboard.pikpik.io"},
		Status:        "running",
	}
	if err := st.Services().Create(ctx, svc); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	validator := ingress.NewStoreDomainValidator(st)

	// Valid active domain
	ok, err := validator.VerifyDomain(ctx, "app.pikpik.io")
	if err != nil || !ok {
		t.Errorf("expected app.pikpik.io to be valid, got ok=%v, err=%v", ok, err)
	}

	// Valid second domain
	ok, err = validator.VerifyDomain(ctx, "DASHBOARD.PIKPIK.IO")
	if err != nil || !ok {
		t.Errorf("expected dashboard.pikpik.io to be valid case-insensitively")
	}

	// Unknown domain
	ok, err = validator.VerifyDomain(ctx, "evil-hacker.com")
	if err != nil || ok {
		t.Errorf("expected evil-hacker.com to be rejected")
	}

	// Stopped service domain rejection
	if err := st.Services().UpdateStatus(ctx, svc.ID, "stopped"); err != nil {
		t.Fatalf("failed to update status: %v", err)
	}

	ok, err = validator.VerifyDomain(ctx, "app.pikpik.io")
	if err != nil || ok {
		t.Errorf("expected stopped service domain to be rejected, got ok=%v", ok)
	}
}
