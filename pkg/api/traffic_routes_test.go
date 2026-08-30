package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/ingress"
)

func setupAPITestServer() (*httptest.Server, *api.DefaultController, string) {
	caddyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`true`))
	}))

	caddyClient := ingress.NewCaddyClient(caddyServer.URL, 0)
	ingressMgr := ingress.NewIngressManager(caddyClient, nil)

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		IngressManager: ingressMgr,
	})
	gw := api.NewAPIGateway(ctrl, nil, nil)
	server := httptest.NewServer(gw)

	token := "pik_live_testtoken_adminkey12345"
	return server, ctrl, token
}

func authedJSONRequest(t *testing.T, serverURL, token, method, path string, body any) *http.Response {
	var bodyReader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("failed to marshal request body: %v", err)
		}
		bodyReader = bytes.NewReader(data)
	} else {
		bodyReader = bytes.NewReader([]byte{})
	}

	req, err := http.NewRequest(method, serverURL+path, bodyReader)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request to %s %s failed: %v", method, path, err)
	}
	return resp
}

// TestAPIRoutes_TrafficSplitting verifies GET and POST /api/v1/apps/{app_id}/traffic
func TestAPIRoutes_TrafficSplitting(t *testing.T) {
	server, ctrl, token := setupAPITestServer()
	defer server.Close()

	ctx := contextBackground()
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:     "web-service",
		Image:    "web:v1",
		Replicas: 1,
		Domains:  []string{"web.example.com"},
	})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	// 1. GET /api/v1/apps/{app_id}/traffic (Initial / Default)
	getResp := authedJSONRequest(t, server.URL, token, "GET", "/api/v1/apps/"+app.ID+"/traffic", nil)
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", getResp.StatusCode)
	}
	var getResult api.Response[api.TrafficSplitDTO]
	_ = json.NewDecoder(getResp.Body).Decode(&getResult)
	if getResult.Data.CanaryPercent != 0 {
		t.Errorf("expected default canary percent 0, got %d", getResult.Data.CanaryPercent)
	}

	// 2. POST /api/v1/apps/{app_id}/traffic (Update split to 30% Canary)
	setReq := api.SetTrafficSplitRequest{
		Domain:         "web.example.com",
		StableUpstream: "web_blue:3000",
		CanaryUpstream: "web_green:3000",
		CanaryPercent:  30,
		Headers: map[string]string{
			"X-Canary": "true",
		},
	}
	postResp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", setReq)
	if postResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for set traffic, got %d", postResp.StatusCode)
	}
	var postResult api.Response[api.TrafficSplitDTO]
	_ = json.NewDecoder(postResp.Body).Decode(&postResult)
	if postResult.Data.CanaryPercent != 30 {
		t.Errorf("expected 30%% canary, got %d", postResult.Data.CanaryPercent)
	}
	if postResult.Data.StableUpstream != "web_blue:3000" {
		t.Errorf("expected stable upstream web_blue:3000, got %s", postResult.Data.StableUpstream)
	}

	// 3. GET /api/v1/apps/{app_id}/traffic (Verify persisted state)
	getResp2 := authedJSONRequest(t, server.URL, token, "GET", "/api/v1/apps/"+app.ID+"/traffic", nil)
	if getResp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", getResp2.StatusCode)
	}
	var getResult2 api.Response[api.TrafficSplitDTO]
	_ = json.NewDecoder(getResp2.Body).Decode(&getResult2)
	if getResult2.Data.CanaryPercent != 30 {
		t.Errorf("expected 30%% canary, got %d", getResult2.Data.CanaryPercent)
	}

	// 4. POST with invalid canary percent (< 0) -> 400 Bad Request
	badReq := api.SetTrafficSplitRequest{
		CanaryPercent: -10,
	}
	badResp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", badReq)
	if badResp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for negative canary percent, got %d", badResp.StatusCode)
	}

	// 5. POST with invalid canary percent (> 100) -> 400 Bad Request
	badReq2 := api.SetTrafficSplitRequest{
		CanaryPercent: 150,
	}
	badResp2 := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/traffic", badReq2)
	if badResp2.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for canary percent > 100, got %d", badResp2.StatusCode)
	}
}

// TestAPIRoutes_BlueGreen verifies POST /api/v1/apps/{app_id}/blue-green
func TestAPIRoutes_BlueGreen(t *testing.T) {
	server, ctrl, token := setupAPITestServer()
	defer server.Close()

	ctx := contextBackground()
	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:     "bg-service",
		Image:    "bg:v1.0.0",
		Replicas: 1,
		Domains:  []string{"bg.example.com"},
	})
	if err != nil {
		t.Fatalf("failed to create test app: %v", err)
	}

	// Trigger Blue-Green rollout
	bgReq := api.BlueGreenDeployRequest{
		Image:           "bg:v2.0.0",
		Domain:          "bg.example.com",
		ContainerPort:   8080,
		HealthCheckPath: "/healthz",
		ProbeTimeoutSec: 10,
		DrainPeriodSec:  1,
	}

	resp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/blue-green", bgReq)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for blue-green deploy, got %d", resp.StatusCode)
	}

	var result api.Response[api.BlueGreenDeployResponse]
	_ = json.NewDecoder(resp.Body).Decode(&result)

	if result.Data.AppID != app.ID {
		t.Errorf("expected AppID %s, got %s", app.ID, result.Data.AppID)
	}
	if result.Data.Status != "success" {
		t.Errorf("expected status success, got %s", result.Data.Status)
	}
	if result.Data.ActiveContainerID == "" {
		t.Errorf("expected non-empty active container ID")
	}

	// Verify app image was updated
	updatedApp, err := ctrl.GetApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("failed to get app: %v", err)
	}
	if updatedApp.Image != "bg:v2.0.0" {
		t.Errorf("expected app image bg:v2.0.0, got %s", updatedApp.Image)
	}

	// Error case: Non-existent app
	notFoundResp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/non-existent-id/blue-green", bgReq)
	if notFoundResp.StatusCode != http.StatusInternalServerError && notFoundResp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404/500 for missing app, got %d", notFoundResp.StatusCode)
	}
}

func contextBackground() context.Context {
	return context.Background()
}
