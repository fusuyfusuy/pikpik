package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/api/types/system"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/fusuycorp/pikpik/pkg/backup/s3"
	"github.com/fusuycorp/pikpik/pkg/ingress"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
)

// ============================================================================
// 1. In-Memory Mock Docker Engine
// ============================================================================

type MockContainerState struct {
	ID        string
	Name      string
	Image     string
	Config    *container.Config
	Host      *container.HostConfig
	Net       *network.NetworkingConfig
	State     types.ContainerState
	Networks  map[string]*network.EndpointSettings
	Logs      []string
	Created   time.Time
	ExposedIP string
}

type MockSwarmServiceState struct {
	ID        string
	Spec      swarm.ServiceSpec
	Version   swarm.Version
	CreatedAt time.Time
	UpdatedAt time.Time
}

type MockExecInstance struct {
	ID          string
	ContainerID string
	Cmd         []string
	Env         []string
	ExitCode    int
	Running     bool
	StdoutBuf   *bytes.Buffer
	StderrBuf   *bytes.Buffer
}

// MockDockerEngine is a comprehensive in-memory thread-safe Docker Engine simulator.
type MockDockerEngine struct {
	client.CommonAPIClient

	mu          sync.RWMutex
	containers  map[string]*MockContainerState
	services    map[string]*MockSwarmServiceState
	tasks       map[string]swarm.Task
	nodes       map[string]swarm.Node
	networks    map[string]types.NetworkResource
	volumes     map[string]volume.Volume
	execs       map[string]*MockExecInstance
	swarmActive bool
	swarmInfo   swarm.Swarm
	nextID      uint64

	// Hook overrides
	OnContainerCreate func(name string, cfg *container.Config)
	OnContainerStart  func(id string)
	OnContainerStop   func(id string)
	OnServiceUpdate   func(id string, spec swarm.ServiceSpec)
}

func NewMockDockerEngine() *MockDockerEngine {
	m := &MockDockerEngine{
		containers: make(map[string]*MockContainerState),
		services:   make(map[string]*MockSwarmServiceState),
		tasks:      make(map[string]swarm.Task),
		nodes:      make(map[string]swarm.Node),
		networks:   make(map[string]types.NetworkResource),
		volumes:    make(map[string]volume.Volume),
		execs:      make(map[string]*MockExecInstance),
	}

	// Seed default bridge network
	m.networks["bridge"] = types.NetworkResource{
		ID:     "net-bridge-default",
		Name:   "bridge",
		Driver: "bridge",
		Scope:  "local",
	}

	// Seed default swarm leader node
	m.nodes["node-leader-1"] = swarm.Node{
		ID: "node-leader-1",
		Meta: swarm.Meta{
			Version: swarm.Version{Index: 1},
		},
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleManager,
			Availability: swarm.NodeAvailabilityActive,
			Annotations: swarm.Annotations{
				Labels: map[string]string{"env": "production"},
			},
		},
		Description: swarm.NodeDescription{
			Hostname: "pikpik-leader",
			Engine: swarm.EngineDescription{
				EngineVersion: "27.5.1",
			},
		},
		Status: swarm.NodeStatus{
			State: swarm.NodeStateReady,
			Addr:  "127.0.0.1",
		},
		ManagerStatus: &swarm.ManagerStatus{
			Leader:       true,
			Reachability: swarm.ReachabilityReachable,
			Addr:         "127.0.0.1:2377",
		},
	}

	return m
}

func (m *MockDockerEngine) nextIDStr(prefix string) string {
	id := atomic.AddUint64(&m.nextID, 1)
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), id)
}

func (m *MockDockerEngine) Info(ctx context.Context) (system.Info, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state := swarm.LocalNodeStateInactive
	if m.swarmActive {
		state = swarm.LocalNodeStateActive
	}

	return system.Info{
		ID:                "pikpik-mock-daemon",
		Containers:        len(m.containers),
		ContainersRunning: len(m.containers),
		Images:            10,
		Driver:            "overlay2",
		MemoryLimit:       true,
		SwapLimit:         true,
		CPUCfsPeriod:      true,
		CPUCfsQuota:       true,
		IPv4Forwarding:    true,
		Name:              "pikpik-mock-host",
		ServerVersion:     "27.5.1",
		Swarm: swarm.Info{
			NodeID:           "node-leader-1",
			LocalNodeState:   state,
			ControlAvailable: m.swarmActive,
			Nodes:            len(m.nodes),
			Managers:         1,
		},
	}, nil
}

