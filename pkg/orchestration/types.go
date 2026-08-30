package orchestration

import (
	"time"

	"github.com/docker/docker/api/types/swarm"
)

// RuntimeMode defines the active operational mode of the orchestrator.
type RuntimeMode string

const (
	ModeStandalone   RuntimeMode = "standalone"
	ModeSwarmLeader  RuntimeMode = "swarm_leader"
	ModeSwarmWorker  RuntimeMode = "swarm_worker"
	ModeDisconnected RuntimeMode = "disconnected"
)

// ResourceRequirements defines compute allocations.
type ResourceRequirements struct {
	CPULimit      int64 `json:"cpuLimit"`      // In NanoCPUs (e.g. 1.0 CPU = 1e9)
	MemoryLimit   int64 `json:"memoryLimit"`   // In bytes (e.g. 512MB = 512 * 1024 * 1024)
	CPUReserve    int64 `json:"cpuReserve"`    // In NanoCPUs
	MemoryReserve int64 `json:"memoryReserve"` // In bytes
}

// HealthCheckConfig defines active container probing.
type HealthCheckConfig struct {
	Test        []string      `json:"test"`        // e.g. ["CMD-SHELL", "curl -f http://localhost:3000/health || exit 1"]
	Interval    time.Duration `json:"interval"`    // e.g. 10s
	Timeout     time.Duration `json:"timeout"`     // e.g. 5s
	StartPeriod time.Duration `json:"startPeriod"` // e.g. 15s
	Retries     int           `json:"retries"`     // e.g. 3
}

// VolumeMountSpec defines filesystem bindings.
type VolumeMountSpec struct {
	Type     string `json:"type"`     // "volume", "bind", "tmpfs"
	Source   string `json:"source"`   // Volume name or host path
	Target   string `json:"target"`   // In-container destination path
	ReadOnly bool   `json:"readOnly"` // Read only flag
}

// PortMappingSpec defines internal routing ports or exposed ports.
type PortMappingSpec struct {
	ContainerPort uint32 `json:"containerPort"`
	HostPort      uint32 `json:"hostPort,omitempty"`
	Protocol      string `json:"protocol"` // "tcp", "udp"
}

// PlacementConstraint represents a parsed Swarm scheduling rule.
type PlacementConstraint struct {
	Field    string `json:"field"`    // "node.role", "node.labels.<key>", "node.hostname", "node.id"
	Operator string `json:"operator"` // "==", "!="
	Value    string `json:"value"`    // "manager", "worker", "gpu-enabled", etc.
}

// RollingUpdateConfig configures zero-downtime rolling deploys.
type RollingUpdateConfig struct {
	Order           string        `json:"order"`           // "start-first" (default) or "stop-first"
	Parallelism     uint64        `json:"parallelism"`     // Max simultaneous tasks updated (default: 1)
	Delay           time.Duration `json:"delay"`           // Delay between updating task batches
	FailureAction   string        `json:"failureAction"`   // "rollback", "pause", "continue"
	Monitor         time.Duration `json:"monitor"`         // Duration to monitor each updated task for health (e.g. 10s)
	MaxFailureRatio float32       `json:"maxFailureRatio"` // Failure rate tolerance (0.0 = zero tolerance)
}

// RollingUpdateResult captures the outcome of a zero-downtime rolling deployment.
type RollingUpdateResult struct {
	OldContainerID string        `json:"oldContainerId,omitempty"`
	NewContainerID string        `json:"newContainerId"`
	SwappedAt      time.Time     `json:"swappedAt"`
	Duration       time.Duration `json:"duration"`
	Healthy        bool          `json:"healthy"`
}

// RestartPolicySpec defines the restart behavior for services/containers.
type RestartPolicySpec struct {
	Condition   string        `json:"condition"`   // "none", "on-failure", "any"
	MaxAttempts uint64        `json:"maxAttempts"` // Max retry attempts
	Delay       time.Duration `json:"delay"`       // Delay between attempts
	Window      time.Duration `json:"window"`      // Window to evaluate restart success
}

