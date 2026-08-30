package ingress_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/ingress"
)

// TestTrafficSplit_BuildRoute verifies canonical Caddy JSON structure across weight distributions.
func TestTrafficSplit_BuildRoute(t *testing.T) {
	// Case 1: 0% Canary -> 100% Stable, round_robin
	cfg0 := ingress.TrafficSplitConfig{
		Domain:         "app.example.com",
		StableUpstream: "blue:3000",
		CanaryUpstream: "green:3000",
		CanaryPercent:  0,
		Paths:          []string{"/api"},
		Headers: map[string]string{
			"X-Custom-Env": "production",
		},
	}
	route0 := ingress.BuildTrafficSplitRoute(cfg0)

	if route0.ID != "route_split_app_example_com" {
		t.Errorf("expected route ID route_split_app_example_com, got %s", route0.ID)
	}
	if len(route0.Match) == 0 || route0.Match[0].Host[0] != "app.example.com" {
		t.Errorf("expected host match app.example.com")
	}
	if len(route0.Match[0].Path) == 0 || route0.Match[0].Path[0] != "/api" {
		t.Errorf("expected path match /api")
	}

	// Extract inner handlers
	subroute0 := route0.Handle[0]
	inner0 := subroute0.Routes[0].Handle
	if len(inner0) != 2 {
		t.Fatalf("expected 2 inner handlers, got %d", len(inner0))
	}
	if inner0[0].Handler != "headers" || inner0[0].Response.Set["X-Custom-Env"][0] != "production" {
		t.Errorf("expected custom header in headers handler")
	}
	rp0 := inner0[1]
	if rp0.Handler != "reverse_proxy" {
		t.Fatalf("expected reverse_proxy handler")
	}
	if len(rp0.Upstreams) != 1 || rp0.Upstreams[0].Dial != "blue:3000" {
		t.Errorf("expected 1 upstream blue:3000 at 0%% canary, got %+v", rp0.Upstreams)
	}
	if rp0.LoadBalancing.SelectionPolicy.Policy != "round_robin" {
		t.Errorf("expected round_robin for 0%% canary, got %s", rp0.LoadBalancing.SelectionPolicy.Policy)
	}

	// Case 2: 25% Canary -> 75% Stable / 25% Canary, weighted_round_robin
	cfg25 := ingress.TrafficSplitConfig{
		Domain:         "app.example.com",
		StableUpstream: "blue:3000",
		CanaryUpstream: "green:3000",
		CanaryPercent:  25,
	}
	route25 := ingress.BuildTrafficSplitRoute(cfg25)
	rp25 := route25.Handle[0].Routes[0].Handle[1]
	if len(rp25.Upstreams) != 2 {
		t.Fatalf("expected 2 upstreams for 25%% canary, got %d", len(rp25.Upstreams))
	}
	if rp25.Upstreams[0].Dial != "blue:3000" || rp25.Upstreams[0].Weight != 75 {
		t.Errorf("expected blue:3000 weight 75, got %+v", rp25.Upstreams[0])
	}
	if rp25.Upstreams[1].Dial != "green:3000" || rp25.Upstreams[1].Weight != 25 {
		t.Errorf("expected green:3000 weight 25, got %+v", rp25.Upstreams[1])
	}
	if rp25.LoadBalancing.SelectionPolicy.Policy != "weighted_round_robin" {
		t.Errorf("expected weighted_round_robin policy, got %s", rp25.LoadBalancing.SelectionPolicy.Policy)
	}
	if rp25.LoadBalancing.SelectionPolicy.Weights["blue:3000"] != 75 ||
		rp25.LoadBalancing.SelectionPolicy.Weights["green:3000"] != 25 {
		t.Errorf("unexpected selection policy weights: %+v", rp25.LoadBalancing.SelectionPolicy.Weights)
	}

	// Case 3: 100% Canary -> 100% Canary, round_robin
	cfg100 := ingress.TrafficSplitConfig{
		Domain:         "app.example.com",
		StableUpstream: "blue:3000",
		CanaryUpstream: "green:3000",
		CanaryPercent:  100,
	}
	route100 := ingress.BuildTrafficSplitRoute(cfg100)
	rp100 := route100.Handle[0].Routes[0].Handle[1]
	if len(rp100.Upstreams) != 1 || rp100.Upstreams[0].Dial != "green:3000" {
		t.Errorf("expected 1 upstream green:3000 at 100%% canary, got %+v", rp100.Upstreams)
	}
	if rp100.LoadBalancing.SelectionPolicy.Policy != "round_robin" {
		t.Errorf("expected round_robin for 100%% canary, got %s", rp100.LoadBalancing.SelectionPolicy.Policy)
	}
}