func (m *MockDockerEngine) Ping(ctx context.Context) (types.Ping, error) {
	return types.Ping{
		APIVersion:     "1.44",
		OSType:         "linux",
		Experimental:   false,
		BuilderVersion: types.BuilderBuildKit,
	}, nil
}

func (m *MockDockerEngine) Close() error {
	return nil
}

// Container Operations

func (m *MockDockerEngine) ContainerCreate(ctx context.Context, config *container.Config, hostConfig *container.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (container.CreateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := strings.TrimPrefix(containerName, "/")
	if name == "" {
		name = m.nextIDStr("c")
	}
	id := m.nextIDStr("cntr")

	nets := make(map[string]*network.EndpointSettings)
	nets["bridge"] = &network.EndpointSettings{
		IPAddress: "172.20.0.10",
		NetworkID: "net-bridge-default",
	}

	c := &MockContainerState{
		ID:        id,
		Name:      name,
		Image:     config.Image,
		Config:    config,
		Host:      hostConfig,
		Net:       networkingConfig,
		Created:   time.Now().UTC(),
		Networks:  nets,
		ExposedIP: "172.20.0.10",
		State: types.ContainerState{
			Status:  "created",
			Running: false,
			Health: &types.Health{
				Status: types.Healthy,
			},
		},
		Logs: []string{fmt.Sprintf("container %s created with image %s", name, config.Image)},
	}

	m.containers[id] = c
	m.containers[name] = c

	if m.OnContainerCreate != nil {
		m.OnContainerCreate(name, config)
	}

	return container.CreateResponse{ID: id}, nil
}

func (m *MockDockerEngine) ContainerStart(ctx context.Context, containerID string, options container.StartOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.containers[containerID]
	if !ok {
		return fmt.Errorf("container not found: %s", containerID)
	}
	c.State.Status = "running"
	c.State.Running = true
	c.State.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if c.State.Health == nil {
		c.State.Health = &types.Health{Status: types.Healthy}
	} else {
		c.State.Health.Status = types.Healthy
	}
	c.Logs = append(c.Logs, "container started successfully")

	if m.OnContainerStart != nil {
		m.OnContainerStart(c.ID)
	}
	return nil
}

func (m *MockDockerEngine) ContainerStop(ctx context.Context, containerID string, options container.StopOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.containers[containerID]
	if !ok {
		return fmt.Errorf("container not found: %s", containerID)
	}
	c.State.Status = "exited"
	c.State.Running = false
	c.State.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	c.Logs = append(c.Logs, "container stopped")

	if m.OnContainerStop != nil {
		m.OnContainerStop(c.ID)
	}
	return nil
}

func (m *MockDockerEngine) ContainerRestart(ctx context.Context, containerID string, options container.StopOptions) error {
	_ = m.ContainerStop(ctx, containerID, options)
	return m.ContainerStart(ctx, containerID, container.StartOptions{})
}

func (m *MockDockerEngine) ContainerRemove(ctx context.Context, containerID string, options container.RemoveOptions) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.containers[containerID]
	if !ok {
		return fmt.Errorf("container not found: %s", containerID)
	}
	delete(m.containers, c.ID)
	delete(m.containers, c.Name)
	return nil
}

func (m *MockDockerEngine) ContainerInspect(ctx context.Context, containerID string) (types.ContainerJSON, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	c, ok := m.containers[containerID]
	if !ok {
		return types.ContainerJSON{}, fmt.Errorf("container not found: %s", containerID)
	}

	cfg := c.Config
	if cfg == nil {
		cfg = &container.Config{Image: c.Image}
	}

	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			ID:         c.ID,
			Name:       "/" + c.Name,
			Created:    c.Created.Format(time.RFC3339Nano),
			State:      &c.State,
			Image:      c.Image,
			HostConfig: c.Host,
		},
		Config:          cfg,
		NetworkSettings: &types.NetworkSettings{Networks: c.Networks},
	}, nil
}