// SecretAttachment binds a Swarm secret into a service.
type SecretAttachment struct {
	SecretID       string `json:"secretId"`
	SecretName     string `json:"secretName"`
	TargetFileName string `json:"targetFileName"`
	TargetUID      string `json:"targetUid,omitempty"`
	TargetGID      string `json:"targetGid,omitempty"`
	TargetMode     uint32 `json:"targetMode,omitempty"`
}

// ConfigAttachment binds a Swarm config into a service.
type ConfigAttachment struct {
	ConfigID       string `json:"configId"`
	ConfigName     string `json:"configName"`
	TargetFileName string `json:"targetFileName"`
	TargetUID      string `json:"targetUid,omitempty"`
	TargetGID      string `json:"targetGid,omitempty"`
	TargetMode     uint32 `json:"targetMode,omitempty"`
}

// ServiceSpec specifies declarative Swarm service configuration.
type ServiceSpec struct {
	ID                string                `json:"id"`
	Name              string                `json:"name"`
	ProjectID         string                `json:"projectId"`
	Image             string                `json:"image"`
	Replicas          uint64                `json:"replicas"`
	Environment       map[string]string     `json:"environment"`
	Secrets           []SecretAttachment    `json:"secrets,omitempty"`
	Configs           []ConfigAttachment    `json:"configs,omitempty"`
	Mounts            []VolumeMountSpec     `json:"mounts,omitempty"`
	Networks          []string              `json:"networks,omitempty"`
	Ports             []PortMappingSpec     `json:"ports,omitempty"`
	Resources         ResourceRequirements  `json:"resources"`
	HealthCheck       *HealthCheckConfig    `json:"healthCheck,omitempty"`
	Constraints       []string              `json:"constraints,omitempty"` // Raw constraints: "node.role == worker"
	ParsedConstraints []PlacementConstraint `json:"parsedConstraints,omitempty"`
	UpdateConfig      RollingUpdateConfig   `json:"updateConfig"`
	RestartPolicy     RestartPolicySpec     `json:"restartPolicy"`
	StopGracePeriod   time.Duration         `json:"stopGracePeriod"`
	Labels            map[string]string     `json:"labels,omitempty"`
}

// ContainerSpec specifies standalone container configuration.
type ContainerSpec struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	ProjectID     string               `json:"projectId"`
	Image         string               `json:"image"`
	Environment   map[string]string    `json:"environment,omitempty"`
	Mounts        []VolumeMountSpec    `json:"mounts,omitempty"`
	Networks      []string             `json:"networks,omitempty"`
	ExposedPorts  []PortMappingSpec    `json:"exposedPorts,omitempty"`
	Resources     ResourceRequirements `json:"resources"`
	HealthCheck   *HealthCheckConfig   `json:"healthCheck,omitempty"`
	RestartPolicy string               `json:"restartPolicy,omitempty"` // "no", "always", "on-failure", "unless-stopped"
	StopTimeout   time.Duration        `json:"stopTimeout,omitempty"`
	Labels        map[string]string    `json:"labels,omitempty"`
	User          string               `json:"user,omitempty"`
	WorkingDir    string               `json:"workingDir,omitempty"`
	Command       []string             `json:"command,omitempty"`
	Entrypoint    []string             `json:"entrypoint,omitempty"`
}

// ContainerStatus represents the runtime status of a container.
type ContainerStatus struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Image     string            `json:"image"`
	State     string            `json:"state"` // "running", "exited", "created", "paused"
	Status    string            `json:"status"`
	Health    string            `json:"health,omitempty"` // "healthy", "unhealthy", "starting", "none"
	CreatedAt time.Time         `json:"createdAt"`
	IPAddress string            `json:"ipAddress,omitempty"`
	Ports     []PortMappingSpec `json:"ports,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// ServiceStatus represents physical Swarm service state.
type ServiceStatus struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Image           string            `json:"image"`
	Replicas        uint64            `json:"replicas"`
	DesiredReplicas uint64            `json:"desiredReplicas"`
	RunningReplicas uint64            `json:"runningReplicas"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
	Version         uint64            `json:"version"`
	Labels          map[string]string `json:"labels,omitempty"`
	Constraints     []string          `json:"constraints,omitempty"`
}

