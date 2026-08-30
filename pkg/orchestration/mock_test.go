package orchestration_test

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// MockDockerClient implements client.CommonAPIClient for testing orchestrator lifecycle without a real daemon.
type MockDockerClient struct {
	client.CommonAPIClient

	// Mock function hooks
	InfoFunc                  func(ctx context.Context) (system.Info, error)
	PingFunc                  func(ctx context.Context) (types.Ping, error)
	CloseFunc                 func() error
	ContainerCreateFunc       func(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error)
	ContainerStartFunc        func(ctx context.Context, containerID string, options container.StartOptions) error
	ContainerStopFunc         func(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRestartFunc      func(ctx context.Context, containerID string, options container.StopOptions) error
	ContainerRemoveFunc       func(ctx context.Context, containerID string, options container.RemoveOptions) error
	ContainerInspectFunc      func(ctx context.Context, containerID string) (types.ContainerJSON, error)
	ContainerListFunc         func(ctx context.Context, options container.ListOptions) ([]types.Container, error)
	ContainerLogsFunc         func(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error)
	ServiceCreateFunc         func(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error)
	ServiceUpdateFunc         func(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error)
	ServiceRemoveFunc         func(ctx context.Context, serviceID string) error
	ServiceInspectWithRawFunc func(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error)
	ServiceListFunc           func(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error)
	ServiceLogsFunc           func(ctx context.Context, service string, options container.LogsOptions) (io.ReadCloser, error)
	TaskListFunc              func(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error)
	NodeListFunc              func(ctx context.Context, options types.NodeListOptions) ([]swarm.Node, error)
	NodeInspectWithRawFunc    func(ctx context.Context, nodeID string) (swarm.Node, []byte, error)
	NodeUpdateFunc            func(ctx context.Context, nodeID string, version swarm.Version, node swarm.NodeSpec) error
	SwarmInitFunc             func(ctx context.Context, req swarm.InitRequest) (string, error)
	SwarmJoinFunc             func(ctx context.Context, req swarm.JoinRequest) error
	SwarmLeaveFunc            func(ctx context.Context, force bool) error
	SwarmInspectFunc          func(ctx context.Context) (swarm.Swarm, error)
	NetworkCreateFunc         func(ctx context.Context, name string, options types.NetworkCreate) (types.NetworkCreateResponse, error)
	NetworkListFunc           func(ctx context.Context, options types.NetworkListOptions) ([]types.NetworkResource, error)
	NetworkRemoveFunc         func(ctx context.Context, networkID string) error
	NetworkConnectFunc        func(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error
	VolumeCreateFunc          func(ctx context.Context, options volume.CreateOptions) (volume.Volume, error)
	VolumeRemoveFunc          func(ctx context.Context, volumeID string, force bool) error
}

func (m *MockDockerClient) Info(ctx context.Context) (system.Info, error) {
	if m.InfoFunc != nil {
		return m.InfoFunc(ctx)
	}
	return system.Info{
		Swarm: swarm.Info{
			LocalNodeState:   swarm.LocalNodeStateInactive,
			ControlAvailable: false,
		},
	}, nil
}

func (m *MockDockerClient) Ping(ctx context.Context) (types.Ping, error) {
	if m.PingFunc != nil {
		return m.PingFunc(ctx)
	}
	return types.Ping{APIVersion: "1.44"}, nil
}

func (m *MockDockerClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func (m *MockDockerClient) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	if m.ContainerCreateFunc != nil {
		return m.ContainerCreateFunc(ctx, config, hostConfig, networkingConfig, platform, containerName)
	}
	return container.CreateResponse{ID: "mock-container-id-" + containerName}, nil
}

func (m *MockDockerClient) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	if m.ContainerStartFunc != nil {
		return m.ContainerStartFunc(ctx, containerID, options)
	}
	return nil
}

func (m *MockDockerClient) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	if m.ContainerStopFunc != nil {
		return m.ContainerStopFunc(ctx, containerID, options)
	}
	return nil
}

func (m *MockDockerClient) ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error {
	if m.ContainerRestartFunc != nil {
		return m.ContainerRestartFunc(ctx, containerID, options)
	}
	return nil
}

func (m *MockDockerClient) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	if m.ContainerRemoveFunc != nil {
		return m.ContainerRemoveFunc(ctx, containerID, options)
	}
	return nil
}