func (m *MockDockerEngine) ContainerList(ctx context.Context, options container.ListOptions) ([]types.Container, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []types.Container
	seen := make(map[string]bool)

	for _, c := range m.containers {
		if seen[c.ID] {
			continue
		}
		seen[c.ID] = true

		if !options.All && !c.State.Running {
			continue
		}

		labels := make(map[string]string)
		if c.Config != nil && c.Config.Labels != nil {
			labels = c.Config.Labels
		}

		list = append(list, types.Container{
			ID:      c.ID,
			Names:   []string{"/" + c.Name},
			Image:   c.Image,
			State:   c.State.Status,
			Status:  "Up 1 hour",
			Created: c.Created.Unix(),
			Labels:  labels,
		})
	}
	return list, nil
}

func (m *MockDockerEngine) ContainerLogs(ctx context.Context, containerID string, options container.LogsOptions) (io.ReadCloser, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	c, ok := m.containers[containerID]
	if !ok {
		return nil, fmt.Errorf("container not found: %s", containerID)
	}

	var buf bytes.Buffer
	for _, line := range c.Logs {
		header := make([]byte, 8)
		header[0] = 1 // stdout
		n := len(line) + 1
		header[4] = byte(n >> 24)
		header[5] = byte(n >> 16)
		header[6] = byte(n >> 8)
		header[7] = byte(n)
		buf.Write(header)
		buf.WriteString(line + "\n")
	}
	return io.NopCloser(&buf), nil
}

// Container Exec Operations

func (m *MockDockerEngine) ContainerExecCreate(ctx context.Context, containerID string, config container.ExecOptions) (types.IDResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.containers[containerID]; !ok {
		return types.IDResponse{}, fmt.Errorf("container not found: %s", containerID)
	}

	execID := m.nextIDStr("exec")
	inst := &MockExecInstance{
		ID:          execID,
		ContainerID: containerID,
		Cmd:         config.Cmd,
		Env:         config.Env,
		ExitCode:    0,
		Running:     false,
		StdoutBuf:   bytes.NewBufferString("mock exec output\n"),
		StderrBuf:   new(bytes.Buffer),
	}
	m.execs[execID] = inst
	return types.IDResponse{ID: execID}, nil
}

func (m *MockDockerEngine) ContainerExecAttach(ctx context.Context, execID string, config container.ExecAttachOptions) (types.HijackedResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	inst, ok := m.execs[execID]
	if !ok {
		return types.HijackedResponse{}, fmt.Errorf("exec not found: %s", execID)
	}

	var buf bytes.Buffer
	stdcopy.NewStdWriter(&buf, stdcopy.Stdout).Write(inst.StdoutBuf.Bytes())

	return types.HijackedResponse{
		Reader: bufio.NewReader(&buf),
	}, nil
}

func (m *MockDockerEngine) ContainerExecInspect(ctx context.Context, execID string) (container.ExecInspect, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	inst, ok := m.execs[execID]
	if !ok {
		return container.ExecInspect{}, fmt.Errorf("exec not found: %s", execID)
	}
	return container.ExecInspect{
		ExecID:   inst.ID,
		Running:  false,
		ExitCode: inst.ExitCode,
	}, nil
}

// Swarm & Service Operations

func (m *MockDockerEngine) SwarmInit(ctx context.Context, req swarm.InitRequest) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.swarmActive = true
	m.swarmInfo = swarm.Swarm{
		ClusterInfo: swarm.ClusterInfo{
			ID: "swarm-cluster-pikpik-01",
			Spec: swarm.Spec{
				Annotations: swarm.Annotations{Name: "pikpik-swarm"},
			},
		},
		JoinTokens: swarm.JoinTokens{
			Worker:  "SWMTKN-1-worker-token-xyz789",
			Manager: "SWMTKN-1-manager-token-abc123",
		},
	}
	return "node-leader-1", nil
}

