package orchestration_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
)

// TestContainerManagerBasicOperations verifies container creation, start, stop, restart, remove, inspect, list.
func TestContainerManagerBasicOperations(t *testing.T) {
	mock := &MockDockerClient{}
	mgr := orchestration.NewDockerContainerManager(mock)
	ctx := context.Background()

	// 1. Create
	spec := orchestration.ContainerSpec{
		Name:      "test-app",
		ProjectID: "proj-1",
		Image:     "nginx:latest",
		Environment: map[string]string{
			"PORT": "8080",
		},
		ExposedPorts: []orchestration.PortMappingSpec{
			{ContainerPort: 8080, HostPort: 8080, Protocol: "tcp"},
		},
		Mounts: []orchestration.VolumeMountSpec{
			{Type: "volume", Source: "data_vol", Target: "/data"},
		},
		Resources: orchestration.ResourceRequirements{
			CPULimit:    1e9,
			MemoryLimit: 512 * 1024 * 1024,
		},
		HealthCheck: &orchestration.HealthCheckConfig{
			Test:     []string{"CMD", "curl", "-f", "http://localhost:8080"},
			Interval: 5 * time.Second,
			Timeout:  2 * time.Second,
		},
		StopTimeout: 15 * time.Second,
		Networks:    []string{"net1", "net2"},
	}

	cid, err := mgr.Create(ctx, spec)
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}
	if !strings.Contains(cid, "test-app") {
		t.Errorf("expected container ID to contain name, got %s", cid)
	}

	// 2. Start
	if err := mgr.Start(ctx, cid); err != nil {
		t.Fatalf("failed to start container: %v", err)
	}

	// 3. Inspect
	status, err := mgr.Inspect(ctx, cid)
	if err != nil {
		t.Fatalf("failed to inspect container: %v", err)
	}
	if status.State != "running" || status.Health != "healthy" {
		t.Errorf("unexpected status: %+v", status)
	}

	// 4. Restart
	if err := mgr.Restart(ctx, cid, 5*time.Second); err != nil {
		t.Fatalf("failed to restart container: %v", err)
	}

	// 5. Stop
	if err := mgr.Stop(ctx, cid, 5*time.Second); err != nil {
		t.Fatalf("failed to stop container: %v", err)
	}

	// 6. List
	list, err := mgr.List(ctx, orchestration.ListOptions{ProjectID: "proj-1", All: true})
	if err != nil {
		t.Fatalf("failed to list containers: %v", err)
	}
	if len(list) == 0 {
		t.Fatalf("expected containers in list")
	}

	// 7. Remove
	if err := mgr.Remove(ctx, cid, true, true); err != nil {
		t.Fatalf("failed to remove container: %v", err)
	}

	// 8. Empty ID edge cases
	if err := mgr.Start(ctx, ""); !errors.Is(err, orchestration.ErrContainerNotFound) {
		t.Errorf("expected ErrContainerNotFound for empty ID start, got %v", err)
	}
	if err := mgr.Stop(ctx, "", 0); !errors.Is(err, orchestration.ErrContainerNotFound) {
		t.Errorf("expected ErrContainerNotFound for empty ID stop, got %v", err)
	}
	if err := mgr.Restart(ctx, "", 0); !errors.Is(err, orchestration.ErrContainerNotFound) {
		t.Errorf("expected ErrContainerNotFound for empty ID restart, got %v", err)
	}
	if err := mgr.Remove(ctx, "", false, false); !errors.Is(err, orchestration.ErrContainerNotFound) {
		t.Errorf("expected ErrContainerNotFound for empty ID remove, got %v", err)
	}
	if _, err := mgr.Inspect(ctx, ""); !errors.Is(err, orchestration.ErrContainerNotFound) {
		t.Errorf("expected ErrContainerNotFound for empty ID inspect, got %v", err)
	}
}

// TestDeployWithRollingUpdate_Success tests zero-downtime rolling update flow.
func TestDeployWithRollingUpdate_Success(t *testing.T) {
	mock := &MockDockerClient{}
	mgr := orchestration.NewDockerContainerManager(mock)
	ctx := context.Background()

	spec := orchestration.ContainerSpec{
		Name:      "app_prod",
		ProjectID: "proj-123",
		Image:     "app:v2",
		HealthCheck: &orchestration.HealthCheckConfig{
			Test: []string{"CMD", "curl", "http://localhost:3000"},
		},
		StopTimeout: 10 * time.Second,
	}

	cfg := orchestration.RollingUpdateConfig{
		Monitor: 500 * time.Millisecond,
	}

	result, err := mgr.DeployWithRollingUpdate(ctx, spec, cfg)
	if err != nil {
		t.Fatalf("rolling update failed: %v", err)
	}

	if !result.Healthy {
		t.Errorf("expected result to be healthy")
	}
	if result.OldContainerID != "mock-c1" {
		t.Errorf("expected old container ID mock-c1, got %s", result.OldContainerID)
	}
	if result.NewContainerID == "" {
		t.Errorf("expected non-empty new container ID")
	}
}

// TestDeployWithRollingUpdate_HealthFailure verifies rollback on health probation failure.
func TestDeployWithRollingUpdate_HealthFailure(t *testing.T) {
	mock := &MockDockerClient{
		ContainerInspectFunc: func(ctx context.Context, containerID string) (types.ContainerJSON, error) {
			return types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					ID: containerID,
					State: &types.ContainerState{
						Status: "running",
						Health: &types.Health{
							Status: "unhealthy",
						},
					},
				},
			}, nil
		},
	}

	mgr := orchestration.NewDockerContainerManager(mock)
	ctx := context.Background()

	spec := orchestration.ContainerSpec{
		Name:      "app_prod",
		ProjectID: "proj-123",
		Image:     "app:v2",
		HealthCheck: &orchestration.HealthCheckConfig{
			Test: []string{"CMD", "curl", "http://localhost:3000"},
		},
	}

	cfg := orchestration.RollingUpdateConfig{
		Monitor: 500 * time.Millisecond,
	}

	_, err := mgr.DeployWithRollingUpdate(ctx, spec, cfg)
	if !errors.Is(err, orchestration.ErrContainerHealthTimeout) {
		t.Fatalf("expected ErrContainerHealthTimeout, got %v", err)
	}
}
