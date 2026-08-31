package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
	"github.com/fusuycorp/pikpik/pkg/store"
)

// Mock Swarm Manager for testing DeployApp CreateService fallback
type mockSwarmManagerForDeploy struct {
	orchestration.SwarmManager
	createdSpec  *orchestration.ServiceSpec
	updatedSpec  *orchestration.ServiceSpec
	createCalled bool
	updateCalled bool
	serviceMap   map[string]*orchestration.ServiceStatus
}

func (m *mockSwarmManagerForDeploy) InspectService(ctx context.Context, serviceID string) (*orchestration.ServiceStatus, error) {
	if svc, ok := m.serviceMap[serviceID]; ok {
		return svc, nil
	}
	return nil, fmt.Errorf("service %s not found", serviceID)
}

func (m *mockSwarmManagerForDeploy) CreateService(ctx context.Context, spec orchestration.ServiceSpec) (string, error) {
	m.createCalled = true
	m.createdSpec = &spec
	id := "s_" + spec.Name
	m.serviceMap[id] = &orchestration.ServiceStatus{
		ID:      id,
		Name:    spec.Name,
		Image:   spec.Image,
		Version: 1,
	}
	m.serviceMap[spec.Name] = m.serviceMap[id]
	return id, nil
}

func (m *mockSwarmManagerForDeploy) UpdateService(ctx context.Context, serviceID string, version uint64, spec orchestration.ServiceSpec) error {
	m.updateCalled = true
	m.updatedSpec = &spec
	return nil
}

func (m *mockSwarmManagerForDeploy) RemoveService(ctx context.Context, serviceID string) error {
	delete(m.serviceMap, serviceID)
	return nil
}

type mockOrchForDeploy struct {
	orchestration.Orchestrator
	swarm *mockSwarmManagerForDeploy
}

func (m *mockOrchForDeploy) Mode() orchestration.RuntimeMode {
	return orchestration.ModeSwarmLeader
}

func (m *mockOrchForDeploy) Swarm() orchestration.SwarmManager {
	return m.swarm
}

func (m *mockOrchForDeploy) Containers() orchestration.ContainerManager {
	return nil
}

// 1. P2.1: Test that DeployApp creates service if not in Swarm, updates if already present
func TestDeployApp_CreateServiceFallback(t *testing.T) {
	ctx := context.Background()
	swarmMock := &mockSwarmManagerForDeploy{
		serviceMap: make(map[string]*orchestration.ServiceStatus),
	}
	orchMock := &mockOrchForDeploy{swarm: swarmMock}

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Orchestrator: orchMock,
	})

	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:     "brand-new-service",
		Image:    "nginx:1.25",
		Replicas: 2,
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	// First deploy: service doesn't exist in Swarm -> must call CreateService
	err = ctrl.DeployApp(ctx, app.ID, "nginx:1.26")
	if err != nil {
		t.Fatalf("deploy failed: %v", err)
	}
	if !swarmMock.createCalled {
		t.Fatalf("expected CreateService to be called for new service")
	}
	if swarmMock.createdSpec == nil || swarmMock.createdSpec.Image != "nginx:1.26" {
		t.Errorf("expected spec image nginx:1.26, got %v", swarmMock.createdSpec)
	}

	// Second deploy: service now exists in Swarm -> must call UpdateService
	swarmMock.createCalled = false
	err = ctrl.DeployApp(ctx, app.ID, "nginx:1.27")
	if err != nil {
		t.Fatalf("second deploy failed: %v", err)
	}
	if !swarmMock.updateCalled {
		t.Fatalf("expected UpdateService to be called for existing service")
	}
	if swarmMock.updatedSpec == nil || swarmMock.updatedSpec.Image != "nginx:1.27" {
		t.Errorf("expected spec image nginx:1.27, got %v", swarmMock.updatedSpec)
	}
}

