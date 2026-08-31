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

// TestTrafficSplit_BuildRoute verifies canonical Caddy JSON structure for direct 1:1 upstream routing.
func TestTrafficSplit_BuildRoute(t *testing.T) {
	cfg := ingress.TrafficSplitConfig{
		Domain:         "app.example.com",
		StableUpstream: "blue:3000",
		Paths:          []string{"/api"},
	}
	route := ingress.BuildTrafficSplitRoute(cfg)

	if route.ID != "route_split_app_example_com" {
		t.Errorf("expected route ID route_split_app_example_com, got %s", route.ID)
	}
	if len(route.Match) == 0 || route.Match[0].Host[0] != "app.example.com" {
		t.Errorf("expected host match app.example.com")
	}
	if len(route.Match[0].Path) == 0 || route.Match[0].Path[0] != "/api" {
		t.Errorf("expected path match /api")
	}

	// Extract inner handlers
	subroute := route.Handle[0]
	inner := subroute.Routes[0].Handle
	if len(inner) != 2 {
		t.Fatalf("expected 2 inner handlers, got %d", len(inner))
	}
	if inner[0].Handler != "headers" {
		t.Errorf("expected headers handler")
	}
	rp := inner[1]
	if rp.Handler != "reverse_proxy" {
		t.Fatalf("expected reverse_proxy handler")
	}
	if len(rp.Upstreams) != 1 || rp.Upstreams[0].Dial != "blue:3000" {
		t.Errorf("expected 1 upstream blue:3000, got %+v", rp.Upstreams)
	}
	if rp.LoadBalancing.SelectionPolicy.Policy != "round_robin" {
		t.Errorf("expected round_robin policy, got %s", rp.LoadBalancing.SelectionPolicy.Policy)
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
	}

	ctx := context.Background()
	start := time.Now()
	err := client.SetTrafficSplit(ctx, "app.example.com", cfg)
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error setting traffic route: %v", err)
	}
	if duration > 15*time.Millisecond {
		t.Errorf("traffic route update took %v, exceeding 15ms target", duration)
	}

	mu.Lock()
	defer mu.Unlock()
	if capturedRoute.ID != "route_split_app_example_com" {
		t.Errorf("expected captured route ID route_split_app_example_com, got %s", capturedRoute.ID)
	}
}

// TestTrafficSplit_GetAndRemove verifies route retrieval and deletion.
func TestTrafficSplit_GetAndRemove(t *testing.T) {
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
		case r.Method == http.MethodDelete && len(r.URL.Path) > 4 && r.URL.Path[:4] == "/id/":
			id := r.URL.Path[4:]
			delete(routesStore, id)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`true`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, time.Second)
	ctx := context.Background()

	cfg := ingress.TrafficSplitConfig{
		Domain:         "routed.pikpik.dev",
		StableUpstream: "prod_v1:3000",
	}
	if err := client.SetTrafficSplit(ctx, "routed.pikpik.dev", cfg); err != nil {
		t.Fatalf("SetTrafficSplit failed: %v", err)
	}

	fetched, err := client.GetTrafficSplit(ctx, "routed.pikpik.dev")
	if err != nil {
		t.Fatalf("GetTrafficSplit failed: %v", err)
	}
	if fetched.StableUpstream != "prod_v1:3000" {
		t.Errorf("expected stable upstream prod_v1:3000, got %s", fetched.StableUpstream)
	}

	if err := client.RemoveTrafficSplit(ctx, "routed.pikpik.dev"); err != nil {
		t.Fatalf("RemoveTrafficSplit failed: %v", err)
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

	// Empty upstream
	err = client.SetTrafficSplit(ctx, "test.com", ingress.TrafficSplitConfig{
		Domain:         "test.com",
		StableUpstream: "",
	})
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for empty upstream, got %v", err)
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
	}

	if err := mgr.SetTrafficSplit(ctx, "mgr.pikpik.dev", cfg); err != nil {
		t.Fatalf("DefaultIngressManager.SetTrafficSplit failed: %v", err)
	}

	fetched, err := mgr.GetTrafficSplit(ctx, "mgr.pikpik.dev")
	if err != nil {
		t.Fatalf("DefaultIngressManager.GetTrafficSplit failed: %v", err)
	}
	if fetched.StableUpstream != "blue:8080" {
		t.Errorf("expected upstream blue:8080, got %s", fetched.StableUpstream)
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
			}
			_ = client.SetTrafficSplit(ctx, domain, cfg)
			_, _ = client.GetTrafficSplit(ctx, domain)
		}(i)
	}
	wg.Wait()
}