func (m *MockDockerClient) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	if m.ContainerInspectFunc != nil {
		return m.ContainerInspectFunc(ctx, containerID)
	}
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			ID:      containerID,
			Name:    "/" + containerID,
			Created: time.Now().Format(time.RFC3339Nano),
			State: &types.ContainerState{
				Status:  "running",
				Running: true,
				Health: &types.Health{
					Status: "healthy",
				},
			},
		},
		Config: &container.Config{
			Image: "alpine:latest",
			Labels: map[string]string{
				"pikpik.managed": "true",
			},
		},
		NetworkSettings: &types.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {
					IPAddress: "172.20.0.5",
				},
			},
		},
	}, nil
}

func (m *MockDockerClient) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	if m.ContainerListFunc != nil {
		return m.ContainerListFunc(ctx, options)
	}
	return []types.Container{
		{
			ID:      "mock-c1",
			Names:   []string{"/app_prod"},
			Image:   "app:v1",
			State:   "running",
			Status:  "Up 2 hours",
			Created: time.Now().Unix(),
			Labels: map[string]string{
				"pikpik.managed":    "true",
				"pikpik.project_id": "proj-123",
				"pikpik.name":       "app_prod",
			},
		},
	}, nil
}

func (m *MockDockerClient) ContainerLogs(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error) {
	if m.ContainerLogsFunc != nil {
		return m.ContainerLogsFunc(ctx, container, options)
	}
	return io.NopCloser(strings.NewReader("mock log line\n")), nil
}

func (m *MockDockerClient) ServiceCreate(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
	if m.ServiceCreateFunc != nil {
		return m.ServiceCreateFunc(ctx, service, options)
	}
	return swarm.ServiceCreateResponse{ID: "mock-service-id"}, nil
}

func (m *MockDockerClient) ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
	if m.ServiceUpdateFunc != nil {
		return m.ServiceUpdateFunc(ctx, serviceID, version, service, options)
	}
	return swarm.ServiceUpdateResponse{}, nil
}

func (m *MockDockerClient) ServiceRemove(ctx context.Context, serviceID string) error {
	if m.ServiceRemoveFunc != nil {
		return m.ServiceRemoveFunc(ctx, serviceID)
	}
	return nil
}

func (m *MockDockerClient) ServiceInspectWithRaw(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error) {
	if m.ServiceInspectWithRawFunc != nil {
		return m.ServiceInspectWithRawFunc(ctx, serviceID, options)
	}
	replicas := uint64(2)
	return swarm.Service{
		ID: serviceID,
		Meta: swarm.Meta{
			Version:   swarm.Version{Index: 1},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		Spec: swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name: "mock-service",
				Labels: map[string]string{
					"pikpik.managed": "true",
				},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image: "nginx:alpine",
				},
				Placement: &swarm.Placement{
					Constraints: []string{"node.role == worker"},
				},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{
					Replicas: &replicas,
				},
			},
		},
	}, nil, nil
}

