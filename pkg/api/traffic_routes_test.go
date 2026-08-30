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

// TestAPIRoutes_AppDeploy verifies in-place rolling deployment via POST /api/v1/apps/{app_id}/deploy
func TestAPIRoutes_AppDeploy(t *testing.T) {
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

	// Trigger in-place deployment with new image
	deployReq := api.DeployAppRequest{
		Image: "web:v2.0.0",
	}
	resp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/"+app.ID+"/deploy", deployReq)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for deploy, got %d", resp.StatusCode)
	}

	// Verify app status and image updated
	updatedApp, err := ctrl.GetApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("failed to get app: %v", err)
	}
	if updatedApp.Image != "web:v2.0.0" {
		t.Errorf("expected updated image web:v2.0.0, got %s", updatedApp.Image)
	}
	if updatedApp.Status != "running" {
		t.Errorf("expected app status 'running', got %s", updatedApp.Status)
	}

	// Error case: Non-existent app
	notFoundResp := authedJSONRequest(t, server.URL, token, "POST", "/api/v1/apps/non-existent-id/deploy", deployReq)
	if notFoundResp.StatusCode != http.StatusNotFound && notFoundResp.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected 404 or 500 for missing app, got %d", notFoundResp.StatusCode)
	}
}

func TestAPIRoutes_CaddyDiagnosticsAndAsk(t *testing.T) {
	server, _, token := setupAPITestServer()
	defer server.Close()

	// 1. GET /api/v1/ingress/caddy/config
	resp := authedJSONRequest(t, server.URL, token, "GET", "/api/v1/ingress/caddy/config", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for caddy config, got %d", resp.StatusCode)
	}

	var diagResp api.Response[api.CaddyDiagnosticsDTO]
	if err := json.NewDecoder(resp.Body).Decode(&diagResp); err != nil {
		t.Fatalf("failed to decode caddy diagnostics: %v", err)
	}
	if diagResp.Data.Status != "online" {
		t.Errorf("expected online status, got %s", diagResp.Data.Status)
	}
	if diagResp.Data.AdminURL != "http://127.0.0.1:2019" {
		t.Errorf("expected admin URL http://127.0.0.1:2019, got %s", diagResp.Data.AdminURL)
	}

	// 2. GET /api/v1/ingress/ask (empty domain should return 403)
	askResp, err := http.Get(server.URL + "/api/v1/ingress/ask")
	if err != nil {
		t.Fatalf("ask request failed: %v", err)
	}
	if askResp.StatusCode != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden for empty domain ask, got %d", askResp.StatusCode)
	}
}
