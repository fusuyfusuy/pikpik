package api

import (
	"encoding/json"
	"time"

	"github.com/fusuycorp/pikpik/pkg/orchestration"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
)

// Standard Response Envelope
type Response[T any] struct {
	Success bool     `json:"success"`
	Data    T        `json:"data,omitempty"`
	Meta    MetaInfo `json:"meta"`
}

type MetaInfo struct {
	RequestID string    `json:"request_id"`
	Timestamp time.Time `json:"timestamp"`
}

// Canonical Error Details (RFC 7807)
type APIError struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"request_id"`
	DocsURL   string         `json:"docs_url,omitempty"`
}

type ErrorResponse struct {
	Success bool     `json:"success"`
	Error   APIError `json:"error"`
}

// Canonical Error Codes
const (
	ErrCodeUnauthorized            = "UNAUTHORIZED"
	ErrCodeForbidden               = "FORBIDDEN"
	ErrCodeNotFound                = "NOT_FOUND"
	ErrCodeResourceConflict        = "RESOURCE_CONFLICT"
	ErrCodeValidationFailed        = "VALIDATION_FAILED"
	ErrCodeRateLimited             = "RATE_LIMITED"
	ErrCodeDockerEngineUnavailable = "DOCKER_ENGINE_UNAVAILABLE"
	ErrCodeIngressReconcileFailed  = "INGRESS_RECONCILE_FAILED"
	ErrCodeInternalError           = "INTERNAL_ERROR"
)

// Organization Models
type OrganizationDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateOrgRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug,omitempty"`
}

// Project Models
type ProjectDTO struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	AppCount    int       `json:"app_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateProjectRequest struct {
	OrgID       string   `json:"org_id,omitempty"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

type UpdateProjectRequest struct {
	Name        string    `json:"name,omitempty"`
	Slug        string    `json:"slug,omitempty"`
	Description *string   `json:"description,omitempty"`
	Tags        *[]string `json:"tags,omitempty"`
}

// Tag Summary Model
type TagSummary struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// App Models
type App struct {
	ID               string            `json:"id"`
	ProjectID        string            `json:"project_id"`
	ProjectName      string            `json:"project_name,omitempty"`
	StageID          string            `json:"stage_id,omitempty"`
	Name             string            `json:"name"`
	Image            string            `json:"image"`
	Replicas         uint64            `json:"replicas"`
	ContainerPort    int               `json:"container_port,omitempty"`
	Domains          []string          `json:"domains"`
	Env              map[string]string `json:"env,omitempty"`
	Tags             []string          `json:"tags"`
	RuntimeMode      string            `json:"runtime_mode"` // "swarm" or "standalone"
	ComposeYAML      string            `json:"compose_yaml,omitempty"`
	Status           string            `json:"status"`
	GitRepoURL       string            `json:"git_repo_url,omitempty"`
	GitBranch        string            `json:"git_branch,omitempty"`
	BuildStrategy    string            `json:"build_strategy,omitempty"`
	DockerfilePath   string            `json:"dockerfile_path,omitempty"`
	PublishDirectory string            `json:"publish_directory,omitempty"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

type CreateAppRequest struct {
	ProjectID        string            `json:"project_id,omitempty"`
	StageID          string            `json:"stage_id,omitempty"`
	Name             string            `json:"name"`
	Image            string            `json:"image"`
	Replicas         uint64            `json:"replicas"`
	ContainerPort    int               `json:"container_port,omitempty"`
	Domains          []string          `json:"domains,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	RuntimeMode      string            `json:"runtime_mode,omitempty"`
	ComposeYAML      string            `json:"compose_yaml,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	GitRepoURL       string            `json:"git_repo_url,omitempty"`
	GitBranch        string            `json:"git_branch,omitempty"`
	BuildStrategy    string            `json:"build_strategy,omitempty"`
	DockerfilePath   string            `json:"dockerfile_path,omitempty"`
	PublishDirectory string            `json:"publish_directory,omitempty"`
}

type UpdateAppRequest struct {
	ProjectID        *string           `json:"project_id,omitempty"`
	StageID          *string           `json:"stage_id,omitempty"`
	Name             string            `json:"name,omitempty"`
	Image            string            `json:"image,omitempty"`
	Replicas         *uint64           `json:"replicas,omitempty"`
	ContainerPort    *int              `json:"container_port,omitempty"`
	Domains          []string          `json:"domains,omitempty"`
	Tags             *[]string         `json:"tags,omitempty"`
	RuntimeMode      *string           `json:"runtime_mode,omitempty"`
	ComposeYAML      *string           `json:"compose_yaml,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	GitRepoURL       string            `json:"git_repo_url,omitempty"`
	GitBranch        string            `json:"git_branch,omitempty"`
	BuildStrategy    string            `json:"build_strategy,omitempty"`
	DockerfilePath   string            `json:"dockerfile_path,omitempty"`
	PublishDirectory string            `json:"publish_directory,omitempty"`
}