func (m *MockDockerEngine) SwarmJoin(ctx context.Context, req swarm.JoinRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.swarmActive = true
	newNodeID := m.nextIDStr("node-worker")
	m.nodes[newNodeID] = swarm.Node{
		ID: newNodeID,
		Spec: swarm.NodeSpec{
			Role:         swarm.NodeRoleWorker,
			Availability: swarm.NodeAvailabilityActive,
		},
		Status: swarm.NodeStatus{State: swarm.NodeStateReady},
	}
	return nil
}

func (m *MockDockerEngine) SwarmLeave(ctx context.Context, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.swarmActive = false
	return nil
}

func (m *MockDockerEngine) SwarmInspect(ctx context.Context) (swarm.Swarm, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.swarmActive {
		return swarm.Swarm{}, fmt.Errorf("node is not part of a swarm cluster")
	}
	return m.swarmInfo, nil
}

func (m *MockDockerEngine) ServiceCreate(ctx context.Context, service swarm.ServiceSpec, options types.ServiceCreateOptions) (swarm.ServiceCreateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextIDStr("srv")
	svc := &MockSwarmServiceState{
		ID:        id,
		Spec:      service,
		Version:   swarm.Version{Index: 1},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	m.services[id] = svc
	if service.Name != "" {
		m.services[service.Name] = svc
	}

	replicas := 1
	if service.Mode.Replicated != nil && service.Mode.Replicated.Replicas != nil {
		replicas = int(*service.Mode.Replicated.Replicas)
	}
	for i := 1; i <= replicas; i++ {
		taskID := fmt.Sprintf("%s-task-%d", id, i)
		m.tasks[taskID] = swarm.Task{
			ID:        taskID,
			ServiceID: id,
			NodeID:    "node-leader-1",
			Slot:      i,
			Status: swarm.TaskStatus{
				State: swarm.TaskStateRunning,
			},
			DesiredState: swarm.TaskStateRunning,
		}
	}

	return swarm.ServiceCreateResponse{ID: id}, nil
}

func (m *MockDockerEngine) ServiceUpdate(ctx context.Context, serviceID string, version swarm.Version, service swarm.ServiceSpec, options types.ServiceUpdateOptions) (swarm.ServiceUpdateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	svc, ok := m.services[serviceID]
	if !ok {
		return swarm.ServiceUpdateResponse{}, fmt.Errorf("service not found: %s", serviceID)
	}
	svc.Spec = service
	svc.Version.Index++
	svc.UpdatedAt = time.Now().UTC()

	if m.OnServiceUpdate != nil {
		m.OnServiceUpdate(svc.ID, service)
	}
	return swarm.ServiceUpdateResponse{}, nil
}

func (m *MockDockerEngine) ServiceRemove(ctx context.Context, serviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	svc, ok := m.services[serviceID]
	if !ok {
		return fmt.Errorf("service not found: %s", serviceID)
	}
	delete(m.services, svc.ID)
	delete(m.services, svc.Spec.Name)
	return nil
}

func (m *MockDockerEngine) ServiceInspectWithRaw(ctx context.Context, serviceID string, options types.ServiceInspectOptions) (swarm.Service, []byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	svc, ok := m.services[serviceID]
	if !ok {
		return swarm.Service{}, nil, fmt.Errorf("service not found: %s", serviceID)
	}
	res := swarm.Service{
		ID: svc.ID,
		Meta: swarm.Meta{
			Version:   svc.Version,
			CreatedAt: svc.CreatedAt,
			UpdatedAt: svc.UpdatedAt,
		},
		Spec: svc.Spec,
	}
	raw, _ := json.Marshal(res)
	return res, raw, nil
}

func (m *MockDockerEngine) ServiceList(ctx context.Context, options types.ServiceListOptions) ([]swarm.Service, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []swarm.Service
	seen := make(map[string]bool)
	for _, s := range m.services {
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		list = append(list, swarm.Service{
			ID: s.ID,
			Meta: swarm.Meta{
				Version:   s.Version,
				CreatedAt: s.CreatedAt,
				UpdatedAt: s.UpdatedAt,
			},
			Spec: s.Spec,
		})
	}
	return list, nil
}

func (m *MockDockerEngine) TaskList(ctx context.Context, options types.TaskListOptions) ([]swarm.Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []swarm.Task
	for _, t := range m.tasks {
		list = append(list, t)
	}
	return list, nil
}

func (m *MockDockerEngine) NodeList(ctx context.Context, options types.NodeListOptions) ([]swarm.Node, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []swarm.Node
	for _, n := range m.nodes {
		list = append(list, n)
	}
	return list, nil
}

func (m *MockDockerEngine) NodeInspectWithRaw(ctx context.Context, nodeID string) (swarm.Node, []byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	n, ok := m.nodes[nodeID]
	if !ok {
		return swarm.Node{}, nil, fmt.Errorf("node not found: %s", nodeID)
	}
	raw, _ := json.Marshal(n)
	return n, raw, nil
}

func (m *MockDockerEngine) NodeUpdate(ctx context.Context, nodeID string, version swarm.Version, node swarm.NodeSpec) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	n, ok := m.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	n.Spec = node
	n.Version.Index++
	m.nodes[nodeID] = n
	return nil
}

