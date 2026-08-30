package registry

import (
	"context"
	"time"
)

// StorageBackendType enumerates supported registry storage drivers.
type StorageBackendType string

const (
	StorageBackendLocal StorageBackendType = "local"
	StorageBackendS3    StorageBackendType = "s3"
)

// RegistryConfig defines the operational parameters for the embedded OCI registry.
type RegistryConfig struct {
	Enabled        bool               `json:"enabled"`
	Domain         string             `json:"domain"`         // e.g. "registry.yourdomain.com"
	StorageBackend StorageBackendType `json:"storageBackend"` // "local" or "s3"
	LocalVolume    string             `json:"localVolume"`    // "pikpik_vol_sys_registry_data"
	S3Config       *S3StorageConfig   `json:"s3Config,omitempty"`
	HtpasswdPath   string             `json:"htpasswdPath"`
	InternalPort   int                `json:"internalPort"` // default 5000
}

// S3StorageConfig specifies S3 backend parameters for registry:2.
type S3StorageConfig struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	Region    string `json:"region"`
	Endpoint  string `json:"endpoint"`
	Bucket    string `json:"bucket"`
	Secure    bool   `json:"secure"`
	V4Auth    bool   `json:"v4auth"`
}

// RobotCredential represents a generated robot auth secret for CI push/pull.
type RobotCredential struct {
	ID          string     `json:"id"`
	ProjectID   string     `json:"projectId"`
	Username    string     `json:"username"`
	SecretToken string     `json:"secretToken,omitempty"` // populated only upon creation
	BcryptHash  string     `json:"-"`
	Description string     `json:"description"`
	CreatedAt   time.Time  `json:"createdAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

// RegistryStatus reports the live operational state of the embedded registry.
type RegistryStatus struct {
	IsRunning     bool      `json:"isRunning"`
	ContainerID   string    `json:"containerId"`
	StorageUsage  int64     `json:"storageUsageBytes"`
	TotalImages   int       `json:"totalImages"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
}

// RegistryManager controls the embedded registry container lifecycle and credentials.
type RegistryManager interface {
	// Reconcile ensures the registry container, volumes, configs, and networks are in the desired state.
	Reconcile(ctx context.Context, cfg RegistryConfig) (*RegistryStatus, error)

	// CreateRobotAccount generates a cryptographically secure token and updates htpasswd.
	CreateRobotAccount(ctx context.Context, projectID, description string) (*RobotCredential, error)

	// RevokeRobotAccount deletes credentials and reloads the registry auth table.
	RevokeRobotAccount(ctx context.Context, robotID string) error

	// ListRobotAccounts returns all active robot accounts for a given project.
	ListRobotAccounts(ctx context.Context, projectID string) ([]RobotCredential, error)

	// GetStatus inspects the registry container health and storage metrics.
	GetStatus(ctx context.Context) (*RegistryStatus, error)
}