// 2. P2.5: Test Git and build strategy fields persistence on apps
func TestAppGitFields_Persistence(t *testing.T) {
	dbPath := t.TempDir() + "/git_fields_test.db"
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
	})

	created, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:             "git-driven-app",
		Image:            "custom:latest",
		Replicas:         3,
		ContainerPort:    3000,
		GitRepoURL:       "https://github.com/org/repo.git",
		GitBranch:        "main",
		BuildStrategy:    "dockerfile",
		DockerfilePath:   "deploy/Dockerfile",
		PublishDirectory: "dist",
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	if created.GitRepoURL != "https://github.com/org/repo.git" || created.BuildStrategy != "dockerfile" {
		t.Errorf("created app missing git fields: %+v", created)
	}

	// Retrieve by ID
	retrieved, err := ctrl.GetApp(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to get app: %v", err)
	}
	if retrieved.GitRepoURL != "https://github.com/org/repo.git" || retrieved.ContainerPort != 3000 || retrieved.DockerfilePath != "deploy/Dockerfile" {
		t.Errorf("retrieved app git fields mismatch: %+v", retrieved)
	}

	// List apps
	list, err := ctrl.ListApps(ctx)
	if err != nil {
		t.Fatalf("failed to list apps: %v", err)
	}
	if len(list) == 0 || list[0].GitRepoURL != "https://github.com/org/repo.git" {
		t.Errorf("list apps git fields mismatch: %+v", list)
	}

	// Update git fields
	updated, err := ctrl.UpdateApp(ctx, created.ID, &api.UpdateAppRequest{
		GitBranch: "staging",
	})
	if err != nil {
		t.Fatalf("failed to update app: %v", err)
	}
	if updated.GitBranch != "staging" {
		t.Errorf("expected branch 'staging', got '%s'", updated.GitBranch)
	}
}

// 3. P2.11: Test App Deployment and Container Port Preservation
func TestAppDeploy_PortAndImagePreservation(t *testing.T) {
	ctx := context.Background()
	ctrl := api.NewDefaultController(api.ControllerDependencies{})

	app, err := ctrl.CreateApp(ctx, &api.CreateAppRequest{
		Name:          "web-api",
		Image:         "web:v1",
		Replicas:      1,
		ContainerPort: 8080,
		Domains:       []string{"api.example.com"},
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	if app.ContainerPort != 8080 {
		t.Errorf("expected container port 8080, got %d", app.ContainerPort)
	}

	// Deploy new image
	err = ctrl.DeployApp(ctx, app.ID, "web:v2")
	if err != nil {
		t.Fatalf("failed to deploy app: %v", err)
	}

	updated, err := ctrl.GetApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("failed to get app: %v", err)
	}
	if updated.Image != "web:v2" {
		t.Errorf("expected image 'web:v2', got '%s'", updated.Image)
	}
	if updated.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", updated.Status)
	}
	if updated.ContainerPort != 8080 {
		t.Errorf("expected container port 8080 preserved, got %d", updated.ContainerPort)
	}
}

