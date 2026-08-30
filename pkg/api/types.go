package api

import (
	"time"
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

// App Models
type App struct {
	ID               string            `json:"id"`
	ProjectID        string            `json:"project_id,omitempty"`
	StageID          string            `json:"stage_id,omitempty"`
	Name             string            `json:"name"`
	Image            string            `json:"image"`
	Replicas         uint64            `json:"replicas"`
	ContainerPort    int               `json:"container_port,omitempty"`
	Domains          []string          `json:"domains"`
	Env              map[string]string `json:"env,omitempty"`
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
	Name             string            `json:"name"`
	Image            string            `json:"image"`
	Replicas         uint64            `json:"replicas"`
	ContainerPort    int               `json:"container_port,omitempty"`
	Domains          []string          `json:"domains,omitempty"`
	Env              map[string]string `json:"env,omitempty"`
	GitRepoURL       string            `json:"git_repo_url,omitempty"`
	GitBranch        string            `json:"git_branch,omitempty"`
	BuildStrategy    string            `json:"build_strategy,omitempty"`
	DockerfilePath   string            `json:"dockerfile_path,omitempty"`
	PublishDirectory string            `json:"publish_directory,omitempty"`
}

type UpdateAppRequest struct {
	Name             string            `json:"name,omitempty"`
	Image            string            `json:"image,omitempty"`
	Replicas         *uint64           `json:"replicas,omitempty"`
	ContainerPort    *int              `json:"container_port,omitempty"`
	Domains          []string          `json:"domains,omitempty"`
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

// Traffic Splitting Models
type TrafficSplitDTO struct {
	AppID          string            `json:"app_id"`
	Domain         string            `json:"domain"`
	StableUpstream string            `json:"stable_upstream"`
	CanaryUpstream string            `json:"canary_upstream"`
	CanaryPercent  int               `json:"canary_percent"`
	Headers        map[string]string `json:"headers,omitempty"`
	Paths          []string          `json:"paths,omitempty"`
}

type SetTrafficSplitRequest struct {
	Domain         string            `json:"domain,omitempty"`
	StableUpstream string            `json:"stable_upstream,omitempty"`
	CanaryUpstream string            `json:"canary_upstream,omitempty"`
	CanaryPercent  int               `json:"canary_percent"`
	Headers        map[string]string `json:"headers,omitempty"`
	Paths          []string          `json:"paths,omitempty"`
}

// Blue-Green Deployment Models
type BlueGreenDeployRequest struct {
	Image           string            `json:"image"`
	Domain          string            `json:"domain,omitempty"`
	ContainerPort   uint32            `json:"container_port,omitempty"`
	HealthCheckPath string            `json:"health_check_path,omitempty"`
	ProbeTimeoutSec int               `json:"probe_timeout_sec,omitempty"`
	DrainPeriodSec  int               `json:"drain_period_sec,omitempty"`
	CanarySteps     []int             `json:"canary_steps,omitempty"`
	Environment     map[string]string `json:"environment,omitempty"`
}

type BlueGreenDeployResponse struct {
	AppID             string    `json:"app_id"`
	BlueContainerID   string    `json:"blue_container_id,omitempty"`
	GreenContainerID  string    `json:"green_container_id"`
	ActiveContainerID string    `json:"active_container_id"`
	Domain            string    `json:"domain"`
	Status            string    `json:"status"`
	SwappedAt         time.Time `json:"swapped_at"`
	DurationMs        int64     `json:"duration_ms"`
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

// Stack Models
type Stack struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ComposeYAML string    `json:"compose_yaml"`
	Services    []string  `json:"services"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateStackRequest struct {
	Name        string `json:"name"`
	ComposeYAML string `json:"compose_yaml"`
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
	ImagesDeleted       []string `json:"images_deleted,omitempty"`
	ContainersDeleted   []string `json:"containers_deleted,omitempty"`
	VolumesDeleted      []string `json:"volumes_deleted,omitempty"`
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
