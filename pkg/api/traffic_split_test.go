package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/api"
)

// TestAPIRoutes_GetAppTraffic_Default verifies that an app without custom splits returns 100% default upstream.
func TestAPIRoutes_GetAppTraffic_Default(t *testing.T) {
	server, ctrl, token := setupAPITestServer()
	defer server.Close()

	ctx := context.Background()
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:     "web-service",
		Image:    "web:v1.0.0",
		Replicas: 1,
		Domains:  []string{"web.example.com"},
	})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	resp := authedJSONRequest(t, server.URL, token, "GET", "/api/v1/apps/"+app.ID+"/traffic", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var res api.Response[api.TrafficSplitResponse]
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.Data.AppID != app.ID {
		t.Errorf("expected app ID %s, got %s", app.ID, res.Data.AppID)
	}
	if len(res.Data.Splits) != 1 || res.Data.Splits[0].Weight != 100 {
		t.Errorf("expected default 100%% split, got %+v", res.Data.Splits)
	}
}

// TestAPIRoutes_SetAppTraffic_50_50_BlueGreen verifies equal 50/50% Blue/Green traffic split.
func TestAPIRoutes_SetAppTraffic_50_50_BlueGreen(t *testing.T) {
	server, ctrl, token := setupAPITestServer()
	defer server.Close()

	ctx := context.Background()
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:     "blue-green-service",
		Image:    "bg:1.0",
		Replicas: 1,
	})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	reqPayload := api.SetTrafficSplitRequest{
		Splits: []api.UpstreamWeight{
			{Upstream: "bg-blue:8080", Weight: 50},
			{Upstream: "bg-green:8080", Weight: 50},
		},
	}

	resp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", reqPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var res api.Response[api.TrafficSplitResponse]
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(res.Data.Splits) != 2 {
		t.Fatalf("expected 2 splits, got %d", len(res.Data.Splits))
	}
	if res.Data.Splits[0].Weight != 50 || res.Data.Splits[1].Weight != 50 {
		t.Errorf("expected 50/50 split weights, got %+v", res.Data.Splits)
	}

	// Verify persistence on subsequent GET
	getResp := authedJSONRequest(t, server.URL, token, "GET", "/api/v1/apps/"+app.ID+"/traffic", nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK on GET, got %d", getResp.StatusCode)
	}

	var getRes api.Response[api.TrafficSplitResponse]
	if err := json.NewDecoder(getResp.Body).Decode(&getRes); err != nil {
		t.Fatalf("failed to decode GET response: %v", err)
	}
	if len(getRes.Data.Splits) != 2 || getRes.Data.Splits[0].Weight != 50 || getRes.Data.Splits[1].Weight != 50 {
		t.Errorf("expected persistent 50/50 split, got %+v", getRes.Data.Splits)
	}
}

// TestAPIRoutes_SetAppTraffic_90_10_Canary verifies 90/10% Canary traffic split.
func TestAPIRoutes_SetAppTraffic_90_10_Canary(t *testing.T) {
	server, ctrl, token := setupAPITestServer()
	defer server.Close()

	ctx := context.Background()
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:     "canary-service",
		Image:    "canary:v1",
		Replicas: 1,
	})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	reqPayload := api.SetTrafficSplitRequest{
		Splits: []api.UpstreamWeight{
			{Upstream: "app-prod:8080", Weight: 90},
			{Upstream: "app-canary:8080", Weight: 10},
		},
	}

	resp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", reqPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var res api.Response[api.TrafficSplitResponse]
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(res.Data.Splits) != 2 {
		t.Fatalf("expected 2 splits, got %d", len(res.Data.Splits))
	}
	if res.Data.Splits[0].Weight != 90 || res.Data.Splits[1].Weight != 10 {
		t.Errorf("expected 90/10 split weights, got %+v", res.Data.Splits)
	}
}

// TestAPIRoutes_SetAppTraffic_0_100_Cutover verifies 0% legacy / 100% new cutover.
func TestAPIRoutes_SetAppTraffic_0_100_Cutover(t *testing.T) {
	server, ctrl, token := setupAPITestServer()
	defer server.Close()

	ctx := context.Background()
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:     "cutover-service",
		Image:    "cutover:v1",
		Replicas: 1,
	})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	reqPayload := api.SetTrafficSplitRequest{
		Splits: []api.UpstreamWeight{
			{Upstream: "app-old:8080", Weight: 0},
			{Upstream: "app-v2:8080", Weight: 100},
		},
	}

	resp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", reqPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var res api.Response[api.TrafficSplitResponse]
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(res.Data.Splits) != 2 || res.Data.Splits[0].Weight != 0 || res.Data.Splits[1].Weight != 100 {
		t.Errorf("expected 0/100 split, got %+v", res.Data.Splits)
	}
}