// Network & Volume Operations

func (m *MockDockerEngine) NetworkCreate(ctx context.Context, name string, options types.NetworkCreate) (types.NetworkCreateResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ipv6 := false
	if options.EnableIPv6 != nil {
		ipv6 = *options.EnableIPv6
	}
	id := m.nextIDStr("net")
	net := types.NetworkResource{
		ID:         id,
		Name:       name,
		Driver:     options.Driver,
		Scope:      options.Scope,
		EnableIPv6: ipv6,
		Internal:   options.Internal,
		Attachable: options.Attachable,
		Labels:     options.Labels,
	}
	m.networks[id] = net
	m.networks[name] = net
	return types.NetworkCreateResponse{ID: id}, nil
}

func (m *MockDockerEngine) NetworkList(ctx context.Context, options types.NetworkListOptions) ([]types.NetworkResource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []types.NetworkResource
	seen := make(map[string]bool)
	for _, n := range m.networks {
		if seen[n.ID] {
			continue
		}
		seen[n.ID] = true
		list = append(list, n)
	}
	return list, nil
}

func (m *MockDockerEngine) NetworkRemove(ctx context.Context, networkID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	n, ok := m.networks[networkID]
	if !ok {
		return fmt.Errorf("network not found: %s", networkID)
	}
	delete(m.networks, n.ID)
	delete(m.networks, n.Name)
	return nil
}

func (m *MockDockerEngine) NetworkConnect(ctx context.Context, networkID, containerID string, config *network.EndpointSettings) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.containers[containerID]
	if !ok {
		return fmt.Errorf("container not found: %s", containerID)
	}
	if config == nil {
		config = &network.EndpointSettings{NetworkID: networkID}
	}
	if c.Networks == nil {
		c.Networks = make(map[string]*network.EndpointSettings)
	}
	c.Networks[networkID] = config
	return nil
}

func (m *MockDockerEngine) NetworkDisconnect(ctx context.Context, networkID, containerID string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	c, ok := m.containers[containerID]
	if !ok {
		return fmt.Errorf("container not found: %s", containerID)
	}
	delete(c.Networks, networkID)
	return nil
}

func (m *MockDockerEngine) NetworkInspect(ctx context.Context, networkID string, options types.NetworkInspectOptions) (types.NetworkResource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	n, ok := m.networks[networkID]
	if !ok {
		return types.NetworkResource{}, fmt.Errorf("network not found: %s", networkID)
	}
	return n, nil
}

func (m *MockDockerEngine) VolumeInspect(ctx context.Context, volumeID string) (volume.Volume, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	v, ok := m.volumes[volumeID]
	if !ok {
		return volume.Volume{}, fmt.Errorf("volume not found: %s", volumeID)
	}
	return v, nil
}

