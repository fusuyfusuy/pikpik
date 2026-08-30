package orchestration

import (
	"context"
	"io"
	"time"

	"github.com/docker/docker/client"
)

// Orchestrator provides a high-level, mode-agnostic abstraction for deploying and managing workloads.
type Orchestrator interface {
	Mode() RuntimeMode
	Ping(ctx context.Context) error
	Close() error
	RawClient() client.CommonAPIClient

	// Sub-manager accessors
	Containers() ContainerManager
	Swarm() SwarmManager
	Stacks() StackManager
	Logs() LogStreamer
}

// ContainerManager handles standalone container lifecycle without shelling.
type ContainerManager interface {
	Create(ctx context.Context, spec ContainerSpec) (string, error)
	Start(ctx context.Context, containerID string) error
	Stop(ctx context.Context, containerID string, timeout time.Duration) error
	Restart(ctx context.Context, containerID string, timeout time.Duration) error
	Remove(ctx context.Context, containerID string, force bool, removeVolumes bool) error
	Inspect(ctx context.Context, containerID string) (*ContainerStatus, error)
	List(ctx context.Context, opts ListOptions) ([]ContainerStatus, error)
	DeployWithRollingUpdate(ctx context.Context, spec ContainerSpec, updateCfg RollingUpdateConfig) (*RollingUpdateResult, error)
}

// SwarmManager handles multi-node Docker Swarm services, nodes, and cluster lifecycle.
type SwarmManager interface {
	InitCluster(ctx context.Context, req SwarmInitRequest) (string, error)
	JoinCluster(ctx context.Context, req SwarmJoinRequest) error
	LeaveCluster(ctx context.Context, force bool) error
	GetClusterInfo(ctx context.Context) (*ClusterInfo, error)

	// Service lifecycle
	CreateService(ctx context.Context, spec ServiceSpec) (string, error)
	UpdateService(ctx context.Context, serviceID string, version uint64, spec ServiceSpec) error
	RemoveService(ctx context.Context, serviceID string) error
	InspectService(ctx context.Context, serviceID string) (*ServiceStatus, error)
	ListServices(ctx context.Context, opts ListOptions) ([]ServiceStatus, error)
	ScaleService(ctx context.Context, serviceID string, replicas uint64) error

	// Task & Node lifecycle
	ListTasks(ctx context.Context, opts ListOptions) ([]TaskStatus, error)
	ListNodes(ctx context.Context) ([]NodeStatus, error)
	UpdateNode(ctx context.Context, nodeID string, version uint64, spec NodeSpec) error
}

// StackManager handles Compose v2 multi-container stacks via direct socket integration.
type StackManager interface {
	DeployStack(ctx context.Context, spec ComposeStackSpec) (*StackDeploymentResult, error)
	RemoveStack(ctx context.Context, stackName string) error
	InspectStack(ctx context.Context, stackName string) (*StackStatus, error)
	ListStacks(ctx context.Context) ([]StackSummary, error)
}

// LogStreamer streams demultiplexed stdout/stderr from containers and swarm tasks.
type LogStreamer interface {
	StreamContainerLogs(ctx context.Context, containerID string, opts LogOptions) (io.ReadCloser, error)
	StreamServiceLogs(ctx context.Context, serviceID string, opts LogOptions) (io.ReadCloser, error)
	StreamDemux(ctx context.Context, containerID string, opts LogOptions, stdout, stderr io.Writer) error
}
