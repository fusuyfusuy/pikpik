package orchestration_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/fusuycorp/pikpik/pkg/ingress"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// MockTrafficSplitter tracks Ingress traffic shifting invocations for Blue-Green tests.
type MockTrafficSplitter struct {
	mu           sync.RWMutex
	splits       map[string]ingress.TrafficSplitConfig
	splitCalls   []ingress.TrafficSplitConfig
	canaryWeight int
}

func NewMockTrafficSplitter() *MockTrafficSplitter {
	return &MockTrafficSplitter{
		splits: make(map[string]ingress.TrafficSplitConfig),
	}
}

func (m *MockTrafficSplitter) SetTrafficSplit(ctx context.Context, domain string, cfg ingress.TrafficSplitConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.splits[domain] = cfg
	m.splitCalls = append(m.splitCalls, cfg)
	m.canaryWeight = cfg.CanaryPercent
	return nil
}

func (m *MockTrafficSplitter) SetCanaryWeight(ctx context.Context, domain string, canaryPercent int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg, ok := m.splits[domain]
	if !ok {
		return ingress.ErrRouteNotFound
	}
	cfg.CanaryPercent = canaryPercent
	m.splits[domain] = cfg
	m.canaryWeight = canaryPercent
	return nil
}

func (m *MockTrafficSplitter) GetTrafficSplit(ctx context.Context, domain string) (*ingress.TrafficSplitConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg, ok := m.splits[domain]
	if !ok {
		return nil, ingress.ErrRouteNotFound
	}
	return &cfg, nil
}

// TestBlueGreenDeployer_Success verifies a complete end-to-end Blue-Green switchover.
func TestBlueGreenDeployer_Success(t *testing.T) {
	var stoppedContainers []string
	var removedContainers []string
	var mu sync.Mutex

	mockCli := &MockDockerClient{
		ContainerListFunc: func(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
			return []types.Container{
				{
					ID:      "blue-container-100",
					Names:   []string{"/my-app_blue"},
					State:   "running",
					Created: time.Now().Add(-1 * time.Hour).Unix(),
					Labels: map[string]string{
						"pikpik.managed": "true",
						"pikpik.name":    "my-app",
						"pikpik.app_id":  "app_123",
					},
				},
			}, nil
		},
		ContainerCreateFunc: func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "green-container-200"}, nil
		},
		ContainerStartFunc: func(ctx context.Context, containerID string, options container.StartOptions) error {
			return nil
		},
		ContainerInspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					ID:   containerID,
					Name: "/" + containerID,
					State: &types.ContainerState{
						Status:  "running",
						Running: true,
					},
				},
				NetworkSettings: &types.NetworkSettings{
					DefaultNetworkSettings: types.DefaultNetworkSettings{
						IPAddress: "172.20.0.8",
					},
				},
			}, nil
		},
		ContainerStopFunc: func(ctx context.Context, containerID string, options container.StopOptions) error {
			mu.Lock()
			stoppedContainers = append(stoppedContainers, containerID)
			mu.Unlock()
			return nil
		},
		ContainerRemoveFunc: func(ctx context.Context, containerID string, options container.RemoveOptions) error {
			mu.Lock()
			removedContainers = append(removedContainers, containerID)
			mu.Unlock()
			return nil
		},
	}

	containerMgr := orchestration.NewDockerContainerManager(mockCli)
	mockIngress := NewMockTrafficSplitter()

	// Prober function returning 200 OK
	probeCount := int32(0)
	deployer := orchestration.NewBlueGreenDeployer(
		containerMgr,
		mockIngress,
		orchestration.WithProbeFunc(func(ctx context.Context, probeURL string) (bool, error) {
			atomic.AddInt32(&probeCount, 1)
			return true, nil
		}),
	)

	cfg := orchestration.BlueGreenConfig{
		AppID:           "app_123",
		Name:            "my-app",
		Domain:          "my-app.example.com",
		Image:           "my-app:v2.0.0",
		ContainerPort:   3000,
		HealthCheckPath: "/healthz",
		ProbeInterval:   10 * time.Millisecond,
		ProbeTimeout:    1 * time.Second,
		DrainPeriod:     10 * time.Millisecond,
	}

	ctx := context.Background()
	result, err := deployer.Deploy(ctx, cfg)
	if err != nil {
		t.Fatalf("expected successful deployment, got error: %v", err)
	}

	if result.Status != "success" {
		t.Errorf("expected status 'success', got %s", result.Status)
	}
	if result.BlueContainerID != "blue-container-100" {
		t.Errorf("expected blue container blue-container-100, got %s", result.BlueContainerID)
	}
	if result.GreenContainerID != "green-container-200" {
		t.Errorf("expected green container green-container-200, got %s", result.GreenContainerID)
	}

	// Verify Ingress traffic was shifted 100% to Green
	mockIngress.mu.RLock()
	split := mockIngress.splits["my-app.example.com"]
	mockIngress.mu.RUnlock()

	if split.StableUpstream != "172.20.0.8:3000" || split.CanaryPercent != 0 {
		t.Errorf("expected ingress stable upstream 172.20.0.8:3000 at 0%% canary, got %+v", split)
	}

	// Verify Blue container was stopped and removed
	mu.Lock()
	defer mu.Unlock()
	if len(stoppedContainers) != 1 || stoppedContainers[0] != "blue-container-100" {
		t.Errorf("expected blue container to be stopped, got %+v", stoppedContainers)
	}
	if len(removedContainers) != 1 || removedContainers[0] != "blue-container-100" {
		t.Errorf("expected blue container to be removed, got %+v", removedContainers)
	}
}