func (m *MockDockerEngine) VolumeCreate(ctx context.Context, options volume.CreateOptions) (volume.Volume, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name := options.Name
	if name == "" {
		name = m.nextIDStr("vol")
	}
	vol := volume.Volume{
		Name:       name,
		Driver:     options.Driver,
		Labels:     options.Labels,
		Mountpoint: "/var/lib/docker/volumes/" + name + "/_data",
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	m.volumes[name] = vol
	return vol, nil
}

func (m *MockDockerEngine) VolumeList(ctx context.Context, options volume.ListOptions) (volume.ListResponse, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []*volume.Volume
	for _, v := range m.volumes {
		vCopy := v
		list = append(list, &vCopy)
	}
	return volume.ListResponse{Volumes: list}, nil
}

func (m *MockDockerEngine) VolumeRemove(ctx context.Context, volumeID string, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.volumes[volumeID]; !ok {
		return fmt.Errorf("volume not found: %s", volumeID)
	}
	delete(m.volumes, volumeID)
	return nil
}

func (m *MockDockerEngine) ImageList(ctx context.Context, options image.ListOptions) ([]image.Summary, error) {
	return []image.Summary{
		{
			ID:       "sha256:alpine-latest",
			RepoTags: []string{"alpine:latest", "postgres:16", "redis:7"},
			Size:     1024 * 1024 * 50,
		},
	}, nil
}

// ============================================================================
// 2. In-Memory Mock Caddy Server
// ============================================================================

type MockCaddyServer struct {
	Server    *httptest.Server
	mu        sync.RWMutex
	routes    map[string]ingress.CaddyRoute
	upstreams map[string]int
	config    map[string]any
}

func NewMockCaddyServer() *MockCaddyServer {
	mcs := &MockCaddyServer{
		routes:    make(map[string]ingress.CaddyRoute),
		upstreams: make(map[string]int),
		config:    make(map[string]any),
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/id/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/id/")
		if r.Method == http.MethodPut {
			var route ingress.CaddyRoute
			if err := json.NewDecoder(r.Body).Decode(&route); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mcs.mu.Lock()
			mcs.routes[id] = route
			mcs.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		} else if r.Method == http.MethodDelete {
			mcs.mu.Lock()
			delete(mcs.routes, id)
			mcs.mu.Unlock()
			w.WriteHeader(http.StatusOK)
			return
		} else if r.Method == http.MethodGet {
			mcs.mu.RLock()
			route, ok := mcs.routes[id]
			mcs.mu.RUnlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(route)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/load", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mcs.mu.Lock()
		mcs.config = payload
		mcs.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/reverse_proxy/upstreams", func(w http.ResponseWriter, r *http.Request) {
		mcs.mu.RLock()
		defer mcs.mu.RUnlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mcs.upstreams)
	})

	mcs.Server = httptest.NewServer(mux)
	return mcs
}

func (s *MockCaddyServer) URL() string {
	return s.Server.URL
}

func (s *MockCaddyServer) Close() {
	s.Server.Close()
}

func (s *MockCaddyServer) GetRoute(id string) (ingress.CaddyRoute, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.routes[id]
	return r, ok
}

func (s *MockCaddyServer) RouteCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.routes)
}

// ============================================================================
// 3. In-Memory Mock S3 Client & Storage
// ============================================================================

type MockS3Client struct {
	mu        sync.RWMutex
	objects   map[string][]byte
	metadata  map[string]s3.ObjectInfo
	uploadErr error
}

func NewMockS3Client() *MockS3Client {
	return &MockS3Client{
		objects:  make(map[string][]byte),
		metadata: make(map[string]s3.ObjectInfo),
	}
}

func (m *MockS3Client) UploadStreamMultipart(ctx context.Context, key string, reader io.Reader, opts s3.UploadOptions) (*s3.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.uploadErr != nil {
		return nil, m.uploadErr
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("s3 upload read error: %w", err)
	}

	m.objects[key] = data
	info := s3.ObjectInfo{
		Key:          key,
		Size:         int64(len(data)),
		ETag:         fmt.Sprintf("\"etag-%x\"", len(data)),
		LastModified: time.Now().UTC(),
	}
	m.metadata[key] = info
	return &info, nil
}