func (m *MockDockerClient) ServiceList(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error) {
	if m.ServiceListFunc != nil {
		return m.ServiceListFunc(ctx, options)
	}
	replicas := uint64(1)
	return []swarm.Service{
		{
			ID: "svc-1",
			Meta: swarm.Meta{
				Version:   swarm.Version{Index: 1},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			Spec: swarm.ServiceSpec{
				Annotations: swarm.Annotations{
					Name: "web-svc",
				},
				TaskTemplate: swarm.TaskSpec{
					ContainerSpec: &swarm.ContainerSpec{
						Image: "web:latest",
					},
				},
				Mode: swarm.ServiceMode{
					Replicated: &swarm.ReplicatedService{
						Replicas: &replicas,
					},
				},
			},
		},
	}, nil
}

func (m *MockDockerClient) ServiceLogs(ctx context.Context, service string, options container.LogsOptions) (io.ReadCloser, error) {
	if m.ServiceLogsFunc != nil {
		return m.ServiceLogsFunc(ctx, service, options)
	}
	return io.NopCloser(strings.NewReader("mock service log\n")), nil
}

func (m *MockDockerClient) TaskList(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error) {
	if m.TaskListFunc != nil {
		return m.TaskListFunc(ctx, options)
	}
	return []swarm.Task{
		{
			ID:        "task-1",
			ServiceID: "svc-1",
			NodeID:    "node-1",
			Slot:      1,
			Status:    swarm.TaskStatus{State: swarm.TaskStateRunning, Message: "started"},
			Meta: swarm.Meta{
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			DesiredState: swarm.TaskStateRunning,
		},
	}, nil
}

func (m *MockDockerClient) NodeList(ctx context.Context, options types.NodeListOptions) ([]swarm.Node, error) {
	if m.NodeListFunc != nil {
		return m.NodeListFunc(ctx, options)
	}
	return []swarm.Node{
		{
			ID: "node-1",
			Description: swarm.NodeDescription{
				Hostname: "leader-node",
				Engine:   swarm.EngineDescription{EngineVersion: "27.5.1"},
				Resources: swarm.Resources{
					NanoCPUs:    4 * 1e9,
					MemoryBytes: 8 * 1024 * 1024 * 1024,
				},
			},
			Status: swarm.NodeStatus{
				State: swarm.NodeStateReady,
				Addr:  "192.168.1.10",
			},
			Spec: swarm.NodeSpec{
				Role:         swarm.NodeRoleManager,
				Availability: swarm.NodeAvailabilityActive,
				Annotations: swarm.Annotations{
					Labels: map[string]string{"tier": "gateway"},
				},
			},
			ManagerStatus: &swarm.ManagerStatus{
				Leader:       true,
				Reachability: swarm.ReachabilityReachable,
			},
		},
	}, nil
}

func (m *MockDockerClient) NodeInspectWithRaw(ctx context.Context, nodeID string) (swarm.Node, []byte, error) {
	if m.NodeInspectWithRawFunc != nil {
		return m.NodeInspectWithRawFunc(ctx, nodeID)
	}
	return swarm.Node{
		ID: nodeID,
		Meta: swarm.Meta{
			Version: swarm.Version{Index: 1},
		},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityActive,
			Annotations: swarm.Annotations{
				Labels: map[string]string{"zone": "us-east"},
			},
		},
	}, nil, nil
}

func (m *MockDockerClient) NodeUpdate(ctx context.Context, nodeID string, version swarm.Version, node swarm.NodeSpec) error {
	if m.NodeUpdateFunc != nil {
		return m.NodeUpdateFunc(ctx, nodeID, version, node)
	}
	return nil
}

func (m *MockDockerClient) SwarmInit(ctx context.Context, req swarm.InitRequest) (string, error) {
	if m.SwarmInitFunc != nil {
		return m.SwarmInitFunc(ctx, req)
	}
	return "mock-leader-node-id", nil
}

func (m *MockDockerClient) SwarmJoin(ctx context.Context, req swarm.JoinRequest) error {
	if m.SwarmJoinFunc != nil {
		return m.SwarmJoinFunc(ctx, req)
	}
	return nil
}

func (m *MockDockerClient) SwarmLeave(ctx context.Context, force bool) error {
	if m.SwarmLeaveFunc != nil {
		return m.SwarmLeaveFunc(ctx, force)
	}
	return nil
}

func (m *MockDockerClient) SwarmInspect(ctx context.Context) (swarm.Swarm, error) {
	if m.SwarmInspectFunc != nil {
		return m.SwarmInspectFunc(ctx)
	}
	return swarm.Swarm{
		ClusterInfo: swarm.ClusterInfo{
			ID: "mock-cluster-id",
			Meta: swarm.Meta{
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
			Spec: swarm.Spec{
				Annotations: swarm.Annotations{
					Name: "default",
				},
			},
		},
		JoinTokens: swarm.JoinTokens{
			Worker:  "SWMTKN-1-worker",
			Manager: "SWMTKN-1-manager",
		},
	}, nil
}

func (m *MockDockerClient) NetworkCreate(ctx context.Context, name string, options types.NetworkCreate) (types.NetworkCreateResponse, error) {
	if m.NetworkCreateFunc != nil {
		return m.NetworkCreateFunc(ctx, name, options)
	}
	return types.NetworkCreateResponse{ID: "mock-net-" + name}, nil
}

func (m *MockDockerClient) NetworkList(ctx context.Context, options types.NetworkListOptions) ([]types.NetworkResource, error) {
	if m.NetworkListFunc != nil {
		return m.NetworkListFunc(ctx, options)
	}
	return []types.NetworkResource{
		{
			ID:     "net-1",
			Name:   "app_net",
			Driver: "bridge",
			Labels: map[string]string{"pikpik.stack_name": "app"},
		},
	}, nil
}

func (m *MockDockerClient) NetworkRemove(ctx context.Context, networkID string) error {
	if m.NetworkRemoveFunc != nil {
		return m.NetworkRemoveFunc(ctx, networkID)
	}
	return nil
}

func (m *MockDockerClient) NetworkConnect(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error {
	if m.NetworkConnectFunc != nil {
		return m.NetworkConnectFunc(ctx, networkID, containerID, config)
	}
	return nil
}

func (m *MockDockerClient) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	if m.VolumeCreateFunc != nil {
		return m.VolumeCreateFunc(ctx, options)
	}
	return volume.Volume{Name: options.Name}, nil
}

func (m *MockDockerClient) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	if m.VolumeRemoveFunc != nil {
		return m.VolumeRemoveFunc(ctx, volumeID, force)
	}
	return nil
}
