package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/fusuycorp/pikpik/pkg/agent"
	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/backup"
	"github.com/fusuycorp/pikpik/pkg/config"
	"github.com/fusuycorp/pikpik/pkg/crypto"
	"github.com/fusuycorp/pikpik/pkg/deploy"
	"github.com/fusuycorp/pikpik/pkg/ingress"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
	"github.com/fusuycorp/pikpik/pkg/registry"
	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
)

// UserCredentials stores test credential details.
type UserCredentials struct {
	ID       string
	Email    string
	Password string
	Role     string
	Token    string
	OrgID    string
}

// TestHarness is the unified, fully-wired test environment for pikpik control plane.
type TestHarness struct {
	T              *testing.T
	Ctx            context.Context
	Cancel         context.CancelFunc
	TempDir        string
	DBPath         string
	Store          store.Store
	Vault          crypto.Vault
	ConfigMgr      config.ConfigManager
	AuthSvc        auth.AuthService
	DockerEngine   *MockDockerEngine
	Orchestrator   *orchestration.EngineClient
	CaddyServer    *MockCaddyServer
	IngressMgr     *ingress.DefaultIngressManager
	S3Client       *MockS3Client
	ExecRunner     *MockExecRunner
	BackupEng      backup.BackupEngine
	CronScheduler  *backup.CronScheduler
	RegistryMgr    registry.RegistryManager
	WSHub          *api.WebSocketHub
	SSEBroadcaster *api.SSEBroadcaster
	AgentServer    telemetry.AgentServer
	Controller     api.Controller
	APIGateway     *api.APIGateway
	HTTPServer     *httptest.Server

	// Seeded Users
	AdminUser   UserCredentials
	OwnerUser   UserCredentials
	DevUser     UserCredentials
	ViewerUser  UserCredentials
	EnrollToken string
}

