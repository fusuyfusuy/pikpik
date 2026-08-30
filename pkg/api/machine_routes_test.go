package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/store"
)

func TestMachineRoutes_CRUD_And_Join(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_api_machines.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer st.Close()

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
	})

	hub := api.NewWebSocketHub()
	gw := api.NewAPIGateway(ctrl, hub, nil)
	token := "pik_live_testadminkey1234567890"

	ctx := context.Background()

	// Seed a machine
	mch := &store.ManagedMachine{
		ID:            "mch_alpha",
		Hostname:      "worker-node-alpha",
		Role:          "worker",
		PublicIP:      "198.51.100.20",
		PrivateIP:     "10.0.0.20",
		OSKernel:      "Linux 6.8",
		CPUArch:       "amd64",
		DockerVersion: "26.1",
		AgentVersion:  "1.0.0",
		Status:        "online",
	}
	if err := st.Machines().Create(ctx, mch); err != nil {
		t.Fatalf("seed machine error: %v", err)
	}

	// 1. GET /api/v1/machines
	req := httptest.NewRequest("GET", "/api/v1/machines", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	gw.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/machines returned status %d: %s", w.Code, w.Body.String())
	}
	var listResp api.Response[[]api.MachineDTO]
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list error: %v", err)
	}
	if len(listResp.Data) != 1 || listResp.Data[0].Hostname != "worker-node-alpha" {
		t.Fatalf("unexpected machine list: %+v", listResp.Data)
	}

	// 2. GET /api/v1/machines/{id}
	req = httptest.NewRequest("GET", "/api/v1/machines/mch_alpha", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	gw.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/machines/mch_alpha returned %d: %s", w.Code, w.Body.String())
	}
	var getResp api.Response[api.MachineDTO]
	if err := json.Unmarshal(w.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("unmarshal get error: %v", err)
	}
	if getResp.Data.ID != "mch_alpha" || getResp.Data.PublicIP != "198.51.100.20" {
		t.Fatalf("unexpected machine get data: %+v", getResp.Data)
	}

	// 3. GET /api/v1/machines/{id}/metrics
	req = httptest.NewRequest("GET", "/api/v1/machines/mch_alpha/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	gw.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/machines/mch_alpha/metrics returned %d: %s", w.Code, w.Body.String())
	}

	// 4. GET /api/v1/machines/enroll
	req = httptest.NewRequest("GET", "/api/v1/machines/enroll?server_url=http://pikpik.example.com", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	gw.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/machines/enroll returned %d: %s", w.Code, w.Body.String())
	}
	var enrollResp api.Response[api.EnrollMachineResponse]
	if err := json.Unmarshal(w.Body.Bytes(), &enrollResp); err != nil {
		t.Fatalf("unmarshal enroll error: %v", err)
	}
	if enrollResp.Data.ServerURL != "http://pikpik.example.com" {
		t.Fatalf("unexpected enroll server_url: %s", enrollResp.Data.ServerURL)
	}

	// 5. POST /api/v1/machines/{id}/join-swarm
	joinBody, _ := json.Marshal(api.JoinSwarmRequest{Role: "worker"})
	req = httptest.NewRequest("POST", "/api/v1/machines/mch_alpha/join-swarm", bytes.NewReader(joinBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	gw.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/machines/mch_alpha/join-swarm returned %d: %s", w.Code, w.Body.String())
	}
	var joinResp api.Response[api.SwarmNode]
	if err := json.Unmarshal(w.Body.Bytes(), &joinResp); err != nil {
		t.Fatalf("unmarshal join error: %v", err)
	}
	if joinResp.Data.ID != "mch_alpha" || joinResp.Data.Role != "worker" {
		t.Fatalf("unexpected join response: %+v", joinResp.Data)
	}

	// 6. DELETE /api/v1/machines/{id}
	req = httptest.NewRequest("DELETE", "/api/v1/machines/mch_alpha", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	gw.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /api/v1/machines/mch_alpha returned %d: %s", w.Code, w.Body.String())
	}

	// Verify not found
	req = httptest.NewRequest("GET", "/api/v1/machines/mch_alpha", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	gw.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", w.Code)
	}
}