type DeployAppRequest struct {
	Image string `json:"image,omitempty"`
}

type InspectComposeRequest struct {
	ComposeYAML string `json:"compose_yaml"`
}

type InspectComposeResponse struct {
	Services         []orchestration.ComposeServiceInspection `json:"services"`
	Variables        []orchestration.ComposeVariableDef       `json:"variables"`
	ExposedPorts     []uint32                                 `json:"exposed_ports"`
	DeclaredVolumes  []string                                 `json:"declared_volumes"`
	DeclaredNetworks []string                                 `json:"declared_networks"`
	SuggestedRuntime string                                   `json:"suggested_runtime"`
}


// Swarm Node Models
type SwarmNode struct {
	ID           string    `json:"id"`
	Hostname     string    `json:"hostname"`
	Role         string    `json:"role"`         // "manager" | "worker"
	Status       string    `json:"status"`       // "ready" | "down"
	Availability string    `json:"availability"` // "active" | "pause" | "drain"
	EngineVer    string    `json:"engine_version"`
	IPAddress    string    `json:"ip_address"`
	CPUs         int       `json:"cpus"`
	MemoryBytes  int64     `json:"memory_bytes"`
	Leader       bool      `json:"leader"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type UpdateNodeRequest struct {
	Availability string `json:"availability"` // "active" | "pause" | "drain"
}

type JoinTokensResponse struct {
	Manager string `json:"manager"`
	Worker  string `json:"worker"`
}

// Managed Machine Models
type MachineDTO struct {
	ID              string                 `json:"id"`
	Hostname        string                 `json:"hostname"`
	Role            string                 `json:"role"` // "worker", "manager", "standalone"
	PublicIP        string                 `json:"public_ip"`
	PrivateIP       string                 `json:"private_ip"`
	OSKernel        string                 `json:"os_kernel"`
	CPUArch         string                 `json:"cpu_arch"`
	DockerVersion   string                 `json:"docker_version"`
	AgentVersion    string                 `json:"agent_version"`
	Status          string                 `json:"status"` // "online", "degraded", "offline"
	LastSeen        *time.Time             `json:"last_seen,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
	UpdatedAt       time.Time              `json:"updated_at"`
	Metrics         *telemetry.HostMetrics `json:"metrics,omitempty"`
	ContainersCount int                    `json:"containers_count,omitempty"`
}

type EnrollMachineResponse struct {
	Command     string `json:"command"`
	Token       string `json:"token"`
	ServerURL   string `json:"server_url"`
	InstallBash string `json:"install_bash"`
}

type JoinSwarmRequest struct {
	Role        string   `json:"role"` // "worker" | "manager"
	RemoteAddrs []string `json:"remote_addrs,omitempty"`
	JoinToken   string   `json:"join_token,omitempty"`
}

// Stack Models
type Stack struct {
	ID          string                          `json:"id"`
	ProjectID   string                          `json:"project_id"`
	Name        string                          `json:"name"`
	ComposeYAML string                          `json:"compose_yaml"`
	Services    []string                        `json:"services"`
	Status      string                          `json:"status"`
	Containers  []orchestration.ContainerStatus `json:"containers,omitempty"`
	CreatedAt   time.Time                       `json:"created_at"`
	UpdatedAt   time.Time                       `json:"updated_at"`
}

type CreateStackRequest struct {
	ProjectID   string `json:"project_id,omitempty"`
	Name        string `json:"name"`
	ComposeYAML string `json:"compose_yaml"`
}

type UpdateStackRequest struct {
	Name        string `json:"name,omitempty"`
	ComposeYAML string `json:"compose_yaml"`
}

// Network Models
type NetworkDTO struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	Name       string    `json:"name"`
	Driver     string    `json:"driver"`
	Scope      string    `json:"scope"` // "stack", "project", "custom"
	IsExternal bool      `json:"is_external"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CreateNetworkRequest struct {
	ProjectID  string `json:"project_id,omitempty"`
	Name       string `json:"name"`
	Driver     string `json:"driver,omitempty"`
	Scope      string `json:"scope,omitempty"`
	IsExternal bool   `json:"is_external,omitempty"`
}

// Volume Models
type VolumeDTO struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Driver    string    `json:"driver"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateVolumeRequest struct {
	ProjectID string `json:"project_id,omitempty"`
	Name      string `json:"name"`
	Driver    string `json:"driver,omitempty"`
}