// NewTestHarness sets up a complete in-memory isolated pikpik control plane.
func NewTestHarness(t *testing.T) *TestHarness {
	ctx, cancel := context.WithCancel(context.Background())

	tempDir, err := os.MkdirTemp("", "pikpik-e2e-*")
	if err != nil {
		t.Fatalf("failed to create temp test directory: %v", err)
	}

	dbPath := filepath.Join(tempDir, "state.db")
	st, err := store.Open(dbPath)
	if err != nil {
		cancel()
		_ = os.RemoveAll(tempDir)
		t.Fatalf("failed to open sqlite store: %v", err)
	}

	// Master Key & AES Vault
	masterSecret := "pikpik-master-e2e-secret-key-32b"
	vault, err := crypto.NewAESVault(masterSecret)
	if err != nil {
		t.Fatalf("failed to init vault: %v", err)
	}
	configMgr := config.NewConfigManager(st, vault)

	// Auth Service & Argon2 Hasher
	hasher := crypto.DefaultArgon2Hasher()
	authSvc := auth.NewAuthService(st, hasher)

	// In-Memory Docker Engine & Orchestrator
	dockerEngine := NewMockDockerEngine()
	orch, err := orchestration.NewOrchestrator(ctx, dockerEngine)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	// Mock Caddy Dynamic Ingress Server (<15ms)
	caddyServer := NewMockCaddyServer()
	caddyClient := ingress.NewCaddyClient(caddyServer.URL(), 5*time.Second)
	ingressMgr := ingress.NewIngressManager(caddyClient, nil)

	// Mock S3 Client & Backup Engine
	s3Client := NewMockS3Client()
	execRunner := NewMockExecRunner()
	backupEng := backup.NewBackupEngine(s3Client, execRunner)
	cronScheduler := backup.NewCronScheduler(st, backupEng, s3Client)
	_ = cronScheduler.Start(ctx)

	// Registry Manager
	regMgr := registry.NewRegistryManager(dockerEngine, filepath.Join(tempDir, "htpasswd"), filepath.Join(tempDir, "reg.yml"))

	// WebSockets & SSE Broadcaster
	wsHub := api.NewWebSocketHub()
	go wsHub.Run(ctx)
	sseBroadcaster := api.NewSSEBroadcaster()

	// Agent Server & Telemetry Hub
	telemetryWSHub := telemetry.NewWebSocketHub()
	enrollToken := "e2e_node_enrollment_token_12345"
	agentServer := agent.NewAgentServer(agent.AgentServerOptions{
		EnrollmentToken: enrollToken,
		WebSocketHub:    telemetryWSHub,
		RingBuffers:     make(map[string]telemetry.RingBuffer),
		MachineStore:    st.Machines(),
		OnTelemetry: func(nodeID string, msg *telemetry.StreamMessage) {
			if msg != nil {
				wsHub.Broadcast(api.WSMessage{
					Channel:  msg.Channel,
					TargetID: msg.TargetID,
					Event:    msg.Type,
					Data:     msg.Payload,
					Time:     time.Unix(msg.Timestamp, 0).UTC(),
				})
				sseBroadcaster.Broadcast(msg.Channel, msg.TargetID, msg.Type, msg.Payload)
			}
		},
	})

	// Controller & API Gateway
	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store:          st,
		AuthService:    authSvc,
		Orchestrator:   orch,
		IngressManager: ingressMgr,
		BackupEngine:   backupEng,
		Registry:       regMgr,
		ConfigManager:  configMgr,
		Vault:          vault,
		WSHub:          wsHub,
		SSEBroadcaster: sseBroadcaster,
	})

	rateLimiter := api.NewRateLimiter(5000, time.Minute)
	nudgeHandler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{
		RateLimitPerMin: 500,
		BurstLimit:      100,
	})

	apiGateway := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller:     ctrl,
		Store:          st,
		AuthService:    authSvc,
		WebSocketHub:   wsHub,
		SSEBroadcaster: sseBroadcaster,
		DeployWebhook:  nudgeHandler,
		RateLimiter:    rateLimiter,
		DockerClient:   dockerEngine,
		EnableCors:     true,
	})

	// Root Router
	rootMux := http.NewServeMux()
	if asHandler, ok := agentServer.(http.Handler); ok {
		rootMux.Handle("/agent/connect", asHandler)
		rootMux.Handle("/api/v1/agent/connect", asHandler)
	}
	rootMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","version":"1.0.0"}`))
	})
	rootMux.Handle("/api/", apiGateway)
	rootMux.Handle("/ws", apiGateway)
	rootMux.Handle("/ws/", apiGateway)

	httpServer := httptest.NewServer(rootMux)

	h := &TestHarness{
		T:              t,
		Ctx:            ctx,
		Cancel:         cancel,
		TempDir:        tempDir,
		DBPath:         dbPath,
		Store:          st,
		Vault:          vault,
		ConfigMgr:      configMgr,
		AuthSvc:        authSvc,
		DockerEngine:   dockerEngine,
		Orchestrator:   orch,
		CaddyServer:    caddyServer,
		IngressMgr:     ingressMgr,
		S3Client:       s3Client,
		ExecRunner:     execRunner,
		BackupEng:      backupEng,
		CronScheduler:  cronScheduler,
		RegistryMgr:    regMgr,
		WSHub:          wsHub,
		SSEBroadcaster: sseBroadcaster,
		AgentServer:    agentServer,
		Controller:     ctrl,
		APIGateway:     apiGateway,
		HTTPServer:     httpServer,
		EnrollToken:    enrollToken,
	}

	// Seed Standard Roles and Users
	h.seedUsers()

	return h
}