// TestBlueGreenDeployer_HealthCheckFailure verifies Blue is untouched and Green is terminated on probe failure.
func TestBlueGreenDeployer_HealthCheckFailure(t *testing.T) {
	var stoppedContainers []string
	var removedContainers []string
	var mu sync.Mutex

	mockCli := &MockDockerClient{
		ContainerListFunc: func(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
			return []types.Container{
				{
					ID:      "blue-container-active",
					Names:   []string{"/prod-app_blue"},
					State:   "running",
					Created: time.Now().Add(-2 * time.Hour).Unix(),
					Labels: map[string]string{
						"pikpik.name":   "prod-app",
						"pikpik.app_id": "app_999",
					},
				},
			}, nil
		},
		ContainerCreateFunc: func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "green-container-failed"}, nil
		},
		ContainerStartFunc: func(ctx context.Context, containerID string, options container.StartOptions) error {
			return nil
		},
		ContainerInspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					ID:   containerID,
					Name: "/" + containerID,
					State: &types.ContainerState{
						Status:  "running",
						Running: true,
					},
				},
				NetworkSettings: &types.NetworkSettings{
					DefaultNetworkSettings: types.DefaultNetworkSettings{
						IPAddress: "172.20.0.9",
					},
				},
			}, nil
		},
		ContainerStopFunc: func(ctx context.Context, containerID string, options container.StopOptions) error {
			mu.Lock()
			stoppedContainers = append(stoppedContainers, containerID)
			mu.Unlock()
			return nil
		},
		ContainerRemoveFunc: func(ctx context.Context, containerID string, options container.RemoveOptions) error {
			mu.Lock()
			removedContainers = append(removedContainers, containerID)
			mu.Unlock()
			return nil
		},
	}

	containerMgr := orchestration.NewDockerContainerManager(mockCli)
	mockIngress := NewMockTrafficSplitter()

	// Prober fails (returns false)
	deployer := orchestration.NewBlueGreenDeployer(
		containerMgr,
		mockIngress,
		orchestration.WithProbeFunc(func(ctx context.Context, probeURL string) (bool, error) {
			return false, errors.New("HTTP 500 Internal Server Error")
		}),
	)

	cfg := orchestration.BlueGreenConfig{
		AppID:           "app_999",
		Name:            "prod-app",
		Domain:          "prod.example.com",
		Image:           "prod-app:v2.0-broken",
		ContainerPort:   8080,
		HealthCheckPath: "/healthz",
		ProbeInterval:   10 * time.Millisecond,
		ProbeTimeout:    100 * time.Millisecond,
	}

	ctx := context.Background()
	result, err := deployer.Deploy(ctx, cfg)
	if err == nil {
		t.Fatalf("expected health check error, got success: %+v", result)
	}

	if !errors.Is(err, orchestration.ErrGreenHealthCheckFailed) {
		t.Errorf("expected ErrGreenHealthCheckFailed, got %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Blue must NOT be stopped or removed!
	for _, c := range stoppedContainers {
		if c == "blue-container-active" {
			t.Errorf("invariant violated: active blue container was stopped upon green failure!")
		}
	}
	for _, c := range removedContainers {
		if c == "blue-container-active" {
			t.Errorf("invariant violated: active blue container was removed upon green failure!")
		}
	}

	// Failed Green MUST be removed
	greenRemoved := false
	for _, c := range removedContainers {
		if c == "green-container-failed" {
			greenRemoved = true
			break
		}
	}
	if !greenRemoved {
		t.Errorf("expected failed green container green-container-failed to be removed")
	}

	// Ingress should NOT have switched to Green
	mockIngress.mu.RLock()
	split, hasSplit := mockIngress.splits["prod.example.com"]
	mockIngress.mu.RUnlock()
	if hasSplit && split.StableUpstream == "172.20.0.9:8080" {
		t.Errorf("ingress was incorrectly updated with failed green upstream")
	}
}

// TestBlueGreenDeployer_CanarySteps verifies staged traffic percentage rollout.
func TestBlueGreenDeployer_CanarySteps(t *testing.T) {
	mockCli := &MockDockerClient{
		ContainerListFunc: func(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
			return []types.Container{
				{
					ID:      "blue-c1",
					Names:   []string{"/canary-app_blue"},
					State:   "running",
					Created: time.Now().Unix(),
					Labels:  map[string]string{"pikpik.name": "canary-app"},
				},
			}, nil
		},
		ContainerCreateFunc: func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "green-c2"}, nil
		},
		ContainerStartFunc: func(ctx context.Context, containerID string, options container.StartOptions) error {
			return nil
		},
		ContainerInspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					ID:    containerID,
					State: &types.ContainerState{Status: "running", Running: true},
				},
				NetworkSettings: &types.NetworkSettings{
					DefaultNetworkSettings: types.DefaultNetworkSettings{IPAddress: "172.20.0.22"},
				},
			}, nil
		},
		ContainerStopFunc: func(ctx context.Context, containerID string, options container.StopOptions) error {
			return nil
		},
		ContainerRemoveFunc: func(ctx context.Context, containerID string, options container.RemoveOptions) error {
			return nil
		},
	}

	containerMgr := orchestration.NewDockerContainerManager(mockCli)
	mockIngress := NewMockTrafficSplitter()

	deployer := orchestration.NewBlueGreenDeployer(
		containerMgr,
		mockIngress,
		orchestration.WithProbeFunc(func(ctx context.Context, probeURL string) (bool, error) {
			return true, nil
		}),
	)

	cfg := orchestration.BlueGreenConfig{
		AppID:           "app_canary",
		Name:            "canary-app",
		Domain:          "canary.example.com",
		Image:           "app:v2",
		ContainerPort:   3000,
		HealthCheckPath: "/healthz",
		CanarySteps:     []int{10, 50},
		StepDelay:       5 * time.Millisecond,
		DrainPeriod:     5 * time.Millisecond,
	}

	ctx := context.Background()
	res, err := deployer.Deploy(ctx, cfg)
	if err != nil {
		t.Fatalf("canary blue-green failed: %v", err)
	}
	if res.Status != "success" {
		t.Errorf("expected success, got %s", res.Status)
	}

	// Verify step history in ingress
	mockIngress.mu.RLock()
	calls := mockIngress.splitCalls
	mockIngress.mu.RUnlock()

	if len(calls) < 3 {
		t.Fatalf("expected at least 3 traffic split steps (10%%, 50%%, 100%%), got %d", len(calls))
	}
	if calls[0].CanaryPercent != 10 {
		t.Errorf("step 1 expected 10%% canary, got %d", calls[0].CanaryPercent)
	}
	if calls[1].CanaryPercent != 50 {
		t.Errorf("step 2 expected 50%% canary, got %d", calls[1].CanaryPercent)
	}
	// Final cutover: 0% canary, stable is green
	if calls[2].CanaryPercent != 0 || calls[2].StableUpstream != "172.20.0.22:3000" {
		t.Errorf("step 3 cutover expected green as stable upstream, got %+v", calls[2])
	}
}

