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
	"github.com/fusuycorp/pikpik/pkg/store"
)

func TestIngressManager_ApplyAndRemoveRoute(t *testing.T) {
	routesStore := make(map[string]ingress.CaddyRoute)
	var mu sync.RWMutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/config/":
			w.WriteHeader(http.StatusOK)
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
	validator := ingress.NewMapDomainValidator([]string{"app.example.com"})
	mgr := ingress.NewIngressManager(client, validator)
	ctx := context.Background()

	// HealthCheck
	if err := mgr.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck failed: %v", err)
	}

	// ApplyRoute without explicit ID -> auto-generated ID
	spec := ingress.RouteSpec{
		ServiceID:    "svc_test",
		Hosts:        []string{"app.example.com"},
		UpstreamDial: "paas_svc_test:3000",
		EnableHSTS:   true,
	}
	if err := mgr.ApplyRoute(ctx, spec); err != nil {
		t.Fatalf("ApplyRoute failed: %v", err)
	}

	expectedID := "route_svc_test_app_example_com"
	fetched, err := mgr.GetRoute(ctx, expectedID)
	if err != nil {
		t.Fatalf("GetRoute failed: %v", err)
	}
	if fetched.UpstreamDial != "paas_svc_test:3000" {
		t.Errorf("expected upstream paas_svc_test:3000, got %s", fetched.UpstreamDial)
	}

	// ListRoutes
	routes, err := mgr.ListRoutes(ctx)
	if err != nil {
		t.Fatalf("ListRoutes failed: %v", err)
	}
	if len(routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(routes))
	}

	// VerifyDomain
	ok, err := mgr.VerifyDomain(ctx, "app.example.com")
	if err != nil || !ok {
		t.Errorf("expected domain verification to succeed, got ok=%v, err=%v", ok, err)
	}

	ok, err = mgr.VerifyDomain(ctx, "unknown.com")
	if ok || !errors.Is(err, ingress.ErrDomainNotWhitelisted) {
		t.Errorf("expected ErrDomainNotWhitelisted for unknown.com, got err=%v", err)
	}

	// RemoveRoute
	if err := mgr.RemoveRoute(ctx, expectedID); err != nil {
		t.Fatalf("RemoveRoute failed: %v", err)
	}

	_, err = mgr.GetRoute(ctx, expectedID)
	if !errors.Is(err, ingress.ErrRouteNotFound) {
		t.Errorf("expected ErrRouteNotFound after deletion, got %v", err)
	}
}

func TestIngressManager_ValidationErrors(t *testing.T) {
	client := ingress.NewCaddyClient("http://127.0.0.1:2019", time.Second)
	mgr := ingress.NewIngressManager(client, nil)
	ctx := context.Background()

	// Missing UpstreamDial
	err := mgr.ApplyRoute(ctx, ingress.RouteSpec{Hosts: []string{"app.example.com"}})
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for missing UpstreamDial, got: %v", err)
	}

	// Missing Hosts and PathPrefixes
	err = mgr.ApplyRoute(ctx, ingress.RouteSpec{UpstreamDial: "app:3000"})
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for missing hosts, got: %v", err)
	}

	// Empty ID on remove
	err = mgr.RemoveRoute(ctx, "")
	if !errors.Is(err, ingress.ErrInvalidRoutePayload) {
		t.Errorf("expected ErrInvalidRoutePayload for empty routeID on RemoveRoute, got: %v", err)
	}
}