// TestTrafficSplit_WeightedDistributions verifies 0%, 50/50%, 90/10%, 100%, and multi-target distributions.
func TestTrafficSplit_WeightedDistributions(t *testing.T) {
	tests := []struct {
		name           string
		cfg            ingress.TrafficSplitConfig
		expectedPolicy string
		expectedCount  int
		expectedWeights map[string]int
	}{
		{
			name: "100% Single Stable",
			cfg: ingress.TrafficSplitConfig{
				Domain: "single.example.com",
				Splits: []ingress.UpstreamWeight{
					{Upstream: "app-prod:8080", Weight: 100},
				},
			},
			expectedPolicy: "round_robin",
			expectedCount:  1,
			expectedWeights: map[string]int{},
		},
		{
			name: "50/50% Blue/Green Split",
			cfg: ingress.TrafficSplitConfig{
				Domain: "bluegreen.example.com",
				Splits: []ingress.UpstreamWeight{
					{Upstream: "app-blue:8080", Weight: 50},
					{Upstream: "app-green:8080", Weight: 50},
				},
			},
			expectedPolicy: "weighted_round_robin",
			expectedCount:  2,
			expectedWeights: map[string]int{
				"app-blue:8080":  50,
				"app-green:8080": 50,
			},
		},
		{
			name: "90/10% Canary Split",
			cfg: ingress.TrafficSplitConfig{
				Domain: "canary.example.com",
				Splits: []ingress.UpstreamWeight{
					{Upstream: "app-stable:8080", Weight: 90},
					{Upstream: "app-canary:8080", Weight: 10},
				},
			},
			expectedPolicy: "weighted_round_robin",
			expectedCount:  2,
			expectedWeights: map[string]int{
				"app-stable:8080": 90,
				"app-canary:8080": 10,
			},
		},
		{
			name: "0/100% Cutover Split",
			cfg: ingress.TrafficSplitConfig{
				Domain: "cutover.example.com",
				Splits: []ingress.UpstreamWeight{
					{Upstream: "app-legacy:8080", Weight: 0},
					{Upstream: "app-v2:8080", Weight: 100},
				},
			},
			expectedPolicy: "weighted_round_robin",
			expectedCount:  2,
			expectedWeights: map[string]int{
				"app-legacy:8080": 0,
				"app-v2:8080":     100,
			},
		},
		{
			name: "70/20/10% Multi-Canary Split",
			cfg: ingress.TrafficSplitConfig{
				Domain: "multicanary.example.com",
				Splits: []ingress.UpstreamWeight{
					{Upstream: "app-v1:8080", Weight: 70},
					{Upstream: "app-v2:8080", Weight: 20},
					{Upstream: "app-v3-exp:8080", Weight: 10},
				},
			},
			expectedPolicy: "weighted_round_robin",
			expectedCount:  3,
			expectedWeights: map[string]int{
				"app-v1:8080":     70,
				"app-v2:8080":     20,
				"app-v3-exp:8080": 10,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			route := ingress.BuildTrafficSplitRoute(tt.cfg)
			if len(route.Handle) == 0 || len(route.Handle[0].Routes) == 0 {
				t.Fatalf("expected subroute handler")
			}
			var rp *ingress.CaddyRouteHandler
			for _, h := range route.Handle[0].Routes[0].Handle {
				if h.Handler == "reverse_proxy" {
					rp = &h
					break
				}
			}
			if rp == nil {
				t.Fatalf("missing reverse_proxy handler")
			}
			if len(rp.Upstreams) != tt.expectedCount {
				t.Errorf("expected %d upstreams, got %d", tt.expectedCount, len(rp.Upstreams))
			}
			if rp.LoadBalancing == nil || rp.LoadBalancing.SelectionPolicy == nil {
				t.Fatalf("missing load balancing selection policy")
			}
			if rp.LoadBalancing.SelectionPolicy.Policy != tt.expectedPolicy {
				t.Errorf("expected policy %s, got %s", tt.expectedPolicy, rp.LoadBalancing.SelectionPolicy.Policy)
			}
			for upstream, expectedWeight := range tt.expectedWeights {
				actualWeight := rp.LoadBalancing.SelectionPolicy.Weights[upstream]
				if actualWeight != expectedWeight {
					t.Errorf("expected weight for %s to be %d, got %d", upstream, expectedWeight, actualWeight)
				}
			}
		})
	}
}

// TestTrafficSplit_AdvancedValidationErrors checks negative weights and sum of weights <= 0.
func TestTrafficSplit_AdvancedValidationErrors(t *testing.T) {
	client := ingress.NewCaddyClient("http://127.0.0.1:2019", time.Second)
	ctx := context.Background()

	// Negative weight
	err := client.SetTrafficSplit(ctx, "app.test", ingress.TrafficSplitConfig{
		Domain: "app.test",
		Splits: []ingress.UpstreamWeight{
			{Upstream: "app:80", Weight: -5},
		},
	})
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for negative weight, got %v", err)
	}

	// All weights zero
	err = client.SetTrafficSplit(ctx, "app.test", ingress.TrafficSplitConfig{
		Domain: "app.test",
		Splits: []ingress.UpstreamWeight{
			{Upstream: "app1:80", Weight: 0},
			{Upstream: "app2:80", Weight: 0},
		},
	})
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for sum of weights == 0, got %v", err)
	}

	// Empty upstream name
	err = client.SetTrafficSplit(ctx, "app.test", ingress.TrafficSplitConfig{
		Domain: "app.test",
		Splits: []ingress.UpstreamWeight{
			{Upstream: "   ", Weight: 100},
		},
	})
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for empty upstream name, got %v", err)
	}
}