// TestTrafficSplit_SetTrafficSplit_Sub15ms verifies latency & payload when calling Caddy Admin REST API.
func TestTrafficSplit_SetTrafficSplit_Sub15ms(t *testing.T) {
	var capturedRoute ingress.CaddyRoute
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/id/route_split_app_example_com" {
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&capturedRoute)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`true`))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, 2*time.Second)

	cfg := ingress.TrafficSplitConfig{
		Domain:         "app.example.com",
		StableUpstream: "app_v1:8080",
		CanaryUpstream: "app_v2:8080",
		CanaryPercent:  20,
	}

	ctx := context.Background()
	start := time.Now()
	err := client.SetTrafficSplit(ctx, "app.example.com", cfg)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error setting traffic split: %v", err)
	}
	if duration > 15*time.Millisecond {
		t.Errorf("traffic split update took %v, exceeding 15ms target", duration)
	}

	mu.Lock()
	defer mu.Unlock()
	if capturedRoute.ID != "route_split_app_example_com" {
		t.Errorf("expected captured route ID route_split_app_example_com, got %s", capturedRoute.ID)
	}
}

// TestTrafficSplit_SetCanaryWeight verifies dynamic weight modification on existing split.
func TestTrafficSplit_SetCanaryWeight(t *testing.T) {
	routesStore := make(map[string]ingress.CaddyRoute)
	var mu sync.RWMutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.Method == http.MethodPut && len(r.URL.Path) > 4 && r.URL.Path[:4] == "/id/":
			id := r.URL.Path[4:]
			var route ingress.CaddyRoute
			_ = json.NewDecoder(r.Body).Decode(&route)
			routesStore[id] = route
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`true`))
		case r.Method == http.MethodGet && len(r.URL.Path) > 4 && r.URL.Path[:4] == "/id/":
			id := r.URL.Path[4:]
			route, ok := routesStore[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(route)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, time.Second)
	ctx := context.Background()

	// Initial configuration: 10% Canary
	initialCfg := ingress.TrafficSplitConfig{
		Domain:         "canary.pikpik.dev",
		StableUpstream: "prod_v1:3000",
		CanaryUpstream: "prod_v2:3000",
		CanaryPercent:  10,
	}
	if err := client.SetTrafficSplit(ctx, "canary.pikpik.dev", initialCfg); err != nil {
		t.Fatalf("initial SetTrafficSplit failed: %v", err)
	}

	// Dynamic shift to 50%
	if err := client.SetCanaryWeight(ctx, "canary.pikpik.dev", 50); err != nil {
		t.Fatalf("SetCanaryWeight(50) failed: %v", err)
	}

	fetched, err := client.GetTrafficSplit(ctx, "canary.pikpik.dev")
	if err != nil {
		t.Fatalf("GetTrafficSplit failed: %v", err)
	}
	if fetched.CanaryPercent != 50 {
		t.Errorf("expected canary percent 50, got %d", fetched.CanaryPercent)
	}

	// Dynamic shift to 100% (Complete Green cutover)
	if err := client.SetCanaryWeight(ctx, "canary.pikpik.dev", 100); err != nil {
		t.Fatalf("SetCanaryWeight(100) failed: %v", err)
	}

	fetched, err = client.GetTrafficSplit(ctx, "canary.pikpik.dev")
	if err != nil {
		t.Fatalf("GetTrafficSplit failed: %v", err)
	}
	if fetched.CanaryPercent != 100 {
		t.Errorf("expected canary percent 100, got %d", fetched.CanaryPercent)
	}
}

