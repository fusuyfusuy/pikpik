package backup

import (
	"context"
	"time"
)

// DatabaseEngine enumerates supported database engines.
type DatabaseEngine string

const (
	EnginePostgres17 DatabaseEngine = "postgres:17"
	EngineMySQL84    DatabaseEngine = "mysql:8.4"
	EngineMariaDB114 DatabaseEngine = "mariadb:11.4"
	EngineMongo70    DatabaseEngine = "mongo:7.0"
	EngineRedis74    DatabaseEngine = "redis:7.4"
)

// BackupJobConfig specifies target database parameters and destination bucket.
type BackupJobConfig struct {
	BackupID       string         `json:"backupId"`
	ProjectSlug    string         `json:"projectSlug"`
	ServiceSlug    string         `json:"serviceSlug"`
	ContainerID    string         `json:"containerId"`
	Engine         DatabaseEngine `json:"engine"`
	DatabaseName   string         `json:"databaseName"`
	Username       string         `json:"username"`
	Password       string         `json:"-"` // injected via exec env, never logged
	S3Bucket       string         `json:"s3Bucket"`
	Compression    string         `json:"compression"` // "gzip" or "zstd"
	RetentionRules RetentionRules `json:"retentionRules"`
}

// RetentionRules defines lifecycle pruning thresholds.
type RetentionRules struct {
	KeepHourly  int `json:"keepHourly"`
	KeepDaily   int `json:"keepDaily"`
	KeepWeekly  int `json:"keepWeekly"`
	KeepMonthly int `json:"keepMonthly"`
	MaxBackups  int `json:"maxBackups"`
}

// BackupResult summarizes the completed backup artifact metrics.
type BackupResult struct {
	BackupID          string         `json:"backupId"`
	S3Key             string         `json:"s3Key"`
	ETag              string         `json:"etag"`
	CompressedBytes   int64          `json:"compressedBytes"`
	UncompressedBytes int64          `json:"uncompressedBytes"`
	DurationMs        int64          `json:"durationMs"`
	CreatedAt         time.Time      `json:"createdAt"`
	Engine            DatabaseEngine `json:"engine"`
}

// RestoreJobConfig defines parameters for restoring a database from an S3 backup stream.
type RestoreJobConfig struct {
	RestoreID    string         `json:"restoreId"`
	ProjectSlug  string         `json:"projectSlug"`
	ServiceSlug  string         `json:"serviceSlug"`
	ContainerID  string         `json:"containerId"`
	Engine       DatabaseEngine `json:"engine"`
	DatabaseName string         `json:"databaseName"`
	Username     string         `json:"username"`
	Password     string         `json:"-"`
	S3Bucket     string         `json:"s3Bucket"`
	S3Key        string         `json:"s3Key"`
	Compression  string         `json:"compression"` // "gzip" or "zstd"
}

// BackupEngine orchestrates zero-disk streaming backup and restore operations.
type BackupEngine interface {
	// StreamBackup runs a native dump inside the container and streams compressed output to S3.
	StreamBackup(ctx context.Context, cfg BackupJobConfig) (*BackupResult, error)

	// StreamRestore downloads an S3 backup stream, decompresses it in memory, and pipes to container stdin.
	StreamRestore(ctx context.Context, cfg RestoreJobConfig) error

	// VerifyBackupEphemeral boots a scratch container, streams the restore, and runs an integrity assertion.
	VerifyBackupEphemeral(ctx context.Context, cfg RestoreJobConfig) (bool, error)
}
