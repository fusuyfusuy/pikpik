package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/api"
)

func TestStackNetworkVolumeRoutes_E2E(t *testing.T) {
	ctrl := api.NewDefaultController(api.ControllerDependencies{})
	gw := api.NewAPIGateway(ctrl, nil, nil)
	server := httptest.NewServer(gw)
	defer server.Close()

	token := "pik_live_testadminkey1234567890"

	// 1. Create Stack
	createStackBody, _ := json.Marshal(api.CreateStackRequest{
		Name: "blog-stack",
		ComposeYAML: `version: '3.8'
services:
  web:
    image: nginx:alpine
  db:
    image: postgres:16
`,
	})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/stacks", bytes.NewReader(createStackBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create stack: %v, status: %d", err, resp.StatusCode)
	}

	var stackResp api.Response[api.Stack]
	_ = json.NewDecoder(resp.Body).Decode(&stackResp)
	stackID := stackResp.Data.ID
	if stackID == "" || stackResp.Data.Name != "blog-stack" {
		t.Fatalf("unexpected stack data: %+v", stackResp.Data)
	}
	if len(stackResp.Data.Services) != 2 {
		t.Errorf("expected 2 services in stack, got %d", len(stackResp.Data.Services))
	}

	// 2. Get Stack
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/stacks/"+stackID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to get stack: %v, status: %d", err, resp.StatusCode)
	}

	// 3. Deploy Stack
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/stacks/"+stackID+"/deploy", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to deploy stack: %v, status: %d", err, resp.StatusCode)
	}

	// 4. Restart Stack
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/stacks/"+stackID+"/restart", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to restart stack: %v, status: %d", err, resp.StatusCode)
	}

	// 5. Stop Stack
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/stacks/"+stackID+"/stop", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to stop stack: %v, status: %d", err, resp.StatusCode)
	}

	// 6. Get Stack Logs
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/stacks/"+stackID+"/logs", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to get stack logs: %v, status: %d", err, resp.StatusCode)
	}

	// 7. Delete Stack
	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/v1/stacks/"+stackID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to delete stack: %v, status: %d", err, resp.StatusCode)
	}

	// 8. Create Network
	createNetBody, _ := json.Marshal(api.CreateNetworkRequest{
		Name:   "custom_mesh",
		Driver: "bridge",
		Scope:  "project",
	})
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/networks", bytes.NewReader(createNetBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create network: %v, status: %d", err, resp.StatusCode)
	}
	var netResp api.Response[api.NetworkDTO]
	_ = json.NewDecoder(resp.Body).Decode(&netResp)
	netID := netResp.Data.ID
	if netID == "" || netResp.Data.Name != "custom_mesh" {
		t.Fatalf("unexpected network data: %+v", netResp.Data)
	}

	// 9. List & Get Network
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/networks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to list networks: %v, status: %d", err, resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/networks/"+netID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to get network: %v, status: %d", err, resp.StatusCode)
	}

	// 10. Prune Networks
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/networks/prune", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to prune networks: %v, status: %d", err, resp.StatusCode)
	}

	// 11. Delete Network
	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/v1/networks/"+netID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to delete network: %v, status: %d", err, resp.StatusCode)
	}

	// 12. Create Volume
	createVolBody, _ := json.Marshal(api.CreateVolumeRequest{
		Name:   "db_storage",
		Driver: "local",
	})
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/volumes", bytes.NewReader(createVolBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("failed to create volume: %v, status: %d", err, resp.StatusCode)
	}
	var volResp api.Response[api.VolumeDTO]
	_ = json.NewDecoder(resp.Body).Decode(&volResp)
	volID := volResp.Data.ID
	if volID == "" || volResp.Data.Name != "db_storage" {
		t.Fatalf("unexpected volume data: %+v", volResp.Data)
	}

	// 13. List & Get Volume
	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/volumes", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to list volumes: %v, status: %d", err, resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, server.URL+"/api/v1/volumes/"+volID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to get volume: %v, status: %d", err, resp.StatusCode)
	}

	// 14. Prune Volumes
	req, _ = http.NewRequest(http.MethodPost, server.URL+"/api/v1/volumes/prune", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to prune volumes: %v, status: %d", err, resp.StatusCode)
	}

	// 15. Delete Volume
	req, _ = http.NewRequest(http.MethodDelete, server.URL+"/api/v1/volumes/"+volID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("failed to delete volume: %v, status: %d", err, resp.StatusCode)
	}
}