// Database Models
type Database struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Engine           string    `json:"engine"` // "postgres" | "mysql" | "mariadb" | "redis" | "mongodb"
	Status           string    `json:"status"`
	Host             string    `json:"host"`
	Port             int       `json:"port"`
	Username         string    `json:"username"`
	Password         string    `json:"password,omitempty"`
	DatabaseName     string    `json:"database_name"`
	MemoryLimitBytes int64     `json:"memory_limit_bytes,omitempty"`
	CPULimit         float64   `json:"cpu_limit,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type CreateDatabaseRequest struct {
	Name             string  `json:"name"`
	Engine           string  `json:"engine"`
	DatabaseName     string  `json:"database_name,omitempty"`
	Username         string  `json:"username,omitempty"`
	Password         string  `json:"password,omitempty"`
	MemoryLimitBytes int64   `json:"memory_limit_bytes,omitempty"`
	CPULimit         float64 `json:"cpu_limit,omitempty"`
}

type UpdateDatabaseRequest struct {
	MemoryLimitBytes *int64   `json:"memory_limit_bytes,omitempty"`
	CPULimit         *float64 `json:"cpu_limit,omitempty"`
}

// Backup Models
type Backup struct {
	ID                string    `json:"id"`
	ServiceID         string    `json:"service_id"`
	S3Key             string    `json:"s3_key"`
	CompressedBytes   int64     `json:"compressed_bytes"`
	UncompressedBytes int64     `json:"uncompressed_bytes"`
	DurationMs        int64     `json:"duration_ms"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}

type CreateBackupRequest struct {
	ServiceID string `json:"service_id"`
}

type CreateBackupScheduleRequest struct {
	ServiceID            string `json:"service_id"`
	CronExpr             string `json:"cron_expr,omitempty"`
	CronExpression       string `json:"cron_expression,omitempty"`
	Engine               string `json:"engine,omitempty"`
	DatabaseType         string `json:"database_type,omitempty"`
	DatabaseName         string `json:"database_name,omitempty"`
	Username             string `json:"username,omitempty"`
	Password             string `json:"password,omitempty"`
	S3Bucket             string `json:"s3_bucket,omitempty"`
	S3Endpoint           string `json:"s3_endpoint,omitempty"`
	S3Region             string `json:"s3_region,omitempty"`
	S3AccessKey          string `json:"s3_access_key,omitempty"`
	S3SecretKey          string `json:"s3_secret_key,omitempty"`
	RetentionHourly      int    `json:"retention_hourly,omitempty"`
	RetentionDaily       int    `json:"retention_daily,omitempty"`
	RetentionWeekly      int    `json:"retention_weekly,omitempty"`
	RetentionMonthly     int    `json:"retention_monthly,omitempty"`
	RetentionDays        int    `json:"retention_days,omitempty"`
	MaxBackups           int    `json:"max_backups,omitempty"`
	Compression          string `json:"compression,omitempty"`
	IsEnabled            *bool  `json:"is_enabled,omitempty"`
}

type UpdateBackupScheduleRequest struct {
	CronExpr             string `json:"cron_expr,omitempty"`
	CronExpression       string `json:"cron_expression,omitempty"`
	Engine               string `json:"engine,omitempty"`
	DatabaseName         string `json:"database_name,omitempty"`
	Username             string `json:"username,omitempty"`
	Password             string `json:"password,omitempty"`
	S3Bucket             string `json:"s3_bucket,omitempty"`
	S3Endpoint           string `json:"s3_endpoint,omitempty"`
	S3Region             string `json:"s3_region,omitempty"`
	S3AccessKey          string `json:"s3_access_key,omitempty"`
	S3SecretKey          string `json:"s3_secret_key,omitempty"`
	RetentionHourly      *int   `json:"retention_hourly,omitempty"`
	RetentionDaily       *int   `json:"retention_daily,omitempty"`
	RetentionWeekly      *int   `json:"retention_weekly,omitempty"`
	RetentionMonthly     *int   `json:"retention_monthly,omitempty"`
	MaxBackups           *int   `json:"max_backups,omitempty"`
	Compression          string `json:"compression,omitempty"`
	IsEnabled            *bool  `json:"is_enabled,omitempty"`
}

type RestoreBackupRequest struct {
	TargetServiceID string `json:"target_service_id,omitempty"`
}

type BackupDestination struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint,omitempty"`
	Prefix          string `json:"prefix,omitempty"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	IsDefault       bool   `json:"is_default"`
}

// Ingress Models
type DomainBinding struct {
	ID        string    `json:"id"`
	AppID     string    `json:"app_id"`
	Domain    string    `json:"domain"`
	AutoTLS   bool      `json:"auto_tls"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type BindDomainRequest struct {
	AppID   string `json:"app_id"`
	Domain  string `json:"domain"`
	AutoTLS bool   `json:"auto_tls"`
}