// 4. P2.2: Test Backup Schedule CRUD API routes
func TestBackupScheduleRoutes_CRUD(t *testing.T) {
	dbPath := t.TempDir() + "/backup_sched_test.db"
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	_ = st.Users().Create(ctx, &store.User{
		ID:    "usr_admin",
		Email: "admin@pikpik.local",
		Role:  "owner",
	})
	_ = st.Organizations().Create(ctx, &store.Organization{
		ID:   "org_default",
		Name: "Default Org",
		Slug: "default",
	})
	_ = st.Projects().Create(ctx, &store.Project{
		ID:    "default",
		OrgID: "org_default",
		Name:  "Default Project",
		Slug:  "default",
	})
	_ = st.Stages().Create(ctx, &store.Stage{
		ID:        "production",
		ProjectID: "default",
		Name:      "Production",
		Slug:      "production",
	})
	_ = st.Services().Create(ctx, &store.Service{
		ID:        "postgres-db-1",
		ProjectID: "default",
		StageID:   "production",
		Name:      "postgres-db-1",
		Slug:      "postgres-db-1",
		Type:      "database",
		Image:     "postgres:16",
		Status:    "running",
	})

	token := "pik_live_test_schedule_token_12345"
	_ = st.APITokens().Create(ctx, &store.APIToken{
		ID:        "tok_test_admin",
		UserID:    "usr_admin",
		Name:      "admin-token",
		Prefix:    "pik_live_test",
		TokenHash: "f6c46a6e5b4104273df5ec305c7423e800c149d5a995e86ea116b4129b0aa8f4", // Hash of token
		Scopes:    []string{"admin", "read:apps", "write:apps", "backups"},
	})

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
	})

	gw := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller: ctrl,
		Store:      st,
	})

	server := httptest.NewServer(gw)
	defer server.Close()

	// 1. Create Schedule
	createReq := api.CreateBackupScheduleRequest{
		ServiceID:       "postgres-db-1",
		CronExpr:        "0 */6 * * *",
		Engine:          "postgres",
		DatabaseName:    "prod_db",
		Username:        "postgres_user",
		S3Bucket:        "my-backup-bucket",
		RetentionDaily:  14,
		RetentionWeekly: 4,
	}
	body, _ := json.Marshal(createReq)
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/backups/schedules", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/v1/backups/schedules failed: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 201 Created, got %d: %s", resp.StatusCode, string(b))
	}

	var createdRes api.Response[store.BackupSchedule]
	_ = json.NewDecoder(resp.Body).Decode(&createdRes)
	schID := createdRes.Data.ID
	if schID == "" || createdRes.Data.ServiceID != "postgres-db-1" {
		t.Fatalf("unexpected created schedule: %+v", createdRes.Data)
	}

	// 2. List Schedules
	listReq, _ := http.NewRequest("GET", server.URL+"/api/v1/backups/schedules?service_id=postgres-db-1", nil)
	listReq.Header.Set("Authorization", "Bearer "+token)
	listResp, err := http.DefaultClient.Do(listReq)
	if err != nil {
		t.Fatalf("GET /api/v1/backups/schedules failed: %v", err)
	}
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", listResp.StatusCode)
	}
	var listRes api.Response[[]*store.BackupSchedule]
	_ = json.NewDecoder(listResp.Body).Decode(&listRes)
	if len(listRes.Data) != 1 || listRes.Data[0].ID != schID {
		t.Errorf("expected 1 schedule with ID %s, got %d items", schID, len(listRes.Data))
	}

	// 3. Get Schedule by ID
	getReq, _ := http.NewRequest("GET", server.URL+"/api/v1/backups/schedules/"+schID, nil)
	getReq.Header.Set("Authorization", "Bearer "+token)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatalf("GET /api/v1/backups/schedules/%s failed: %v", schID, err)
	}
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", getResp.StatusCode)
	}

	// 4. Update Schedule
	retDays := 30
	updateReq := api.UpdateBackupScheduleRequest{
		RetentionDaily: &retDays,
	}
	updateBody, _ := json.Marshal(updateReq)
	patchReq, _ := http.NewRequest("PATCH", server.URL+"/api/v1/backups/schedules/"+schID, bytes.NewReader(updateBody))
	patchReq.Header.Set("Authorization", "Bearer "+token)
	patchReq.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(patchReq)
	if err != nil {
		t.Fatalf("PATCH failed: %v", err)
	}
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", patchResp.StatusCode)
	}

	// 5. Delete Schedule
	delReq, _ := http.NewRequest("DELETE", server.URL+"/api/v1/backups/schedules/"+schID, nil)
	delReq.Header.Set("Authorization", "Bearer "+token)
	delResp, err := http.DefaultClient.Do(delReq)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	if delResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", delResp.StatusCode)
	}
}

// 5. P3.7: Test CORS Allowed Origins and MaxBytesReader (10MB)
func TestCORS_AllowedOrigins(t *testing.T) {
	gw := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		EnableCors:     true,
		AllowedOrigins: []string{"https://app.pikpik.dev", "https://staging.pikpik.dev"},
	})
	server := httptest.NewServer(gw)
	defer server.Close()

	// A. Allowed Origin
	req, _ := http.NewRequest("OPTIONS", server.URL+"/api/v1/apps", nil)
	req.Header.Set("Origin", "https://app.pikpik.dev")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS request failed: %v", err)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "https://app.pikpik.dev" {
		t.Errorf("expected allow-origin 'https://app.pikpik.dev', got '%s'", resp.Header.Get("Access-Control-Allow-Origin"))
	}
	if resp.Header.Get("Access-Control-Allow-Credentials") != "true" {
		t.Errorf("expected allow-credentials 'true', got '%s'", resp.Header.Get("Access-Control-Allow-Credentials"))
	}

	// B. Disallowed Origin
	req2, _ := http.NewRequest("OPTIONS", server.URL+"/api/v1/apps", nil)
	req2.Header.Set("Origin", "https://malicious-site.com")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("OPTIONS request 2 failed: %v", err)
	}
	if resp2.Header.Get("Access-Control-Allow-Origin") == "https://malicious-site.com" {
		t.Errorf("disallowed origin should not have access-control-allow-origin set")
	}
}

func TestMaxBytesReader_Enforcement(t *testing.T) {
	gw := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{})
	server := httptest.NewServer(gw)
	defer server.Close()

	// 11MB body exceeds 10MB limit
	hugeBody := io.LimitReader(bytes.NewReader(bytes.Repeat([]byte("A"), 11*1024*1024)), 11*1024*1024)
	req, _ := http.NewRequest("POST", server.URL+"/api/v1/auth/login", hugeBody)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// Connection drop or error is valid behavior for MaxBytesReader
		return
	}
	// If response is returned, it should be 400 Bad Request or 413 or 500 error
	if resp.StatusCode == http.StatusOK {
		t.Errorf("expected oversized payload (>10MB) to be rejected, got %d", resp.StatusCode)
	}
}