// TestTrafficSplit_ValidationErrors validates boundary constraints.
func TestTrafficSplit_ValidationErrors(t *testing.T) {
	client := ingress.NewCaddyClient("http://127.0.0.1:2019", time.Second)
	ctx := context.Background()

	// Empty domain
	err := client.SetTrafficSplit(ctx, "", ingress.TrafficSplitConfig{StableUpstream: "a:80"})
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for empty domain, got %v", err)
	}

	// CanaryPercent < 0
	err = client.SetTrafficSplit(ctx, "test.com", ingress.TrafficSplitConfig{
		Domain:         "test.com",
		StableUpstream: "a:80",
		CanaryPercent:  -5,
	})
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for negative percent, got %v", err)
	}

	// CanaryPercent > 100
	err = client.SetTrafficSplit(ctx, "test.com", ingress.TrafficSplitConfig{
		Domain:         "test.com",
		StableUpstream: "a:80",
		CanaryPercent:  105,
	})
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for percent > 100, got %v", err)
	}

	// CanaryPercent > 0 but empty CanaryUpstream
	err = client.SetTrafficSplit(ctx, "test.com", ingress.TrafficSplitConfig{
		Domain:         "test.com",
		StableUpstream: "a:80",
		CanaryUpstream: "",
		CanaryPercent:  20,
	})
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for missing canary upstream, got %v", err)
	}

	// SetCanaryWeight on unknown domain
	err = client.SetCanaryWeight(ctx, "unknown-domain.com", 25)
	if !errors.Is(err, ingress.ErrRouteNotFound) && !errors.Is(err, ingress.ErrCaddyUnreachable) {
		t.Errorf("expected ErrRouteNotFound or ErrCaddyUnreachable, got %v", err)
	}
}

// TestTrafficSplit_IngressManager tests DefaultIngressManager delegation.
func TestTrafficSplit_IngressManager(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`true`))
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, time.Second)
	mgr := ingress.NewIngressManager(client, nil)
	ctx := context.Background()

	cfg := ingress.TrafficSplitConfig{
		Domain:         "mgr.pikpik.dev",
		StableUpstream: "blue:8080",
		CanaryUpstream: "green:8080",
		CanaryPercent:  30,
	}

	if err := mgr.SetTrafficSplit(ctx, "mgr.pikpik.dev", cfg); err != nil {
		t.Fatalf("DefaultIngressManager.SetTrafficSplit failed: %v", err)
	}

	if err := mgr.SetCanaryWeight(ctx, "mgr.pikpik.dev", 60); err != nil {
		t.Fatalf("DefaultIngressManager.SetCanaryWeight failed: %v", err)
	}

	fetched, err := mgr.GetTrafficSplit(ctx, "mgr.pikpik.dev")
	if err != nil {
		t.Fatalf("DefaultIngressManager.GetTrafficSplit failed: %v", err)
	}
	if fetched.CanaryPercent != 60 {
		t.Errorf("expected 60%% canary, got %d", fetched.CanaryPercent)
	}
}

// TestTrafficSplit_Concurrent ensures thread safety under the Go race detector.
func TestTrafficSplit_Concurrent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`true`))
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, time.Second)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			domain := "concurrent.pikpik.dev"
			cfg := ingress.TrafficSplitConfig{
				Domain:         domain,
				StableUpstream: "blue:3000",
				CanaryUpstream: "green:3000",
				CanaryPercent:  idx % 100,
			}
			_ = client.SetTrafficSplit(ctx, domain, cfg)
			_ = client.SetCanaryWeight(ctx, domain, (idx*5)%100)
			_, _ = client.GetTrafficSplit(ctx, domain)
		}(i)
	}
	wg.Wait()
}