// TestAPIRoutes_SetAppTraffic_100_Single verifies single upstream 100% split.
func TestAPIRoutes_SetAppTraffic_100_Single(t *testing.T) {
	server, ctrl, token := setupAPITestServer()
	defer server.Close()

	ctx := context.Background()
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:     "single-target-service",
		Image:    "single:1.0",
		Replicas: 1,
	})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	reqPayload := api.SetTrafficSplitRequest{
		Splits: []api.UpstreamWeight{
			{Upstream: "app-target:3000", Weight: 100},
		},
	}

	resp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", reqPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var res api.Response[api.TrafficSplitResponse]
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(res.Data.Splits) != 1 || res.Data.Splits[0].Upstream != "app-target:3000" || res.Data.Splits[0].Weight != 100 {
		t.Errorf("expected 100%% app-target:3000 split, got %+v", res.Data.Splits)
	}
}

// TestAPIRoutes_SetAppTraffic_Reset verifies resetting traffic distribution to 100% default stable upstream.
func TestAPIRoutes_SetAppTraffic_Reset(t *testing.T) {
	server, ctrl, token := setupAPITestServer()
	defer server.Close()

	ctx := context.Background()
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:     "reset-service",
		Image:    "reset:v1",
		Replicas: 1,
	})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	// First set a 90/10 split
	_ = authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", api.SetTrafficSplitRequest{
		Splits: []api.UpstreamWeight{
			{Upstream: "app-prod:8080", Weight: 90},
			{Upstream: "app-canary:8080", Weight: 10},
		},
	})

	// Now send reset request
	resetResp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", api.SetTrafficSplitRequest{
		Reset: true,
	})
	if resetResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for reset, got %d", resetResp.StatusCode)
	}

	var res api.Response[api.TrafficSplitResponse]
	if err := json.NewDecoder(resetResp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode reset response: %v", err)
	}

	if len(res.Data.Splits) != 1 || res.Data.Splits[0].Weight != 100 {
		t.Errorf("expected reset to 100%% default split, got %+v", res.Data.Splits)
	}
}

// TestAPIRoutes_SetAppTraffic_RawArrayPayload verifies that raw array `[]UpstreamWeight` is also accepted.
func TestAPIRoutes_SetAppTraffic_RawArrayPayload(t *testing.T) {
	server, ctrl, token := setupAPITestServer()
	defer server.Close()

	ctx := context.Background()
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:     "raw-array-service",
		Image:    "raw:v1",
		Replicas: 1,
	})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	rawPayload := []api.UpstreamWeight{
		{Upstream: "app-a:8080", Weight: 70},
		{Upstream: "app-b:8080", Weight: 30},
	}

	resp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", rawPayload)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for raw array, got %d", resp.StatusCode)
	}

	var res api.Response[api.TrafficSplitResponse]
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(res.Data.Splits) != 2 || res.Data.Splits[0].Weight != 70 || res.Data.Splits[1].Weight != 30 {
		t.Errorf("expected 70/30 split, got %+v", res.Data.Splits)
	}
}

// TestAPIRoutes_SetAppTraffic_ValidationErrors checks 400 and 404 validation cases.
func TestAPIRoutes_SetAppTraffic_ValidationErrors(t *testing.T) {
	server, ctrl, token := setupAPITestServer()
	defer server.Close()

	ctx := context.Background()
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:     "val-service",
		Image:    "val:v1",
		Replicas: 1,
	})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	// 1. App Not Found (404)
	notFoundResp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/non-existent-app-id/traffic", api.SetTrafficSplitRequest{
		Splits: []api.UpstreamWeight{
			{Upstream: "app:80", Weight: 100},
		},
	})
	if notFoundResp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", notFoundResp.StatusCode)
	}

	// 2. Negative Weight (400)
	negResp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", api.SetTrafficSplitRequest{
		Splits: []api.UpstreamWeight{
			{Upstream: "app:80", Weight: -10},
		},
	})
	if negResp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for negative weight, got %d", negResp.StatusCode)
	}

	// 3. Sum of Weights Zero (400)
	zeroResp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", api.SetTrafficSplitRequest{
		Splits: []api.UpstreamWeight{
			{Upstream: "app1:80", Weight: 0},
			{Upstream: "app2:80", Weight: 0},
		},
	})
	if zeroResp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for sum == 0, got %d", zeroResp.StatusCode)
	}

	// 4. Empty Upstream Dial (400)
	emptyResp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", api.SetTrafficSplitRequest{
		Splits: []api.UpstreamWeight{
			{Upstream: "   ", Weight: 100},
		},
	})
	if emptyResp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for empty upstream, got %d", emptyResp.StatusCode)
	}
}
