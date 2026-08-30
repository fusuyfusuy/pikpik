package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var (
	// ErrNotFound is returned when a requested record does not exist.
	ErrNotFound = errors.New("store: record not found")

	// ErrDuplicateKey is returned when an insert/update violates a unique constraint.
	ErrDuplicateKey = errors.New("store: duplicate key conflict")

	// ErrOptimisticLock is returned when an optimistic concurrency check fails.
	ErrOptimisticLock = errors.New("store: optimistic lock violation")

	// ErrTransactionClosed is returned when an operation is executed on a closed transaction.
	ErrTransactionClosed = errors.New("store: transaction already closed")

	// ErrMigrationChecksumMismatch is returned when an applied migration's checksum differs from disk.
	ErrMigrationChecksumMismatch = errors.New("store: migration checksum mismatch")
)

// Store aggregates all database sub-stores and transaction lifecycle management.
type Store interface {
	Organizations() OrganizationStore
	Users() UserStore
	Sessions() SessionStore
	APITokens() APITokenStore
	Projects() ProjectStore
	Stages() StageStore
	Services() ServiceStore
	EnvVars() EnvVarStore
	Volumes() VolumeStore
	Deployments() DeploymentStore
	Backups() BackupStore
	Audit() AuditStore
	Builds() BuildStore
	GitHubInstallations() GitHubInstallationStore

	// WithTx executes the supplied operation inside an atomic database transaction.
	WithTx(ctx context.Context, fn func(tx Store) error) error

	// Ping verifies database connectivity and WAL state.
	Ping(ctx context.Context) error

	// Close terminates the database connection pool cleanly.
	Close() error

	// DB returns the underlying sql.DB instance (or nil if in a transaction).
	DB() *sql.DB
}

// OrganizationStore handles organization persistence.
type OrganizationStore interface {
	Create(ctx context.Context, org *Organization) error
	GetByID(ctx context.Context, id string) (*Organization, error)
	GetBySlug(ctx context.Context, slug string) (*Organization, error)
	List(ctx context.Context) ([]*Organization, error)
	Delete(ctx context.Context, id string) error
}

// UserStore handles administrative tenant persistence.
type UserStore interface {
	Create(ctx context.Context, user *User) error
	GetByID(ctx context.Context, id string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	UpdatePassword(ctx context.Context, id string, passwordHash string, bumpSession bool) error
	Count(ctx context.Context) (int64, error)
	Delete(ctx context.Context, id string) error
}

// SessionStore handles web UI session persistence.
type SessionStore interface {
	Create(ctx context.Context, session *Session) error
	GetByID(ctx context.Context, id string) (*Session, error)
	Delete(ctx context.Context, id string) error
	DeleteByUserID(ctx context.Context, userID string) error
	CleanExpired(ctx context.Context) (int64, error)
}

// APITokenStore handles scoped API token persistence and validation tracking.
type APITokenStore interface {
	Create(ctx context.Context, token *APIToken) error
	GetByID(ctx context.Context, id string) (*APIToken, error)
	GetByHash(ctx context.Context, tokenHash string) (*APIToken, error)
	ListByUser(ctx context.Context, userID string) ([]*APIToken, error)
	TouchLastUsed(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

// ProjectStore handles project grouping persistence.
type ProjectStore interface {
	Create(ctx context.Context, p *Project) error
	GetByID(ctx context.Context, id string) (*Project, error)
	GetBySlug(ctx context.Context, slug string) (*Project, error)
	List(ctx context.Context, orgID string) ([]*Project, error)
	Delete(ctx context.Context, id string) error
}

// StageStore handles environment stage persistence (e.g. staging, prod).
type StageStore interface {
	Create(ctx context.Context, s *Stage) error
	GetByID(ctx context.Context, id string) (*Stage, error)
	GetBySlug(ctx context.Context, projectID, slug string) (*Stage, error)
	ListByProject(ctx context.Context, projectID string) ([]*Stage, error)
	Delete(ctx context.Context, id string) error
}

// ServiceStore handles container and database workload persistence.
type ServiceStore interface {
	Create(ctx context.Context, s *Service) error
	GetByID(ctx context.Context, id string) (*Service, error)
	GetBySlug(ctx context.Context, projectID, stageID, slug string) (*Service, error)
	ListByStage(ctx context.Context, stageID string) ([]*Service, error)
	UpdateStatus(ctx context.Context, id string, status string) error
	Update(ctx context.Context, s *Service) error
	Delete(ctx context.Context, id string) error
}

// EnvVarStore handles hierarchical environment variable persistence.
type EnvVarStore interface {
	Set(ctx context.Context, v *EnvVar) error
	Get(ctx context.Context, tier ScopeTier, resourceID, key string) (*EnvVar, error)
	ListByResource(ctx context.Context, tier ScopeTier, resourceID string) ([]*EnvVar, error)
	Delete(ctx context.Context, tier ScopeTier, resourceID, key string) error
}

// VolumeStore handles persistent storage volume metadata.
type VolumeStore interface {
	Create(ctx context.Context, v *Volume) error
	GetByID(ctx context.Context, id string) (*Volume, error)
	ListByService(ctx context.Context, serviceID string) ([]*Volume, error)
	Delete(ctx context.Context, id string) error
}

// DeploymentStore handles service deployment event records.
type DeploymentStore interface {
	Create(ctx context.Context, d *Deployment) error
	GetByID(ctx context.Context, id string) (*Deployment, error)
	ListByService(ctx context.Context, serviceID string, limit int) ([]*Deployment, error)
	UpdateStatus(ctx context.Context, id string, status string, finishedAt *time.Time, logsSummary *string) error
}

// BackupStore handles database backup configs and execution records.
type BackupStore interface {
	CreateConfig(ctx context.Context, c *BackupConfig) error
	GetConfigByService(ctx context.Context, serviceID string) (*BackupConfig, error)
	UpdateConfig(ctx context.Context, c *BackupConfig) error
	RecordExecution(ctx context.Context, exec *BackupExecution) error
	ListExecutions(ctx context.Context, serviceID string, limit int) ([]*BackupExecution, error)
}

// AuditStore handles immutable audit logs.
type AuditStore interface {
	Record(ctx context.Context, userID, action, resType, resID, metadataJSON, ip string) error
	ListByResource(ctx context.Context, resType, resID string, limit int) ([]*AuditLog, error)
}
