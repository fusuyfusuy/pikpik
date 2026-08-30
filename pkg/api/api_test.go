package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/gorilla/websocket"
)

// 1. Test Rate Limiting Sliding Window
func TestRateLimiter_SlidingWindow(t *testing.T) {
	rl := api.NewRateLimiter(5, time.Minute) // 5 requests per minute

	key := "192.168.1.100"
	for i := 1; i <= 5; i++ {
		allowed, remaining, _ := rl.Allow(key)
		if !allowed {
			t.Fatalf("request %d should be allowed", i)
		}
		if remaining != 5-i {
			t.Fatalf("expected remaining %d, got %d", 5-i, remaining)
		}
	}

	// 6th Request must be rejected
	allowed, remaining, retryAfter := rl.Allow(key)
	if allowed {
		t.Fatalf("6th request should be rate limited")
	}
	if remaining != 0 || retryAfter <= 0 {
		t.Fatalf("invalid rate limit metadata: remaining=%d, retryAfter=%v", remaining, retryAfter)
	}
}

// 2. Test Error Response Serialization
func TestErrorResponse_Formatting(t *testing.T) {
	errResp := api.ErrorResponse{
		Success: false,
		Error: api.APIError{
			Code:      "RESOURCE_CONFLICT",
			Message:   "Domain already bound",
			RequestID: "req_test_01",
		},
	}

	data, err := json.Marshal(errResp)
	if err != nil {
		t.Fatalf("json marshal failed: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json unmarshal failed: %v", err)
	}

	if parsed["success"] != false {
		t.Errorf("expected success to be false")
	}
	errObj := parsed["error"].(map[string]any)
	if errObj["code"] != "RESOURCE_CONFLICT" {
		t.Errorf("expected code RESOURCE_CONFLICT, got %v", errObj["code"])
	}
}

// 3. Test WebSocket Multiplexer Channel Routing
func TestWebSocketHub_Multiplexing(t *testing.T) {
	hub := api.NewWebSocketHub()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go hub.Run(ctx)

	gw := api.NewAPIGateway(api.NewDefaultController(api.ControllerDependencies{}), hub, nil)
	server := httptest.NewServer(gw)
	defer server.Close()

	// Connect Client
	wsURL := "ws" + server.URL[4:] + "/ws"
	dialer := websocket.Dialer{
		Subprotocols: []string{"pikpik-auth.testtoken123"},
	}
	ws, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("websocket dial failed: %v", err)
	}
	defer ws.Close()

	// Send subscription
	sub := api.ClientAction{
		Action:   "subscribe",
		Channel:  "logs",
		TargetID: "app_100",
	}
	subBytes, _ := json.Marshal(sub)
	_ = ws.WriteMessage(websocket.TextMessage, subBytes)

	time.Sleep(50 * time.Millisecond)

	// Broadcast matching message
	hub.Broadcast(api.WSMessage{
		Channel:  "logs",
		TargetID: "app_100",
		Event:    "log_line",
		Data:     "Server running",
		Time:     time.Now().UTC(),
	})

	_ = ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("expected message, got error: %v", err)
	}

	var received api.WSMessage
	if err := json.Unmarshal(msg, &received); err != nil {
		t.Fatalf("failed to parse message: %v", err)
	}
	if received.TargetID != "app_100" || received.Data != "Server running" {
		t.Fatalf("unexpected message payload: %v", received)
	}
}

