package s3

import (
	"context"
	"io"
	"time"
)

// ProviderType identifies the remote S3 storage service.
type ProviderType string

const (
	ProviderAWS       ProviderType = "aws"
	ProviderR2        ProviderType = "cloudflare_r2"
	ProviderMinIO     ProviderType = "minio"
	ProviderBackblaze ProviderType = "backblaze_b2"
)

// ClientOptions configures the Universal S3 client.
type ClientOptions struct {
	Provider        ProviderType `json:"provider"`
	Endpoint        string       `json:"endpoint"`        // custom endpoint URL for R2/MinIO/B2
	Region          string       `json:"region"`          // e.g. "us-east-1" or "auto"
	Bucket          string       `json:"bucket"`
	AccessKeyID     string       `json:"accessKeyId"`
	SecretAccessKey string       `json:"secretAccessKey"`
	ForcePathStyle  bool         `json:"forcePathStyle"`  // true for MinIO/B2
	UseSSL          bool         `json:"useSsl"`          // default true
	PartSizeBytes   int64        `json:"partSizeBytes"`   // default 5MB (5 * 1024 * 1024)
	MaxConcurrency  int          `json:"maxConcurrency"`  // default 4 workers
}

// UploadOptions provides metadata tags for multipart stream uploads.
type UploadOptions struct {
	ContentType string            `json:"contentType"`
	Metadata    map[string]string `json:"metadata"`
}

// ObjectInfo holds summary data for an object in S3.
type ObjectInfo struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	ETag         string    `json:"etag"`
	LastModified time.Time `json:"lastModified"`
}

// RetentionPolicy defines the retention counts for Grandfather-Father-Son lifecycle pruning.
type RetentionPolicy struct {
	KeepHourly  int `json:"keepHourly"`  // e.g. 24
	KeepDaily   int `json:"keepDaily"`   // e.g. 7
	KeepWeekly  int `json:"keepWeekly"`  // e.g. 4
	KeepMonthly int `json:"keepMonthly"` // e.g. 12
	MaxBackups  int `json:"maxBackups"`  // hard upper bound safeguard
}

// S3Client provides pure streaming multipart uploads, downloads, and lifecycle pruning.
type S3Client interface {
	// UploadStreamMultipart streams data from reader directly to S3 via concurrent multipart uploads.
	UploadStreamMultipart(ctx context.Context, key string, reader io.Reader, opts UploadOptions) (*ObjectInfo, error)

	// DownloadStream returns an io.ReadCloser streaming data directly from S3 without saving to disk.
	DownloadStream(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error)

	// ListObjects returns objects matching a given prefix.
	ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error)

	// DeleteObjects performs batch deletion of specified keys.
	DeleteObjects(ctx context.Context, keys []string) error

	// PruneRetention evaluates existing objects under a prefix and deletes those exceeding retention policy.
	PruneRetention(ctx context.Context, prefix string, policy RetentionPolicy) ([]string, error)
}