// NodeStatus represents physical Swarm node state.
type NodeStatus struct {
	ID            string            `json:"id"`
	Hostname      string            `json:"hostname"`
	Role          string            `json:"role"`         // "manager", "worker"
	State         string            `json:"state"`        // "ready", "down", "disconnected"
	Availability  string            `json:"availability"` // "active", "pause", "drain"
	Reachability  string            `json:"reachability"` // "reachable", "unreachable"
	IPAddress     string            `json:"ipAddress"`
	EngineVersion string            `json:"engineVersion"`
	Labels        map[string]string `json:"labels,omitempty"`
	EngineLabels  map[string]string `json:"engineLabels,omitempty"`
	NanoCPUs      int64             `json:"nanoCPUs"`
	MemoryBytes   int64             `json:"memoryBytes"`
}

// NodeSpec represents editable node properties in Swarm.
type NodeSpec struct {
	Availability string            `json:"availability"` // "active", "pause", "drain"
	Role         string            `json:"role"`         // "manager", "worker"
	Labels       map[string]string `json:"labels,omitempty"`
}

// TaskStatus represents an individual replica task instance in Swarm.
type TaskStatus struct {
	ID           string    `json:"id"`
	ServiceID    string    `json:"serviceId"`
	NodeID       string    `json:"nodeId"`
	Slot         int       `json:"slot"`
	State        string    `json:"state"` // "running", "preparing", "starting", "failed", "shutdown"
	DesiredState string    `json:"desiredState"`
	Message      string    `json:"message"`
	Err          string    `json:"err,omitempty"`
	ContainerID  string    `json:"containerId,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ListOptions controls resource filtering.
type ListOptions struct {
	ProjectID string            `json:"projectId,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	All       bool              `json:"all,omitempty"`
}

// LogOptions specifies log retrieval and streaming options.
type LogOptions struct {
	Follow     bool   `json:"follow"`
	Tail       string `json:"tail,omitempty"`
	Since      string `json:"since,omitempty"`
	Timestamps bool   `json:"timestamps"`
	ShowStdout bool   `json:"showStdout"`
	ShowStderr bool   `json:"showStderr"`
	Details    bool   `json:"details"`
}

// SwarmInitRequest parameters for initializing a Swarm cluster.
type SwarmInitRequest struct {
	ListenAddr      string   `json:"listenAddr"`
	AdvertiseAddr   string   `json:"advertiseAddr"`
	DataPathAddr    string   `json:"dataPathAddr,omitempty"`
	DefaultAddrPool []string `json:"defaultAddrPool,omitempty"`
	ForceNewCluster bool     `json:"forceNewCluster,omitempty"`
}

// SwarmJoinRequest parameters for joining an existing Swarm cluster.
type SwarmJoinRequest struct {
	ListenAddr    string   `json:"listenAddr"`
	AdvertiseAddr string   `json:"advertiseAddr"`
	RemoteAddrs   []string `json:"remoteAddrs"`
	JoinToken     string   `json:"joinToken"`
}

// ClusterInfo summarizes Swarm cluster metadata.
type ClusterInfo struct {
	ID         string           `json:"id"`
	CreatedAt  time.Time        `json:"createdAt"`
	UpdatedAt  time.Time        `json:"updatedAt"`
	Spec       swarm.Spec       `json:"spec"`
	JoinTokens swarm.JoinTokens `json:"joinTokens"`
	Nodes      int              `json:"nodes"`
	Managers   int              `json:"managers"`
	Workers    int              `json:"workers"`
}