// TestBlueGreenDeployer_FirstDeployment verifies initial deployment with no existing Blue container.
func TestBlueGreenDeployer_FirstDeployment(t *testing.T) {
	mockCli := &MockDockerClient{
		ContainerListFunc: func(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
			return []types.Container{}, nil // No existing container
		},
		ContainerCreateFunc: func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "green-first"}, nil
		},
		ContainerStartFunc: func(ctx context.Context, containerID string, options container.StartOptions) error {
			return nil
		},
		ContainerInspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					ID:    containerID,
					State: &types.ContainerState{Status: "running", Running: true},
				},
				NetworkSettings: &types.NetworkSettings{
					DefaultNetworkSettings: types.DefaultNetworkSettings{IPAddress: "172.20.0.50"},
				},
			}, nil
		},
	}

	containerMgr := orchestration.NewDockerContainerManager(mockCli)
	mockIngress := NewMockTrafficSplitter()

	deployer := orchestration.NewBlueGreenDeployer(
		containerMgr,
		mockIngress,
		orchestration.WithProbeFunc(func(ctx context.Context, probeURL string) (bool, error) {
			return true, nil
		}),
	)

	cfg := orchestration.BlueGreenConfig{
		AppID:         "app_init",
		Domain:        "init.example.com",
		Image:         "init:v1",
		ContainerPort: 80,
	}

	ctx := context.Background()
	res, err := deployer.Deploy(ctx, cfg)
	if err != nil {
		t.Fatalf("first deployment failed: %v", err)
	}
	if res.BlueContainerID != "" {
		t.Errorf("expected empty blue container ID on first deploy, got %s", res.BlueContainerID)
	}
	if res.GreenContainerID != "green-first" {
		t.Errorf("expected green-first, got %s", res.GreenContainerID)
	}
}

