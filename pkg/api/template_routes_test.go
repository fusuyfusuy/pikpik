package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/templates"
)

func setupTemplateTestServer() (*httptest.Server, *api.DefaultController, string) {
	ctrl := api.NewDefaultController(api.ControllerDependencies{})
	gw := api.NewAPIGateway(ctrl, nil, nil)
	server := httptest.NewServer(gw)

	token := "pik_live_testtoken_adminkey12345"
	return server, ctrl, token
}

func doJSONRequest(t *testing.T, serverURL, token, method, path string, body any) *http.Response {
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

	req, err := http.NewRequestWithContext(context.Background(), method, serverURL+path, bodyReader)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request to %s %s failed: %v", method, path, err)
	}
	return resp
}

func TestTemplateRoutes_List(t *testing.T) {
	server, _, token := setupTemplateTestServer()
	defer server.Close()

	// 1. Unauthenticated request -> 401 Unauthorized
	unauthResp := doJSONRequest(t, server.URL, "", "GET", "/api/v1/templates", nil)
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for missing token, got %d", unauthResp.StatusCode)
	}

	// 2. Authenticated list all templates -> 200 OK with >= 21 templates
	resp := doJSONRequest(t, server.URL, token, "GET", "/api/v1/templates", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var result api.Response[[]templates.Template]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}
	if !result.Success {
		t.Errorf("expected success=true, got false")
	}
	if len(result.Data) < 22 {
		t.Errorf("expected at least 22 templates, got %d", len(result.Data))
	}

	// 3. Filter by category=Databases
	catResp := doJSONRequest(t, server.URL, token, "GET", "/api/v1/templates?category=Databases", nil)
	if catResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for category filter, got %d", catResp.StatusCode)
	}
	var catResult api.Response[[]templates.Template]
	_ = json.NewDecoder(catResp.Body).Decode(&catResult)
	if len(catResult.Data) != 8 {
		t.Errorf("expected 8 database templates, got %d", len(catResult.Data))
	}

	// 4. Search by query ?search=pocketbase
	searchResp := doJSONRequest(t, server.URL, token, "GET", "/api/v1/templates?search=pocketbase", nil)
	if searchResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for search query, got %d", searchResp.StatusCode)
	}
	var searchResult api.Response[[]templates.Template]
	_ = json.NewDecoder(searchResp.Body).Decode(&searchResult)
	if len(searchResult.Data) == 0 || searchResult.Data[0].ID != "pocketbase" {
		t.Errorf("expected pocketbase in search results, got %v", searchResult.Data)
	}
}

func TestTemplateRoutes_GetDetails(t *testing.T) {
	server, _, token := setupTemplateTestServer()
	defer server.Close()

	// 1. Get existing template details -> 200 OK
	resp := doJSONRequest(t, server.URL, token, "GET", "/api/v1/templates/pocketbase", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var result api.Response[templates.Template]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if result.Data.ID != "pocketbase" {
		t.Errorf("expected template ID pocketbase, got %s", result.Data.ID)
	}
	if result.Data.DefaultPort != 8090 {
		t.Errorf("expected default port 8090, got %d", result.Data.DefaultPort)
	}
	if len(result.Data.EnvVars) == 0 {
		t.Errorf("expected env var schema in template details")
	}

	// 2. Get non-existent template -> 404 Not Found
	notFoundResp := doJSONRequest(t, server.URL, token, "GET", "/api/v1/templates/unknown-template-id", nil)
	if notFoundResp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d", notFoundResp.StatusCode)
	}

	var errResult api.ErrorResponse
	_ = json.NewDecoder(notFoundResp.Body).Decode(&errResult)
	if errResult.Error.Code != api.ErrCodeNotFound {
		t.Errorf("expected error code %s, got %s", api.ErrCodeNotFound, errResult.Error.Code)
	}
}

func TestTemplateRoutes_Deploy(t *testing.T) {
	server, _, token := setupTemplateTestServer()
	defer server.Close()

	// 1. Deploy template -> 201 Created
	deployReq := templates.DeployTemplateRequest{
		Name:      "prod-postgres",
		ProjectID: "default",
		StageID:   "production",
		Variables: map[string]string{
			"POSTGRES_USER": "myadmin",
			"POSTGRES_DB":   "mydb",
		},
	}

	resp := doJSONRequest(t, server.URL, token, "POST", "/api/v1/templates/postgres-16/deploy", deployReq)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}

	var result api.Response[templates.DeployTemplateResponse]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode deploy response: %v", err)
	}
	if result.Data.Name != "prod-postgres" {
		t.Errorf("expected name 'prod-postgres', got '%s'", result.Data.Name)
	}
	if result.Data.TemplateID != "postgres-16" {
		t.Errorf("expected template 'postgres-16', got '%s'", result.Data.TemplateID)
	}
	if result.Data.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", result.Data.Status)
	}
	if result.Data.ResolvedVariables["POSTGRES_PASSWORD"] != "[REDACTED]" {
		t.Errorf("expected '[REDACTED]', got '%s'", result.Data.ResolvedVariables["POSTGRES_PASSWORD"])
	}

	// 2. Deploy non-existent template -> 404 Not Found
	badResp := doJSONRequest(t, server.URL, token, "POST", "/api/v1/templates/nonexistent-template/deploy", deployReq)
	if badResp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for nonexistent template deploy, got %d", badResp.StatusCode)
	}
}
