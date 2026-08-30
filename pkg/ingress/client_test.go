package ingress_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/ingress"
)

// TestCaddyClient_PutRoute_Sub15ms validates Invariant 3: atomic route mutation latency.
func TestCaddyClient_PutRoute_Sub15ms(t *testing.T) {
	// Mock Caddy Admin API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/id/route_svc_test_app_example_com" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`true`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, 2*time.Second)

	spec := ingress.RouteSpec{
		ID:           "route_svc_test_app_example_com",
		ServiceID:    "svc_test",
		Hosts:        []string{"app.example.com"},
		UpstreamDial: "paas_svc_test:3000",
		EnableHSTS:   true,
	}

	caddyRoute := ingress.BuildCaddyRoute(spec)

	ctx := context.Background()
	start := time.Now()
	err := client.PutRoute(ctx, spec.ID, caddyRoute)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if duration > 15*time.Millisecond {
		t.Errorf("Invariant 3 Violation: mutation latency %v exceeded 15ms limit", duration)
	}
}

// TestCaddyClient_Reconciliation_Idempotent verifies full config rebuild from SQLite state.
func TestCaddyClient_Reconciliation_Idempotent(t *testing.T) {
	var loadCalls int
	var lastPayload []byte
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/load" {
			mu.Lock()
			loadCalls++
			var err error
			lastPayload, err = io.ReadAll(r.Body)
			mu.Unlock()
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, 2*time.Second)
	manager := ingress.NewIngressManager(client, nil)

	routes := []ingress.RouteSpec{
		{ID: "route_1", Hosts: []string{"a.domain.com"}, UpstreamDial: "svc1:8080"},
		{ID: "route_2", Hosts: []string{"b.domain.com"}, UpstreamDial: "svc2:8080"},
	}
	tlsCfg := ingress.GlobalTLSConfig{
		AdminEmail:          "admin@example.com",
		OnDemandAskEndpoint: "http://127.0.0.1:8080/api/internal/ingress/ask",
	}

	ctx := context.Background()
	if err := manager.ReconcileAll(ctx, routes, tlsCfg); err != nil {
		t.Fatalf("reconcile 1 failed: %v", err)
	}
	if err := manager.ReconcileAll(ctx, routes, tlsCfg); err != nil {
		t.Fatalf("reconcile 2 failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if loadCalls != 2 {
		t.Errorf("expected 2 load calls, got %d", loadCalls)
	}

	var parsed ingress.CaddyConfig
	if err := json.Unmarshal(lastPayload, &parsed); err != nil {
		t.Fatalf("failed to unmarshal caddy config: %v", err)
	}

	serverRoutes := parsed.Apps.HTTP.Servers["srv0"].Routes
	if len(serverRoutes) != 2 {
		t.Errorf("expected 2 routes in reconciled config, got %d", len(serverRoutes))
	}
}

// TestCaddyClient_CRUD covers the full CRUD lifecycle on Caddy REST endpoints.
func TestCaddyClient_CRUD(t *testing.T) {
	routesStore := make(map[string]ingress.CaddyRoute)
	var mu sync.RWMutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/config/":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"admin":{"listen":"127.0.0.1:2019"}}`))

		case r.Method == http.MethodPut && len(r.URL.Path) > 4 && r.URL.Path[:4] == "/id/":
			id := r.URL.Path[4:]
			var route ingress.CaddyRoute
			if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"error":"bad json"}`))
				return
			}
			routesStore[id] = route
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`true`))

		case r.Method == http.MethodGet && len(r.URL.Path) > 4 && r.URL.Path[:4] == "/id/":
			id := r.URL.Path[4:]
			route, ok := routesStore[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(`{"error":"unknown id"}`))
				return
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(route)

		case r.Method == http.MethodDelete && len(r.URL.Path) > 4 && r.URL.Path[:4] == "/id/":
			id := r.URL.Path[4:]
			if _, ok := routesStore[id]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(routesStore, id)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`true`))

		case r.Method == http.MethodGet && r.URL.Path == "/config/apps/http/servers/srv0/routes":
			var list []ingress.CaddyRoute
			for _, r := range routesStore {
				list = append(list, r)
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(list)

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, time.Second)
	ctx := context.Background()

	// 1. Ping
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("ping failed: %v", err)
	}

	// 2. PutRoute
	spec := ingress.RouteSpec{
		ID:           "route_svc_api_example_com",
		ServiceID:    "svc_api",
		Hosts:        []string{"api.example.com"},
		UpstreamDial: "backend:8080",
		EnableHSTS:   true,
	}
	route := ingress.BuildCaddyRoute(spec)
	if err := client.PutRoute(ctx, spec.ID, route); err != nil {
		t.Fatalf("put route failed: %v", err)
	}

	// 3. GetRoute
	fetched, err := client.GetRoute(ctx, spec.ID)
	if err != nil {
		t.Fatalf("get route failed: %v", err)
	}
	if fetched.ID != spec.ID {
		t.Errorf("expected route ID %s, got %s", spec.ID, fetched.ID)
	}

	// 4. ListRoutes
	list, err := client.ListRoutes(ctx)
	if err != nil {
		t.Fatalf("list routes failed: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 route, got %d", len(list))
	}

	// 5. DeleteRoute
	if err := client.DeleteRoute(ctx, spec.ID); err != nil {
		t.Fatalf("delete route failed: %v", err)
	}

	// 6. GetRoute on deleted route returns ErrRouteNotFound
	_, err = client.GetRoute(ctx, spec.ID)
	if !errors.Is(err, ingress.ErrRouteNotFound) {
		t.Errorf("expected ErrRouteNotFound, got %v", err)
	}

	// 7. DeleteRoute on missing route returns ErrRouteNotFound
	err = client.DeleteRoute(ctx, "non_existent_route")
	if !errors.Is(err, ingress.ErrRouteNotFound) {
		t.Errorf("expected ErrRouteNotFound, got %v", err)
	}
}

// TestCaddyClient_ErrorHandling verifies network failure and error mapping.
func TestCaddyClient_ErrorHandling(t *testing.T) {
	// Connect to non-existent endpoint
	client := ingress.NewCaddyClient("http://127.0.0.1:59999", 100*time.Millisecond)
	ctx := context.Background()

	err := client.Ping(ctx)
	if !errors.Is(err, ingress.ErrCaddyUnreachable) {
		t.Errorf("expected ErrCaddyUnreachable, got: %v", err)
	}

	err = client.PutRoute(ctx, "test", ingress.CaddyRoute{})
	if !errors.Is(err, ingress.ErrCaddyUnreachable) {
		t.Errorf("expected ErrCaddyUnreachable, got: %v", err)
	}

	err = client.PutRoute(ctx, "", ingress.CaddyRoute{})
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for empty id, got: %v", err)
	}
}

// TestCaddyClient_GetRawConfig verifies retrieval of raw JSON config.
func TestCaddyClient_GetRawConfig(t *testing.T) {
	mockJSON := `{"admin":{"listen":"127.0.0.1:2019"},"apps":{}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/config/" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(mockJSON))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, 2*time.Second)
	ctx := context.Background()

	raw, err := client.GetRawConfig(ctx)
	if err != nil {
		t.Fatalf("unexpected error getting raw config: %v", err)
	}
	if string(raw) != mockJSON {
		t.Errorf("expected %s, got %s", mockJSON, string(raw))
	}
}