func (h *TestHarness) seedUsers() {
	// 1. Owner Admin
	ownerUser, err := h.AuthSvc.BootstrapAdmin(h.Ctx, "owner@pikpik.local", "OwnerPass123!")
	if err != nil {
		h.T.Fatalf("failed to seed owner user: %v", err)
	}
	ownerToken, _ := h.AuthSvc.CreateAPIToken(h.Ctx, ownerUser.ID, "owner-token", []string{"*"}, nil)
	var ownerSecret string
	if ownerToken != nil {
		ownerSecret = ownerToken.RawSecret
	}
	h.OwnerUser = UserCredentials{
		ID:       ownerUser.ID,
		Email:    "owner@pikpik.local",
		Password: "OwnerPass123!",
		Role:     api.RoleOwner,
		Token:    ownerSecret,
		OrgID:    "org_default",
	}

	// 2. Admin User
	adminUser := &store.User{
		Email:        "admin@pikpik.local",
		PasswordHash: "$argon2id$v=19$m=65536,t=1,p=4$mock$mockhash",
		Role:         api.RoleAdmin,
	}
	if err := h.Store.Users().Create(h.Ctx, adminUser); err == nil {
		token, _ := h.AuthSvc.CreateAPIToken(h.Ctx, adminUser.ID, "admin-token", []string{"*"}, nil)
		var secret string
		if token != nil {
			secret = token.RawSecret
		}
		h.AdminUser = UserCredentials{
			ID:       adminUser.ID,
			Email:    "admin@pikpik.local",
			Password: "AdminPass123!",
			Role:     api.RoleAdmin,
			Token:    secret,
			OrgID:    "org_default",
		}
	}

	// 3. Developer User
	devUser := &store.User{
		Email:        "dev@pikpik.local",
		PasswordHash: "$argon2id$v=19$m=65536,t=1,p=4$mock$mockhash",
		Role:         api.RoleDeveloper,
	}
	if err := h.Store.Users().Create(h.Ctx, devUser); err == nil {
		token, _ := h.AuthSvc.CreateAPIToken(h.Ctx, devUser.ID, "dev-token", []string{"*"}, nil)
		var secret string
		if token != nil {
			secret = token.RawSecret
		}
		h.DevUser = UserCredentials{
			ID:       devUser.ID,
			Email:    "dev@pikpik.local",
			Password: "DevPass123!",
			Role:     api.RoleDeveloper,
			Token:    secret,
			OrgID:    "org_default",
		}
	}

	// 4. Viewer User
	viewerUser := &store.User{
		Email:        "viewer@pikpik.local",
		PasswordHash: "$argon2id$v=19$m=65536,t=1,p=4$mock$mockhash",
		Role:         api.RoleViewer,
	}
	if err := h.Store.Users().Create(h.Ctx, viewerUser); err == nil {
		token, _ := h.AuthSvc.CreateAPIToken(h.Ctx, viewerUser.ID, "viewer-token", []string{"*"}, nil)
		var secret string
		if token != nil {
			secret = token.RawSecret
		}
		h.ViewerUser = UserCredentials{
			ID:       viewerUser.ID,
			Email:    "viewer@pikpik.local",
			Password: "ViewerPass123!",
			Role:     api.RoleViewer,
			Token:    secret,
			OrgID:    "org_default",
		}
	}

	// Ensure Default Project exists
	_ = h.Store.Projects().Create(h.Ctx, &store.Project{
		ID:          "prj_default",
		OrgID:       "org_default",
		Name:        "Default Project",
		Description: "Primary workspace",
	})
}

// Close gracefully stops all harness subsystems and purges temporary storage.
func (h *TestHarness) Close() {
	h.Cancel()
	h.CronScheduler.Stop()
	h.HTTPServer.Close()
	h.CaddyServer.Close()
	_ = h.Store.Close()
	_ = os.RemoveAll(h.TempDir)
}

// URL returns the base HTTP URL of the test control plane.
func (h *TestHarness) URL() string {
	return h.HTTPServer.URL
}

// ============================================================================
// HTTP Client & Helper Methods
// ============================================================================

type E2EResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	Success    bool
	Error      *api.APIError
}

func (r *E2EResponse) JSON(target any) error {
	return json.Unmarshal(r.Body, target)
}

func (h *TestHarness) Request(method, path string, token string, bodyIn any) (*E2EResponse, error) {
	var bodyReader io.Reader
	if bodyIn != nil {
		data, err := json.Marshal(bodyIn)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := h.URL() + path
	req, err := http.NewRequestWithContext(h.Ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request %s %s failed: %w", method, path, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var apiResp struct {
		Success bool          `json:"success"`
		Error   *api.APIError `json:"error,omitempty"`
	}
	_ = json.Unmarshal(bodyBytes, &apiResp)

	return &E2EResponse{
		StatusCode: resp.StatusCode,
		Header:     resp.Header,
		Body:       bodyBytes,
		Success:    apiResp.Success,
		Error:      apiResp.Error,
	}, nil
}

func (h *TestHarness) Get(path string, token string) (*E2EResponse, error) {
	return h.Request(http.MethodGet, path, token, nil)
}

func (h *TestHarness) Post(path string, token string, body any) (*E2EResponse, error) {
	return h.Request(http.MethodPost, path, token, body)
}

func (h *TestHarness) Put(path string, token string, body any) (*E2EResponse, error) {
	return h.Request(http.MethodPut, path, token, body)
}

func (h *TestHarness) Delete(path string, token string) (*E2EResponse, error) {
	return h.Request(http.MethodDelete, path, token, nil)
}

// DialWebSocket opens an authenticated WebSocket session to the control plane.
func (h *TestHarness) DialWebSocket(path string, token string) (*websocket.Conn, *http.Response, error) {
	u, err := url.Parse(h.URL())
	if err != nil {
		return nil, nil, err
	}
	wsScheme := "ws"
	if u.Scheme == "https" {
		wsScheme = "wss"
	}
	wsURL := fmt.Sprintf("%s://%s%s", wsScheme, u.Host, path)

	header := make(http.Header)
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		Subprotocols:     []string{"pikpik-auth"},
	}
	conn, resp, err := dialer.Dial(wsURL, header)
	return conn, resp, err
}

// GenerateRandomHex produces random hex tokens.
func GenerateRandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