func TestIngressManager_ReconcileFromStore(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()

	org := &store.Organization{Name: "Org Rec", Slug: "org-rec"}
	if err := st.Organizations().Create(ctx, org); err != nil {
		t.Fatalf("failed to create org: %v", err)
	}
	proj := &store.Project{OrgID: org.ID, Name: "Proj Rec", Slug: "proj-rec"}
	if err := st.Projects().Create(ctx, proj); err != nil {
		t.Fatalf("failed to create project: %v", err)
	}
	stage := &store.Stage{ProjectID: proj.ID, Name: "Prod", Slug: "prod"}
	if err := st.Stages().Create(ctx, stage); err != nil {
		t.Fatalf("failed to create stage: %v", err)
	}

	// Active service with 2 domains
	svc1 := &store.Service{
		ProjectID:     proj.ID,
		StageID:       stage.ID,
		Name:          "Web App",
		Slug:          "web-app",
		Type:          "app",
		Image:         "node:alpine",
		ContainerPort: 3000,
		DomainNames:   []string{"app.pikpik.dev", "console.pikpik.dev"},
		Status:        "running",
	}
	if err := st.Services().Create(ctx, svc1); err != nil {
		t.Fatalf("failed to create svc1: %v", err)
	}

	// Active service with 1 domain
	svc2 := &store.Service{
		ProjectID:     proj.ID,
		StageID:       stage.ID,
		Name:          "API Gateway",
		Slug:          "api-gw",
		Type:          "app",
		Image:         "golang:alpine",
		ContainerPort: 8080,
		DomainNames:   []string{"api.pikpik.dev"},
		Status:        "running",
	}
	if err := st.Services().Create(ctx, svc2); err != nil {
		t.Fatalf("failed to create svc2: %v", err)
	}

	// Stopped service (should NOT be routed)
	svc3 := &store.Service{
		ProjectID:     proj.ID,
		StageID:       stage.ID,
		Name:          "Stopped Worker",
		Slug:          "stopped-worker",
		Type:          "worker",
		Image:         "worker:latest",
		ContainerPort: 9000,
		DomainNames:   []string{"worker.pikpik.dev"},
		Status:        "stopped",
	}
	if err := st.Services().Create(ctx, svc3); err != nil {
		t.Fatalf("failed to create svc3: %v", err)
	}

	var lastConfig ingress.CaddyConfig
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/load" {
			mu.Lock()
			_ = json.NewDecoder(r.Body).Decode(&lastConfig)
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, time.Second)
	mgr := ingress.NewIngressManager(client, nil)

	tlsCfg := ingress.GlobalTLSConfig{
		AdminEmail:          "admin@pikpik.dev",
		OnDemandAskEndpoint: "http://127.0.0.1:8080/api/internal/ingress/ask",
	}

	if err := mgr.ReconcileFromStore(ctx, st, tlsCfg); err != nil {
		t.Fatalf("ReconcileFromStore failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	routes := lastConfig.Apps.HTTP.Servers["srv0"].Routes
	if len(routes) != 3 {
		t.Fatalf("expected 3 active routes in Caddy config, got %d", len(routes))
	}

	// Verify all 3 routes belong to svc1 or svc2
	for _, r := range routes {
		if len(r.Match) == 0 || len(r.Match[0].Host) == 0 {
			t.Errorf("route missing host match: %+v", r)
		}
		host := r.Match[0].Host[0]
		if host != "app.pikpik.dev" && host != "console.pikpik.dev" && host != "api.pikpik.dev" {
			t.Errorf("unexpected host in routes: %s", host)
		}
	}
}

func TestIngressManager_ReconcilePreservesActiveDirectRouting(t *testing.T) {
	var lastConfig ingress.CaddyConfig
	var mu sync.Mutex
	routesStore := make(map[string]ingress.CaddyRoute)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/load":
			_ = json.NewDecoder(r.Body).Decode(&lastConfig)
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && len(r.URL.Path) > 4 && r.URL.Path[:4] == "/id/":
			id := r.URL.Path[4:]
			var route ingress.CaddyRoute
			_ = json.NewDecoder(r.Body).Decode(&route)
			routesStore[id] = route
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`true`))
		case r.Method == http.MethodDelete && len(r.URL.Path) > 4 && r.URL.Path[:4] == "/id/":
			id := r.URL.Path[4:]
			delete(routesStore, id)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`true`))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	client := ingress.NewCaddyClient(server.URL, time.Second)
	mgr := ingress.NewIngressManager(client, nil)
	ctx := context.Background()

	// 1. Set active direct route on api.example.com
	splitCfg := ingress.TrafficSplitConfig{
		Domain:         "api.example.com",
		StableUpstream: "api_blue:8080",
	}
	if err := mgr.SetTrafficSplit(ctx, "api.example.com", splitCfg); err != nil {
		t.Fatalf("SetTrafficSplit failed: %v", err)
	}

	// 2. Perform ReconcileAll with static routes
	staticRoutes := []ingress.RouteSpec{
		{
			ID:           "route_app_example_com",
			ServiceID:    "svc_app",
			Hosts:        []string{"app.example.com"},
			UpstreamDial: "app:3000",
			EnableHSTS:   true,
		},
		{
			ID:           "route_api_example_com",
			ServiceID:    "svc_api",
			Hosts:        []string{"api.example.com"},
			UpstreamDial: "api_blue:8080",
			EnableHSTS:   true,
		},
	}
	tlsCfg := ingress.GlobalTLSConfig{AdminEmail: "admin@example.com"}

	if err := mgr.ReconcileAll(ctx, staticRoutes, tlsCfg); err != nil {
		t.Fatalf("ReconcileAll failed: %v", err)
	}

	mu.Lock()
	loadedRoutes := lastConfig.Apps.HTTP.Servers["srv0"].Routes
	mu.Unlock()

	if len(loadedRoutes) != 2 {
		t.Fatalf("expected 2 loaded routes, got %d", len(loadedRoutes))
	}

	// Verify that api.example.com preserved the split route structure
	var apiRoute *ingress.CaddyRoute
	for i := range loadedRoutes {
		if len(loadedRoutes[i].Match) > 0 && len(loadedRoutes[i].Match[0].Host) > 0 && loadedRoutes[i].Match[0].Host[0] == "api.example.com" {
			apiRoute = &loadedRoutes[i]
			break
		}
	}
	if apiRoute == nil {
		t.Fatalf("api.example.com route not found in reconciled config")
	}
	if apiRoute.ID != ingress.GenerateTrafficSplitRouteID("api.example.com") {
		t.Errorf("expected split route ID %s, got %s", ingress.GenerateTrafficSplitRouteID("api.example.com"), apiRoute.ID)
	}

	// Check upstream inside subroute handler
	subroute := apiRoute.Handle[0]
	innerRoute := subroute.Routes[0]
	var rpHandler *ingress.CaddyRouteHandler
	for _, h := range innerRoute.Handle {
		if h.Handler == "reverse_proxy" {
			hCopy := h
			rpHandler = &hCopy
			break
		}
	}
	if rpHandler == nil {
		t.Fatalf("reverse_proxy handler not found on split route")
	}
	if len(rpHandler.Upstreams) != 1 || rpHandler.Upstreams[0].Dial != "api_blue:8080" {
		t.Fatalf("expected 1 upstream api_blue:8080 on split route, got %+v", rpHandler.Upstreams)
	}

	// 3. Remove traffic route explicitly and reconcile again
	if err := mgr.RemoveTrafficSplit(ctx, "api.example.com"); err != nil {
		t.Fatalf("RemoveTrafficSplit failed: %v", err)
	}

	if err := mgr.ReconcileAll(ctx, staticRoutes, tlsCfg); err != nil {
		t.Fatalf("second ReconcileAll failed: %v", err)
	}

	mu.Lock()
	loadedRoutesAfter := lastConfig.Apps.HTTP.Servers["srv0"].Routes
	mu.Unlock()

	var apiRouteAfter *ingress.CaddyRoute
	for i := range loadedRoutesAfter {
		if len(loadedRoutesAfter[i].Match) > 0 && len(loadedRoutesAfter[i].Match[0].Host) > 0 && loadedRoutesAfter[i].Match[0].Host[0] == "api.example.com" {
			apiRouteAfter = &loadedRoutesAfter[i]
			break
		}
	}
	if apiRouteAfter == nil {
		t.Fatalf("api.example.com route not found after removing split")
	}
	if apiRouteAfter.ID == ingress.GenerateTrafficSplitRouteID("api.example.com") {
		t.Errorf("route ID should revert from split ID, got %s", apiRouteAfter.ID)
	}
}
