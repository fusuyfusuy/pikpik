package store

import (
	"time"
)

// ScopeTier represents the 4-tier inheritance level.
type ScopeTier string

const (
	TierOrg     ScopeTier = "organization"
	TierProject ScopeTier = "project"
	TierStage   ScopeTier = "stage"
	TierService ScopeTier = "service"
)

// Organization represents a top-level tenant group.
type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// User represents an administrative tenant.
type User struct {
	ID             string    `json:"id"`
	Email          string    `json:"email"`
	PasswordHash   string    `json:"-"`
	Role           string    `json:"role"`
	TOTPSecret     string    `json:"-"`
	TOTPEnabled    bool      `json:"totp_enabled"`
	SessionVersion int       `json:"session_version"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Session represents an authenticated web UI session.
type Session struct {
	ID        string    `json:"id"` // Session Token Hash (sha256)
	UserID    string    `json:"user_id"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// APIToken represents a machine-to-machine scoped authorization token.
type APIToken struct {
	ID             string     `json:"id"`
	UserID         string     `json:"user_id"`
	Name           string     `json:"name"`
	Prefix         string     `json:"prefix"` // First 12 chars e.g. "pik_live_a1b2"
	TokenHash      string     `json:"-"`      // SHA-256 hex digest
	Scopes         []string   `json:"scopes"`
	SessionVersion int        `json:"-"` // User's session_version at issuance; invalidated once it falls behind
	LastUsedAt     *time.Time `json:"last_used_at"`
	ExpiresAt      *time.Time `json:"expires_at"` // NULL = Never expires
	CreatedAt      time.Time  `json:"created_at"`
}

// Project represents an application grouping.
type Project struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Stage represents an environment stage (e.g. production, staging).
type Stage struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Service represents a workload container or database.
type Service struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"project_id"`
	StageID          string    `json:"stage_id"`
	Name             string    `json:"name"`
	Slug             string    `json:"slug"`
	Type             string    `json:"type"` // "app", "database", "worker", "job"
	Image            string    `json:"image"`
	Replicas         int       `json:"replicas"`
	ContainerPort    int       `json:"container_port"`
	DomainNames      []string  `json:"domain_names"`
	DeployTokenHash  string    `json:"-"`
	Status           string    `json:"status"` // "idle", "deploying", "running", "unhealthy", "stopped", "failed"
	GitRepoURL       string    `json:"git_repo_url,omitempty"`
	GitBranch        string    `json:"git_branch,omitempty"`
	BuildStrategy    string    `json:"build_strategy,omitempty"`
	DockerfilePath   string    `json:"dockerfile_path,omitempty"`
	PublishDirectory string    `json:"publish_directory,omitempty"`
	Tags             []string  `json:"tags"`
	RuntimeMode      string    `json:"runtime_mode"` // "swarm" or "standalone"
	ComposeYAML      string    `json:"compose_yaml,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// EnvVar represents an encrypted environment variable at any hierarchy tier.
type EnvVar struct {
	ID             string    `json:"id"`
	ScopeTier      ScopeTier `json:"scope_tier"`
	ResourceID     string    `json:"resource_id"` // Target org_id, project_id, stage_id, or service_id
	Key            string    `json:"key"`
	ValueEncrypted string    `json:"-"` // Encrypted format: v1:base64(iv):base64(authTag):base64(ciphertext)
	IsSecret       bool      `json:"is_secret"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Volume represents a persistent volume or config mount.
type Volume struct {
	ID                     string    `json:"id"`
	ProjectID              string    `json:"project_id"`
	ServiceID              string    `json:"service_id"`
	Name                   string    `json:"name"`
	Slug                   string    `json:"slug"`
	MountPath              string    `json:"mount_path"`
	Type                   string    `json:"type"` // "named", "bind", "file"
	HostPath               string    `json:"host_path,omitempty"`
	ConfigContentEncrypted string    `json:"-"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// Deployment represents a rollout execution for a service.
type Deployment struct {
	ID          string     `json:"id"`
	ServiceID   string     `json:"service_id"`
	ImageTag    string     `json:"image_tag"`
	CommitSHA   string     `json:"commit_sha,omitempty"`
	Status      string     `json:"status"` // "queued", "preparing", "starting", "healthy", "failed", "rolled_back"
	LogsSummary string     `json:"logs_summary,omitempty"`
	InitiatedBy string     `json:"initiated_by"` // user_id or 'api_token:<prefix>' or 'webhook'
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

// BackupConfig represents continuous or scheduled backup settings for a database service.
type BackupConfig struct {
	ID                   string    `json:"id"`
	ServiceID            string    `json:"service_id"`
	S3Endpoint           string    `json:"s3_endpoint"`
	S3Bucket             string    `json:"s3_bucket"`
	S3Region             string    `json:"s3_region"`
	S3AccessKey          string    `json:"s3_access_key"`
	S3SecretKeyEncrypted string    `json:"-"`
	CronExpr             string    `json:"cron_expr"`
	RetentionDays        int       `json:"retention_days"`
	IsEnabled            bool      `json:"is_enabled"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// BackupExecution represents an individual backup run log.
type BackupExecution struct {
	ID            string    `json:"id"`
	ConfigID      string    `json:"config_id"`
	ServiceID     string    `json:"service_id"`
	S3Key         string    `json:"s3_key"`
	BytesStreamed int64     `json:"bytes_streamed"`
	DurationMS    int64     `json:"duration_ms"`
	Status        string    `json:"status"` // "in_progress", "completed", "failed"
	ErrorMessage  string    `json:"error_message,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// AuditLog represents an immutable security audit event.
type AuditLog struct {
	ID           string    `json:"id"`
	UserID       string    `json:"user_id,omitempty"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Metadata     string    `json:"metadata"` // JSON string
	IPAddress    string    `json:"ip_address"`
	CreatedAt    time.Time `json:"created_at"`
}

// BackupSchedule represents a recurring multi-database backup schedule.
type BackupSchedule struct {
	ID                   string     `json:"id"`
	ServiceID            string     `json:"service_id"`
	CronExpr             string     `json:"cron_expr"`
	Engine               string     `json:"engine"`
	DatabaseName         string     `json:"database_name"`
	Username             string     `json:"username"`
	PasswordEncrypted    string     `json:"-"`
	S3Bucket             string     `json:"s3_bucket"`
	S3Endpoint           string     `json:"s3_endpoint,omitempty"`
	S3Region             string     `json:"s3_region,omitempty"`
	S3AccessKey          string     `json:"s3_access_key,omitempty"`
	S3SecretKeyEncrypted string     `json:"-"`
	RetentionHourly      int        `json:"retention_hourly"`
	RetentionDaily       int        `json:"retention_daily"`
	RetentionWeekly      int        `json:"retention_weekly"`
	RetentionMonthly     int        `json:"retention_monthly"`
	MaxBackups           int        `json:"max_backups"`
	Compression          string     `json:"compression"` // "gzip" or "zstd"
	IsEnabled            bool       `json:"is_enabled"`
	LastRunAt            *time.Time `json:"last_run_at,omitempty"`
	NextRunAt            *time.Time `json:"next_run_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// Stack represents a multi-container compose stack grouping within a project.
type Stack struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Name        string    `json:"name"`
	ComposeYAML string    `json:"compose_yaml"`
	Status      string    `json:"status"` // "stopped", "deploying", "running", "degraded", "failed"
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ManagedNetwork represents a project-level or stack-isolated virtual network.
type ManagedNetwork struct {
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	Name       string    `json:"name"`
	Driver     string    `json:"driver"`
	Scope      string    `json:"scope"` // "stack", "project", "custom"
	IsExternal bool      `json:"is_external"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ManagedVolume represents a persistent storage volume managed by pikpik.
type ManagedVolume struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Driver    string    `json:"driver"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ManagedMachine represents a remote host running pikpik-agent or swarm worker/manager.
type ManagedMachine struct {
	ID            string     `json:"id"`
	Hostname      string     `json:"hostname"`
	Role          string     `json:"role"` // "worker", "manager", "standalone"
	PublicIP      string     `json:"public_ip"`
	PrivateIP     string     `json:"private_ip"`
	OSKernel      string     `json:"os_kernel"`
	CPUArch       string     `json:"cpu_arch"`
	DockerVersion string     `json:"docker_version"`
	AgentVersion  string     `json:"agent_version"`
	Status        string     `json:"status"` // "online", "degraded", "offline"
	LastSeen      *time.Time `json:"last_seen,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}


