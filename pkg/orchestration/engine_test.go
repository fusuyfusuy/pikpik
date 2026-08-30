package orchestration_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
)

// TestDetectRuntimeMode verifies mode resolution across standalone and swarm environments.
func TestDetectRuntimeMode(t *testing.T) {
	ctx := context.Background()

	t.Run("standalone mode", func(t *testing.T) {
		mock := &MockDockerClient{
			InfoFunc: func(ctx context.Context) (system.Info, error) {
				return system.Info{
					Swarm: swarm.Info{
						LocalNodeState:   swarm.LocalNodeStateInactive,
						ControlAvailable: false,
					},
				}, nil
			},
		}

		mode, err := orchestration.DetectRuntimeMode(ctx, mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mode != orchestration.ModeStandalone {
			t.Errorf("expected ModeStandalone, got %s", mode)
		}
	})

	t.Run("swarm leader mode", func(t *testing.T) {
		mock := &MockDockerClient{
			InfoFunc: func(ctx context.Context) (system.Info, error) {
				return system.Info{
					Swarm: swarm.Info{
						LocalNodeState:   swarm.LocalNodeStateActive,
						ControlAvailable: true,
					},
				}, nil
			},
		}

		mode, err := orchestration.DetectRuntimeMode(ctx, mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mode != orchestration.ModeSwarmLeader {
			t.Errorf("expected ModeSwarmLeader, got %s", mode)
		}
	})

	t.Run("swarm worker mode", func(t *testing.T) {
		mock := &MockDockerClient{
			InfoFunc: func(ctx context.Context) (system.Info, error) {
				return system.Info{
					Swarm: swarm.Info{
						LocalNodeState:   swarm.LocalNodeStateActive,
						ControlAvailable: false,
					},
				}, nil
			},
		}

		mode, err := orchestration.DetectRuntimeMode(ctx, mock)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mode != orchestration.ModeSwarmWorker {
			t.Errorf("expected ModeSwarmWorker, got %s", mode)
		}
	})

	t.Run("disconnected socket error", func(t *testing.T) {
		mock := &MockDockerClient{
			InfoFunc: func(ctx context.Context) (system.Info, error) {
				return system.Info{}, errors.New("daemon unreachable")
			},
		}

		mode, err := orchestration.DetectRuntimeMode(ctx, mock)
		if err == nil {
			t.Fatalf("expected error for unreachable daemon")
		}
		if mode != orchestration.ModeDisconnected {
			t.Errorf("expected ModeDisconnected, got %s", mode)
		}
	})
}

// TestOrchestratorGateway verifies the unified EngineClient interface and sub-managers.
func TestOrchestratorGateway(t *testing.T) {
	mock := &MockDockerClient{}
	ctx := context.Background()

	orch, err := orchestration.NewOrchestrator(ctx, mock)
	if err != nil {
		t.Fatalf("failed to create orchestrator: %v", err)
	}

	if orch.Mode() != orchestration.ModeStandalone {
		t.Errorf("expected standalone mode, got %s", orch.Mode())
	}

	if err := orch.Ping(ctx); err != nil {
		t.Errorf("expected successful ping, got %v", err)
	}

	if orch.Containers() == nil {
		t.Errorf("expected non-nil Containers manager")
	}
	if orch.Swarm() == nil {
		t.Errorf("expected non-nil Swarm manager")
	}
	if orch.Stacks() == nil {
		t.Errorf("expected non-nil Stacks manager")
	}
	if orch.Logs() == nil {
		t.Errorf("expected non-nil Logs streamer")
	}

	if err := orch.Close(); err != nil {
		t.Errorf("expected clean close, got %v", err)
	}
}

// TestLogStreamerOperations verifies log streaming and demuxing over the log streamer interface.
func TestLogStreamerOperations(t *testing.T) {
	mock := &MockDockerClient{}
	logStreamer := orchestration.NewDockerLogStreamer(mock)
	ctx := context.Background()

	// 1. Stream container logs
	reader, err := logStreamer.StreamContainerLogs(ctx, "c-1", orchestration.LogOptions{})
	if err != nil {
		t.Fatalf("failed to stream container logs: %v", err)
	}
	_ = reader.Close()

	// 2. Stream service logs
	sReader, err := logStreamer.StreamServiceLogs(ctx, "svc-1", orchestration.LogOptions{})
	if err != nil {
		t.Fatalf("failed to stream service logs: %v", err)
	}
	_ = sReader.Close()

	// 3. Demux stream
	var stdoutPayload = []byte("App Log Output\n")
	var raw bytes.Buffer
	hdr := make([]byte, 8)
	hdr[0] = 1
	binary.BigEndian.PutUint32(hdr[4:8], uint32(len(stdoutPayload)))
	raw.Write(hdr)
	raw.Write(stdoutPayload)

	mock.ContainerLogsFunc = func(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error) {
		return io.NopCloser(&raw), nil
	}

	var outBuf, errBuf bytes.Buffer
	err = logStreamer.StreamDemux(ctx, "c-1", orchestration.LogOptions{}, &outBuf, &errBuf)
	if err != nil {
		t.Fatalf("stream demux failed: %v", err)
	}
	if outBuf.String() != "App Log Output\n" {
		t.Errorf("expected 'App Log Output\\n', got '%s'", outBuf.String())
	}
}

// TestStackManagerDeployment verifies Compose v2 multi-container stack deployment and teardown.
func TestStackManagerDeployment(t *testing.T) {
	mock := &MockDockerClient{}
	containers := orchestration.NewDockerContainerManager(mock)
	stacks := orchestration.NewDockerStackManager(mock, containers)
	ctx := context.Background()

	stackSpec := orchestration.ComposeStackSpec{
		Name:      "test-stack",
		ProjectID: "proj-123",
		Networks:  []string{"frontend_net"},
		Volumes:   []string{"db_data"},
		Services: map[string]orchestration.ComposeServiceDef{
			"db": {
				Name:  "db",
				Image: "postgres:16",
			},
			"web": {
				Name:      "web",
				Image:     "nginx:alpine",
				DependsOn: []string{"db"},
			},
		},
	}

	// 1. Deploy Stack
	res, err := stacks.DeployStack(ctx, stackSpec)
	if err != nil {
		t.Fatalf("failed to deploy stack: %v", err)
	}

	if len(res.ServicesDeployed) != 2 {
		t.Errorf("expected 2 services deployed, got %d", len(res.ServicesDeployed))
	}
	if len(res.CreatedNetworks) != 1 {
		t.Errorf("expected 1 network created, got %d", len(res.CreatedNetworks))
	}
	if len(res.CreatedVolumes) != 1 {
		t.Errorf("expected 1 volume created, got %d", len(res.CreatedVolumes))
	}

	// 2. Inspect Stack
	mock.ContainerListFunc = func(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
		return []types.Container{
			{
				ID:      "c-1",
				Names:   []string{"/test-stack_db"},
				State:   "running",
				Created: time.Now().Unix(),
				Labels: map[string]string{
					"pikpik.stack_name":   "test-stack",
					"pikpik.service_name": "db",
					"pikpik.project_id":   "proj-123",
				},
			},
			{
				ID:      "c-2",
				Names:   []string{"/test-stack_web"},
				State:   "running",
				Created: time.Now().Unix(),
				Labels: map[string]string{
					"pikpik.stack_name":   "test-stack",
					"pikpik.service_name": "web",
					"pikpik.project_id":   "proj-123",
				},
			},
		}, nil
	}

	status, err := stacks.InspectStack(ctx, "test-stack")
	if err != nil {
		t.Fatalf("failed to inspect stack: %v", err)
	}
	if status.Name != "test-stack" || status.State != "running" {
		t.Errorf("unexpected stack status: %+v", status)
	}
	if len(status.Containers) != 2 {
		t.Errorf("expected 2 containers in stack status, got %d", len(status.Containers))
	}

	// 3. List Stacks
	summaries, err := stacks.ListStacks(ctx)
	if err != nil {
		t.Fatalf("failed to list stacks: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Name != "test-stack" {
		t.Errorf("unexpected stack summaries: %+v", summaries)
	}
	if summaries[0].ContainersCount != 2 || summaries[0].ServicesCount != 2 {
		t.Errorf("unexpected counts: %+v", summaries[0])
	}

	// 4. Remove Stack
	if err := stacks.RemoveStack(ctx, "test-stack"); err != nil {
		t.Fatalf("failed to remove stack: %v", err)
	}
}