func (m *MockS3Client) DownloadStream(ctx context.Context, key string) (io.ReadCloser, *s3.ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, ok := m.objects[key]
	if !ok {
		return nil, nil, fmt.Errorf("s3 object not found: %s", key)
	}
	info := m.metadata[key]
	return io.NopCloser(bytes.NewReader(data)), &info, nil
}

func (m *MockS3Client) DeleteObject(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.objects, key)
	delete(m.metadata, key)
	return nil
}

func (m *MockS3Client) DeleteObjects(ctx context.Context, keys []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, k := range keys {
		delete(m.objects, k)
		delete(m.metadata, k)
	}
	return nil
}

func (m *MockS3Client) ListObjects(ctx context.Context, prefix string) ([]s3.ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var list []s3.ObjectInfo
	for k, info := range m.metadata {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			list = append(list, info)
		}
	}
	return list, nil
}

func (m *MockS3Client) PruneRetention(ctx context.Context, prefix string, policy s3.RetentionPolicy) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var matchingKeys []string
	for k := range m.objects {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			matchingKeys = append(matchingKeys, k)
		}
	}

	var deleted []string
	if policy.MaxBackups > 0 && len(matchingKeys) > policy.MaxBackups {
		toDeleteCount := len(matchingKeys) - policy.MaxBackups
		for i := 0; i < toDeleteCount && i < len(matchingKeys); i++ {
			k := matchingKeys[i]
			delete(m.objects, k)
			delete(m.metadata, k)
			deleted = append(deleted, k)
		}
	}
	return deleted, nil
}

func (m *MockS3Client) PruneStaleMultipartUploads(ctx context.Context, maxAge time.Duration) ([]string, error) {
	return nil, nil
}

// ============================================================================
// 4. In-Memory Mock Docker Exec Runner (Multi-DB Streams)
// ============================================================================

type MockExecRunner struct {
	mu           sync.Mutex
	dumpPayload  []byte
	dumpExitCode int
	restoreInput []byte
	capturedCmd  []string
}

func NewMockExecRunner() *MockExecRunner {
	return &MockExecRunner{
		dumpPayload: []byte("CREATE TABLE users (id serial primary key, email text);\nINSERT INTO users (email) VALUES ('test@pikpik.dev');\n"),
	}
}

func (m *MockExecRunner) ExecStreamStdout(ctx context.Context, containerID string, cmd []string, env []string, stdout io.Writer) (int, error) {
	m.mu.Lock()
	m.capturedCmd = cmd
	payload := m.dumpPayload
	code := m.dumpExitCode
	m.mu.Unlock()

	if payload != nil {
		_, _ = stdout.Write(payload)
	}
	return code, nil
}

func (m *MockExecRunner) ExecStreamStdin(ctx context.Context, containerID string, cmd []string, env []string, stdin io.Reader) (int, error) {
	data, _ := io.ReadAll(stdin)
	m.mu.Lock()
	m.capturedCmd = cmd
	m.restoreInput = data
	m.mu.Unlock()
	return 0, nil
}

// ============================================================================
// 5. In-Memory Mock Telemetry Scraper & Docker Collector
// ============================================================================

type MockTelemetryReader struct {
	mu      sync.Mutex
	metrics *telemetry.HostMetrics
}

func NewMockTelemetryReader() *MockTelemetryReader {
	return &MockTelemetryReader{
		metrics: &telemetry.HostMetrics{
			NodeID:        "node-mock-01",
			Timestamp:     time.Now().UTC(),
			CPUPercent:    28.5,
			CPUCores:      8,
			MemTotalBytes: 16 * 1024 * 1024 * 1024,
			MemUsedBytes:  4 * 1024 * 1024 * 1024,
			DiskReadBps:   1024 * 1024 * 10,
			DiskWriteBps:  1024 * 1024 * 5,
			NetRxBps:      1024 * 1024 * 15,
			NetTxBps:      1024 * 1024 * 35,
		},
	}
}

func (m *MockTelemetryReader) ReadHostMetrics(ctx context.Context) (*telemetry.HostMetrics, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := *m.metrics
	res.Timestamp = time.Now().UTC()
	return &res, nil
}