// 4. Test REST Endpoints with Auth
func TestAPIGateway_Routes(t *testing.T) {
	ctrl := api.NewDefaultController(api.ControllerDependencies{})
	gw := api.NewAPIGateway(ctrl, nil, nil)
	server := httptest.NewServer(gw)
	defer server.Close()

	token := "pik_live_testadminkey1234567890"

	authedRequest := func(method, path string, body []byte) *http.Response {
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatalf("failed to build request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		return resp
	}

	// 1. Auth Login
	loginBody, _ := json.Marshal(api.LoginRequest{
		Email:    "admin@pikpik.local",
		Password: "password123",
	})
	loginResp, err := http.Post(server.URL+"/api/v1/auth/login", "application/json", bytes.NewReader(loginBody))
	if err != nil || loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %v, status %d", err, loginResp.StatusCode)
	}

	// 2. Auth Me
	meResp := authedRequest("GET", "/api/v1/auth/me", nil)
	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("auth/me failed: status %d", meResp.StatusCode)
	}

	// 3. Create App
	createAppReq := api.CreateAppRequest{
		Name:     "web-api",
		Image:    "nginx:alpine",
		Replicas: 2,
		Domains:  []string{"api.example.com"},
	}
	body, _ := json.Marshal(createAppReq)
	resp := authedRequest("POST", "/api/v1/apps", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", resp.StatusCode)
	}
	var createdApp api.Response[api.App]
	_ = json.NewDecoder(resp.Body).Decode(&createdApp)
	appID := createdApp.Data.ID
	if appID == "" || createdApp.Data.Name != "web-api" {
		t.Fatalf("invalid created app data: %+v", createdApp)
	}

	// 4. List Apps
	listResp := authedRequest("GET", "/api/v1/apps", nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", listResp.StatusCode)
	}
	var listResult api.Response[[]api.App]
	_ = json.NewDecoder(listResp.Body).Decode(&listResult)
	if len(listResult.Data) != 1 {
		t.Fatalf("expected 1 app, got %d", len(listResult.Data))
	}

	// 5. Deploy App
	deployResp := authedRequest("POST", "/api/v1/apps/"+appID+"/deploy", []byte(`{}`))
	if deployResp.StatusCode != http.StatusOK {
		t.Fatalf("deploy app failed: status %d", deployResp.StatusCode)
	}

	// 6. App Environment
	setEnvResp := authedRequest("PUT", "/api/v1/apps/"+appID+"/env", []byte(`{"NODE_ENV":"production"}`))
	if setEnvResp.StatusCode != http.StatusOK {
		t.Fatalf("set env failed: status %d", setEnvResp.StatusCode)
	}
	getEnvResp := authedRequest("GET", "/api/v1/apps/"+appID+"/env", nil)
	if getEnvResp.StatusCode != http.StatusOK {
		t.Fatalf("get env failed: status %d", getEnvResp.StatusCode)
	}

	// 7. List Nodes
	nodesResp := authedRequest("GET", "/api/v1/nodes", nil)
	if nodesResp.StatusCode != http.StatusOK {
		t.Fatalf("list nodes failed: status %d", nodesResp.StatusCode)
	}

	// 8. Create Database
	createDBReq := api.CreateDatabaseRequest{
		Name:   "main-db",
		Engine: "postgres",
	}
	dbBody, _ := json.Marshal(createDBReq)
	dbResp := authedRequest("POST", "/api/v1/databases", dbBody)
	if dbResp.StatusCode != http.StatusCreated {
		t.Fatalf("create database failed: status %d", dbResp.StatusCode)
	}

	// 9. Bind Domain
	bindReq := api.BindDomainRequest{
		AppID:   appID,
		Domain:  "test.example.com",
		AutoTLS: true,
	}
	bindBody, _ := json.Marshal(bindReq)
	bindResp := authedRequest("POST", "/api/v1/ingress/domains", bindBody)
	if bindResp.StatusCode != http.StatusCreated {
		t.Fatalf("bind domain failed: status %d", bindResp.StatusCode)
	}

	// 10. System Info
	sysResp := authedRequest("GET", "/api/v1/system/info", nil)
	if sysResp.StatusCode != http.StatusOK {
		t.Fatalf("get system info failed: status %d", sysResp.StatusCode)
	}

	// 11. System Prune
	pruneResp := authedRequest("POST", "/api/v1/system/prune", []byte(`{"all":true}`))
	if pruneResp.StatusCode != http.StatusOK {
		t.Fatalf("prune failed: status %d", pruneResp.StatusCode)
	}

	// 12. Delete App
	delResp := authedRequest("DELETE", "/api/v1/apps/"+appID, nil)
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("delete app failed: status %d", delResp.StatusCode)
	}
}