type CertificateUploadRequest struct {
	Domain  string `json:"domain"`
	CertPEM string `json:"cert_pem"`
	KeyPEM  string `json:"key_pem"`
}

type CaddyDiagnosticsDTO struct {
	Status       string          `json:"status"`
	AdminURL     string          `json:"admin_url"`
	LatencyMs    int64           `json:"latency_ms"`
	ActiveRoutes int             `json:"active_routes"`
	Config       json.RawMessage `json:"config"`
}

// Traffic Split Models
type UpstreamWeight struct {
	Upstream string `json:"upstream"`
	Weight   int    `json:"weight"`
}

type SetTrafficSplitRequest struct {
	Splits    []UpstreamWeight `json:"splits,omitempty"`
	Upstreams []UpstreamWeight `json:"upstreams,omitempty"`
	Reset     bool             `json:"reset,omitempty"`
}

type TrafficSplitResponse struct {
	AppID  string           `json:"app_id"`
	Domain string           `json:"domain,omitempty"`
	Splits []UpstreamWeight `json:"splits"`
}

// Registry Models
type RegistryStatusResponse struct {
	IsRunning     bool      `json:"is_running"`
	ContainerID   string    `json:"container_id,omitempty"`
	StorageBytes  int64     `json:"storage_bytes"`
	Repositories  int       `json:"repositories_count"`
	LastHeartbeat time.Time `json:"last_heartbeat"`
}

type RepositoryCatalogResponse struct {
	Repositories []string            `json:"repositories"`
	Tags         map[string][]string `json:"tags,omitempty"`
}

type RobotCredentialsResponse struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Username    string    `json:"username"`
	SecretToken string    `json:"secret_token,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type CreateRobotRequest struct {
	ProjectID   string `json:"project_id"`
	Description string `json:"description,omitempty"`
}

// System Models
type SystemInfo struct {
	HostOS          string `json:"host_os"`
	DockerVersion   string `json:"docker_version"`
	SwarmActive     bool   `json:"swarm_active"`
	NodesCount      int    `json:"nodes_count"`
	ContainersCount int    `json:"containers_count"`
	TotalMemory     int64  `json:"total_memory_bytes"`
	TotalCPUs       int    `json:"total_cpus"`
}

type DiskUsageInfo struct {
	ImagesBytes           int64 `json:"images_bytes"`
	ContainersBytes       int64 `json:"containers_bytes"`
	VolumesBytes          int64 `json:"volumes_bytes"`
	BuildCacheBytes       int64 `json:"build_cache_bytes"`
	TotalReclaimableBytes int64 `json:"total_reclaimable_bytes"`
}

type PruneRequest struct {
	All        bool `json:"all"`
	Volumes    bool `json:"volumes"`
	BuildCache bool `json:"build_cache"`
}

type PruneResult struct {
	SpaceReclaimedBytes int64    `json:"space_reclaimed_bytes"`
	SpaceReclaimed      int64    `json:"space_reclaimed,omitempty"`
	Deleted             []string `json:"deleted,omitempty"`
	ImagesDeleted       []string `json:"images_deleted,omitempty"`
	ContainersDeleted   []string `json:"containers_deleted,omitempty"`
	VolumesDeleted      []string `json:"volumes_deleted,omitempty"`
	NetworksDeleted     []string `json:"networks_deleted,omitempty"`
}

// Auth Models
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      UserDTO   `json:"user"`
}

type UserDTO struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateTokenRequest struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type APITokenDTO struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Scopes     []string   `json:"scopes"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RawSecret  string     `json:"raw_secret,omitempty"` // populated only on creation
}

// WebSocket Frame Models
type ClientAction struct {
	Action   string         `json:"action"`    // "subscribe" | "unsubscribe" | "ping"
	Channel  string         `json:"channel"`   // "logs" | "stats" | "events" | "pty"
	TargetID string         `json:"target_id"` // app_id | node_id | container_id | "*"
	Params   map[string]any `json:"params,omitempty"`
}

type WSMessage struct {
	Channel  string    `json:"channel"`
	TargetID string    `json:"target_id"`
	Event    string    `json:"event"`
	Data     any       `json:"data"`
	Time     time.Time `json:"timestamp"`
}

type TermResizeMessage struct {
	Cols uint `json:"cols"`
	Rows uint `json:"rows"`
}

type TermSignalMessage struct {
	Signal string `json:"signal"`
}

type TermExitMessage struct {
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}