// TestBlueGreenDeployer_HTTPHealthProber verifies real HTTP probe execution.
func TestBlueGreenDeployer_HTTPHealthProber(t *testing.T) {
	var probeReceived atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			probeReceived.Store(true)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	// Parse host & port from test server
	var host string
	var port uint32
	fmt.Sscanf(server.URL, "http://%s", &host)
	parts := strings.Split(host, ":")
	if len(parts) == 2 {
		fmt.Sscanf(parts[1], "%d", &port)
	}

	mockCli := &MockDockerClient{
		ContainerListFunc: func(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
			return []types.Container{}, nil
		},
		ContainerCreateFunc: func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
			return container.CreateResponse{ID: "c-http-probe"}, nil
		},
		ContainerStartFunc: func(ctx context.Context, containerID string, options container.StartOptions) error {
			return nil
		},
		ContainerInspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					ID:    containerID,
					State: &types.ContainerState{Status: "running", Running: true},
				},
				NetworkSettings: &types.NetworkSettings{
					DefaultNetworkSettings: types.DefaultNetworkSettings{IPAddress: "127.0.0.1"},
				},
			}, nil
		},
	}

	containerMgr := orchestration.NewDockerContainerManager(mockCli)
	deployer := orchestration.NewBlueGreenDeployer(containerMgr, nil, orchestration.WithHTTPClient(server.Client()))

	cfg := orchestration.BlueGreenConfig{
		AppID:           "app_http",
		Domain:          "http.example.com",
		Image:           "http:v1",
		ContainerPort:   port,
		HealthCheckPath: "/healthz",
		ProbeInterval:   20 * time.Millisecond,
		ProbeTimeout:    1 * time.Second,
	}

	ctx := context.Background()
	res, err := deployer.Deploy(ctx, cfg)
	if err != nil {
		t.Fatalf("HTTP probe deployment failed: %v", err)
	}
	if !probeReceived.Load() {
		t.Errorf("expected HTTP probe to be received by test server")
	}
	if res.Status != "success" {
		t.Errorf("expected success, got %s", res.Status)
	}
}
