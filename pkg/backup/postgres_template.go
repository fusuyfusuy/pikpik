package backup

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"
)

const (
	DefaultPostgresImage     = "postgres:17-alpine"
	DefaultPostgresUser      = "pguser"
	DefaultPostgresPort      = 5432
	DefaultPostgresDataDir   = "/var/lib/postgresql/data"
	DefaultPostgresPGDATA    = "/var/lib/postgresql/data/pgdata"
	DefaultPostgresInitDBArg = "--auth-host=scram-sha-256 --encoding=UTF8 --locale=C"
	passwordCharset          = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// PostgresTemplateConfig holds input parameters for generating a managed Postgres 17 service.
type PostgresTemplateConfig struct {
	ProjectSlug   string `json:"projectSlug"`
	ServiceSlug   string `json:"serviceSlug"`
	DatabaseName  string `json:"databaseName"`
	Username      string `json:"username"`
	Password      string `json:"password,omitempty"`
	MemoryLimitMB int    `json:"memoryLimitMb"`
}

// PostgresTemplate represents the fully materialized deterministic container spec for Postgres 17.
type PostgresTemplate struct {
	ServiceName    string            `json:"serviceName"`
	ContainerName  string            `json:"containerName"`
	OverlayNetwork string            `json:"overlayNetwork"`
	VolumeName     string            `json:"volumeName"`
	InternalDNS    string            `json:"internalDns"`
	InternalPort   int               `json:"internalPort"`
	MountPath      string            `json:"mountPath"`
	PGDATA         string            `json:"pgdata"`
	Image          string            `json:"image"`
	Environment    map[string]string `json:"environment"`
	Command        []string          `json:"command"`
	HealthcheckCmd []string          `json:"healthcheckCmd"`
	HealthInterval time.Duration     `json:"healthInterval"`
	HealthTimeout  time.Duration     `json:"healthTimeout"`
	HealthRetries  int               `json:"healthRetries"`
	HealthStart    time.Duration     `json:"healthStart"`
	ConnectionURL  string            `json:"connectionUrl"`
}

// GeneratePassword generates a cryptographically secure alphanumeric random string.
func GeneratePassword(length int) (string, error) {
	if length <= 0 {
		length = 32
	}
	res := make([]byte, length)
	charsetLen := big.NewInt(int64(len(passwordCharset)))
	for i := 0; i < length; i++ {
		idx, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			return "", fmt.Errorf("failed to generate random password byte: %w", err)
		}
		res[i] = passwordCharset[idx.Int64()]
	}
	return string(res), nil
}

// GeneratePostgresTemplate materializes a deterministic Postgres 17 managed template.
func GeneratePostgresTemplate(cfg PostgresTemplateConfig) (*PostgresTemplate, error) {
	projectSlug := strings.TrimSpace(cfg.ProjectSlug)
	if projectSlug == "" {
		return nil, fmt.Errorf("projectSlug is required")
	}
	serviceSlug := strings.TrimSpace(cfg.ServiceSlug)
	if serviceSlug == "" {
		serviceSlug = "postgres"
	}
	dbName := strings.TrimSpace(cfg.DatabaseName)
	if dbName == "" {
		dbName = serviceSlug
	}
	user := strings.TrimSpace(cfg.Username)
	if user == "" {
		user = DefaultPostgresUser
	}

	pass := cfg.Password
	if pass == "" {
		var err error
		pass, err = GeneratePassword(32)
		if err != nil {
			return nil, err
		}
	}

	serviceName := fmt.Sprintf("pikpik_svc_%s_%s", projectSlug, serviceSlug)
	containerName := fmt.Sprintf("pikpik_cnt_%s_%s", projectSlug, serviceSlug)
	networkName := fmt.Sprintf("pikpik_net_proj_%s", projectSlug)
	volumeName := fmt.Sprintf("pikpik_vol_%s_%s_pgdata", projectSlug, serviceSlug)
	internalDNS := "postgres"

	env := map[string]string{
		"POSTGRES_USER":        user,
		"POSTGRES_PASSWORD":    pass,
		"POSTGRES_DB":          dbName,
		"PGDATA":               DefaultPostgresPGDATA,
		"POSTGRES_INITDB_ARGS": DefaultPostgresInitDBArg,
	}

	// Performance-tuned parameters for 2GB+ memory allocations
	tunedCmd := []string{
		"postgres",
		"-c", "shared_buffers=256MB",
		"-c", "work_mem=16MB",
		"-c", "maintenance_work_mem=64MB",
		"-c", "effective_cache_size=768MB",
		"-c", "max_connections=100",
		"-c", "checkpoint_completion_target=0.9",
		"-c", "wal_buffers=16MB",
		"-c", "default_statistics_target=100",
	}

	healthCmd := []string{
		"CMD-SHELL",
		"pg_isready -U $$POSTGRES_USER -d $$POSTGRES_DB -h 127.0.0.1 -p 5432",
	}

	connURL := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		user, pass, internalDNS, DefaultPostgresPort, dbName)

	return &PostgresTemplate{
		ServiceName:    serviceName,
		ContainerName:  containerName,
		OverlayNetwork: networkName,
		VolumeName:     volumeName,
		InternalDNS:    internalDNS,
		InternalPort:   DefaultPostgresPort,
		MountPath:      DefaultPostgresDataDir,
		PGDATA:         DefaultPostgresPGDATA,
		Image:          DefaultPostgresImage,
		Environment:    env,
		Command:        tunedCmd,
		HealthcheckCmd: healthCmd,
		HealthInterval: 10 * time.Second,
		HealthTimeout:  5 * time.Second,
		HealthRetries:  5,
		HealthStart:    15 * time.Second,
		ConnectionURL:  connURL,
	}, nil
}