// ComposeVariableDef describes a variable placeholder detected in a Compose file.
type ComposeVariableDef struct {
	Name         string `json:"name"`
	DefaultValue string `json:"defaultValue,omitempty"`
	IsSecret     bool   `json:"isSecret"`
	Required     bool   `json:"required"`
}

// ComposeServiceInspection describes a service parsed from a Compose file.
type ComposeServiceInspection struct {
	Name            string            `json:"name"`
	Image           string            `json:"image"`
	Ports           []PortMappingSpec `json:"ports,omitempty"`
	Volumes         []string          `json:"volumes,omitempty"`
	Networks        []string          `json:"networks,omitempty"`
	EnvironmentKeys []string          `json:"environmentKeys,omitempty"`
	HasDeployBlock  bool              `json:"hasDeployBlock"`
	Replicas        int               `json:"replicas"`
}

// ComposeInspectionResult provides detailed AST inspection of a Compose or Stack blueprint.
type ComposeInspectionResult struct {
	Services         []ComposeServiceInspection `json:"services"`
	Variables        []ComposeVariableDef       `json:"variables"`
	ExposedPorts     []uint32                   `json:"exposedPorts"`
	DeclaredVolumes  []string                   `json:"declaredVolumes"`
	DeclaredNetworks []string                   `json:"declaredNetworks"`
	SuggestedRuntime string                     `json:"suggestedRuntime"` // "swarm" or "standalone"
}

// ComposeServiceDef defines an individual service within a Compose stack.
type ComposeServiceDef struct {
	Name        string               `json:"name"`
	Image       string               `json:"image"`
	Build       string               `json:"build,omitempty"`
	Command     []string             `json:"command,omitempty"`
	Entrypoint  []string             `json:"entrypoint,omitempty"`
	Environment map[string]string    `json:"environment,omitempty"`
	DependsOn   []string             `json:"dependsOn,omitempty"`
	Ports       []PortMappingSpec    `json:"ports,omitempty"`
	Mounts      []VolumeMountSpec    `json:"mounts,omitempty"`
	Networks    []string             `json:"networks,omitempty"`
	Resources   ResourceRequirements `json:"resources,omitempty"`
	HealthCheck *HealthCheckConfig   `json:"healthCheck,omitempty"`
	Restart     string               `json:"restart,omitempty"`
	Labels      map[string]string    `json:"labels,omitempty"`
}

// ComposeStackSpec defines a multi-container Compose stack specification.
type ComposeStackSpec struct {
	Name      string                       `json:"name"`
	ProjectID string                       `json:"projectId"`
	RawYAML   string                       `json:"rawYaml,omitempty"`
	Services  map[string]ComposeServiceDef `json:"services"`
	Networks  []string                     `json:"networks,omitempty"`
	Volumes   []string                     `json:"volumes,omitempty"`
	EnvVars   map[string]string            `json:"envVars,omitempty"`
}

// StackDeploymentResult contains the output of a stack deployment.
type StackDeploymentResult struct {
	StackName        string    `json:"stackName"`
	ServicesDeployed []string  `json:"servicesDeployed"`
	CreatedNetworks  []string  `json:"createdNetworks"`
	CreatedVolumes   []string  `json:"createdVolumes"`
	DeployedAt       time.Time `json:"deployedAt"`
	Errors           []string  `json:"errors,omitempty"`
}

// StackStatus represents the runtime status of a stack.
type StackStatus struct {
	Name       string            `json:"name"`
	ProjectID  string            `json:"projectId"`
	Services   []ServiceStatus   `json:"services,omitempty"`
	Containers []ContainerStatus `json:"containers,omitempty"`
	State      string            `json:"state"`
	CreatedAt  time.Time         `json:"createdAt"`
}

// StackSummary provides a lightweight summary of an existing stack.
type StackSummary struct {
	Name            string    `json:"name"`
	ProjectID       string    `json:"projectId"`
	ServicesCount   int       `json:"servicesCount"`
	ContainersCount int       `json:"containersCount"`
	State           string    `json:"state"`
	CreatedAt       time.Time `json:"createdAt"`
}
