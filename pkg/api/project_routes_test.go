package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/api"
)

func TestProjectAndTagRoutes_CRUD(t *testing.T) {
	ctrl := api.NewDefaultController(api.ControllerDependencies{})
	gw := api.NewAPIGateway(ctrl, nil, nil)
	server := httptest.NewServer(gw)
	defer server.Close()

	token := "pik_live_testadminkey1234567890"

	// 1. Create Project
	createProjBody, _ := json.Marshal(api.CreateProjectRequest{
		Name:        "Billing Service",
		Description: "Payment gateways and invoicing",
		Tags:        []string{"finance", "team:payments"},
	})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/projects", bytes.NewReader(createProjBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create project: %v, status: %d", err, resp.StatusCode)
	}

	var projResp api.Response[api.ProjectDTO]
	_ = json.NewDecoder(resp.Body).Decode(&projResp)
	projID := projResp.Data.ID
	if projID == "" || projResp.Data.Name != "Billing Service" {
		t.Fatalf("unexpected project data: %+v", projResp.Data)
	}

	// 2. List Projects
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/projects", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to list projects: %v, status: %d", err, resp.StatusCode)
	}
	var listResp api.Response[[]api.ProjectDTO]
	_ = json.NewDecoder(resp.Body).Decode(&listResp)
	if len(listResp.Data) < 1 {
		t.Fatalf("expected at least 1 project, got %d", len(listResp.Data))
	}

	// 3. Create App assigned to Project with Tags
	createAppBody, _ := json.Marshal(api.CreateAppRequest{
		ProjectID: projID,
		Name:      "stripe-webhook",
		Image:     "pikpik/stripe:v1",
		Replicas:  1,
		Tags:      []string{"payments", "webhooks", "env:prod"},
	})
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/apps", bytes.NewReader(createAppBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create app: %v, status: %d", err, resp.StatusCode)
	}

	var appResp api.Response[api.App]
	_ = json.NewDecoder(resp.Body).Decode(&appResp)
	if appResp.Data.ProjectID != projID || len(appResp.Data.Tags) != 3 {
		t.Fatalf("unexpected app data: %+v", appResp.Data)
	}

	// 4. Query Tags Aggregation
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/tags", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to get tags: %v, status: %d", err, resp.StatusCode)
	}
	var tagResp api.Response[[]api.TagSummary]
	_ = json.NewDecoder(resp.Body).Decode(&tagResp)
	if len(tagResp.Data) < 3 {
		t.Fatalf("expected at least 3 aggregated tags, got %d", len(tagResp.Data))
	}

	// 5. Filter Apps by Tag
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/apps?tag=payments", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to filter apps by tag: %v, status: %d", err, resp.StatusCode)
	}
	var filteredApps api.Response[[]api.App]
	_ = json.NewDecoder(resp.Body).Decode(&filteredApps)
	if len(filteredApps.Data) != 1 || filteredApps.Data[0].Name != "stripe-webhook" {
		t.Fatalf("expected 1 matching app for tag 'payments', got %d", len(filteredApps.Data))
	}
}

func TestInspectComposeRoutes(t *testing.T) {
	ctrl := api.NewDefaultController(api.ControllerDependencies{})
	gw := api.NewAPIGateway(ctrl, nil, nil)
	server := httptest.NewServer(gw)
	defer server.Close()

	token := "pik_live_testadminkey1234567890"

	rawYAML := `version: '3.8'
services:
  web:
    image: nginx:alpine
    ports:
      - "8080:80"
    environment:
      - API_SECRET=${BACKEND_SECRET}
      - DB_HOST=${DB_HOST:-postgres}
    deploy:
      replicas: 2
`

	inspectBody, _ := json.Marshal(api.InspectComposeRequest{
		ComposeYAML: rawYAML,
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/apps/inspect-compose", bytes.NewReader(inspectBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to inspect compose: %v, status: %d", err, resp.StatusCode)
	}

	var inspectResp api.Response[api.InspectComposeResponse]
	_ = json.NewDecoder(resp.Body).Decode(&inspectResp)

	if len(inspectResp.Data.Services) != 1 || inspectResp.Data.Services[0].Name != "web" {
		t.Fatalf("expected 1 service 'web', got %+v", inspectResp.Data.Services)
	}
	if inspectResp.Data.SuggestedRuntime != "swarm" {
		t.Errorf("expected suggested runtime 'swarm', got '%s'", inspectResp.Data.SuggestedRuntime)
	}
	if len(inspectResp.Data.Variables) != 2 {
		t.Fatalf("expected 2 detected variables, got %d", len(inspectResp.Data.Variables))
	}
}
