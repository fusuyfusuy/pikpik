package registry

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// RegistryYAMLConfig represents the structured config.yml for Docker Distribution Registry:2.
type RegistryYAMLConfig struct {
	Version string                 `yaml:"version"`
	Log     RegistryYAMLLog        `yaml:"log"`
	Storage RegistryYAMLStorage    `yaml:"storage"`
	HTTP    RegistryYAMLHTTP       `yaml:"http"`
	Auth    RegistryYAMLAuth       `yaml:"auth"`
}

type RegistryYAMLLog struct {
	Level  string                 `yaml:"level"`
	Fields map[string]interface{} `yaml:"fields"`
}

type RegistryYAMLStorage struct {
	Cache      map[string]string         `yaml:"cache"`
	Delete     map[string]bool           `yaml:"delete"`
	Filesystem *RegistryYAMLFilesystem   `yaml:"filesystem,omitempty"`
	S3         *RegistryYAMLS3           `yaml:"s3,omitempty"`
}

type RegistryYAMLFilesystem struct {
	RootDirectory string `yaml:"rootdirectory"`
}

type RegistryYAMLS3 struct {
	AccessKey      string `yaml:"accesskey"`
	SecretKey      string `yaml:"secretkey"`
	Region         string `yaml:"region"`
	RegionEndpoint string `yaml:"regionendpoint,omitempty"`
	Bucket         string `yaml:"bucket"`
	Encrypt        bool   `yaml:"encrypt"`
	Secure         bool   `yaml:"secure"`
	V4Auth         bool   `yaml:"v4auth"`
	ChunkSize      int64  `yaml:"chunksize"`
}

type RegistryYAMLHTTP struct {
	Addr    string              `yaml:"addr"`
	Headers map[string][]string `yaml:"headers"`
}

type RegistryYAMLAuth struct {
	Htpasswd RegistryYAMLHtpasswd `yaml:"htpasswd"`
}

type RegistryYAMLHtpasswd struct {
	Realm string `yaml:"realm"`
	Path  string `yaml:"path"`
}

// ValidateConfig verifies that the RegistryConfig contains valid parameters.
func ValidateConfig(cfg RegistryConfig) error {
	if cfg.StorageBackend == "" {
		cfg.StorageBackend = StorageBackendLocal
	}

	switch cfg.StorageBackend {
	case StorageBackendLocal:
		// Local storage is valid
	case StorageBackendS3:
		if cfg.S3Config == nil {
			return fmt.Errorf("%w: s3Config is required for s3 storage backend", ErrInvalidRegistryConfig)
		}
		if strings.TrimSpace(cfg.S3Config.AccessKey) == "" ||
			strings.TrimSpace(cfg.S3Config.SecretKey) == "" ||
			strings.TrimSpace(cfg.S3Config.Bucket) == "" {
			return fmt.Errorf("%w: s3 accessKey, secretKey, and bucket are mandatory", ErrInvalidRegistryConfig)
		}
	default:
		return fmt.Errorf("%w: unsupported storage backend %q", ErrInvalidRegistryConfig, cfg.StorageBackend)
	}

	return nil
}

// GenerateConfigYAML produces the raw YAML string for registry:2 container injection.
func GenerateConfigYAML(cfg RegistryConfig) (string, error) {
	if err := ValidateConfig(cfg); err != nil {
		return "", err
	}

	htpasswdPath := cfg.HtpasswdPath
	if htpasswdPath == "" {
		htpasswdPath = "/etc/docker/registry/htpasswd"
	}

	port := cfg.InternalPort
	if port <= 0 {
		port = 5000
	}

	regYAML := RegistryYAMLConfig{
		Version: "0.1",
		Log: RegistryYAMLLog{
			Level: "info",
			Fields: map[string]interface{}{
				"service": "registry",
			},
		},
		Storage: RegistryYAMLStorage{
			Cache: map[string]string{
				"blobdescriptor": "inmemory",
			},
			Delete: map[string]bool{
				"enabled": true,
			},
		},
		HTTP: RegistryYAMLHTTP{
			Addr: fmt.Sprintf(":%d", port),
			Headers: map[string][]string{
				"X-Content-Type-Options": {"nosniff"},
			},
		},
		Auth: RegistryYAMLAuth{
			Htpasswd: RegistryYAMLHtpasswd{
				Realm: "pikpik-registry",
				Path:  htpasswdPath,
			},
		},
	}

	if cfg.StorageBackend == StorageBackendS3 && cfg.S3Config != nil {
		region := cfg.S3Config.Region
		if region == "" {
			region = "us-east-1"
		}
		regYAML.Storage.S3 = &RegistryYAMLS3{
			AccessKey:      cfg.S3Config.AccessKey,
			SecretKey:      cfg.S3Config.SecretKey,
			Region:         region,
			RegionEndpoint: cfg.S3Config.Endpoint,
			Bucket:         cfg.S3Config.Bucket,
			Encrypt:        false,
			Secure:         cfg.S3Config.Secure,
			V4Auth:         cfg.S3Config.V4Auth,
			ChunkSize:      5242880, // 5MB parts
		}
	} else {
		regYAML.Storage.Filesystem = &RegistryYAMLFilesystem{
			RootDirectory: "/var/lib/registry",
		}
	}

	data, err := yaml.Marshal(regYAML)
	if err != nil {
		return "", fmt.Errorf("failed to marshal registry yaml: %w", err)
	}

	return string(data), nil
}
