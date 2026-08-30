package templates

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fusuycorp/pikpik/pkg/orchestration"
)

var (
	// ErrTemplateNotFound is returned when a requested template ID is not present in the catalog.
	ErrTemplateNotFound = errors.New("template not found")
)

const (
	CategoryProductivityDevTools = "Productivity & DevTools"
	CategoryAnalyticsCMS         = "Analytics & CMS"
	CategoryDatabases            = "Databases"
)

// Catalog manages the embedded in-memory repository of curated marketplace templates.
type Catalog struct {
	mu        sync.RWMutex
	templates map[string]Template
	order     []string
}

// defaultCatalog is the singleton embedded catalog.
var (
	defaultCatalogInstance *Catalog
	defaultCatalogOnce     sync.Once
)

// DefaultCatalog returns the initialized singleton catalog containing 20+ production templates.
func DefaultCatalog() *Catalog {
	defaultCatalogOnce.Do(func() {
		defaultCatalogInstance = newCuratedCatalog()
	})
	return defaultCatalogInstance
}

// ListTemplates retrieves templates optionally filtered by category.
func (c *Catalog) ListTemplates(category string) []Template {
	c.mu.RLock()
	defer c.mu.RUnlock()

	normalizedCat := normalizeCategory(category)
	var list []Template
	for _, id := range c.order {
		tpl := c.templates[id]
		if normalizedCat == "" || matchesCategory(tpl.Category, normalizedCat) {
			list = append(list, tpl)
		}
	}
	return list
}

// GetTemplate finds and returns a template by ID.
func (c *Catalog) GetTemplate(id string) (*Template, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	tpl, ok := c.templates[strings.ToLower(strings.TrimSpace(id))]
	if !ok {
		return nil, fmt.Errorf("%w: '%s'", ErrTemplateNotFound, id)
	}
	return &tpl, nil
}

// SearchTemplates filters templates by category and search keyword.
func (c *Catalog) SearchTemplates(category string, query string) []Template {
	c.mu.RLock()
	defer c.mu.RUnlock()

	normalizedCat := normalizeCategory(category)
	q := strings.ToLower(strings.TrimSpace(query))

	var list []Template
	for _, id := range c.order {
		tpl := c.templates[id]
		if normalizedCat != "" && !matchesCategory(tpl.Category, normalizedCat) {
			continue
		}
		if q == "" || matchesSearch(tpl, q) {
			list = append(list, tpl)
		}
	}
	return list
}

// Global package-level convenience functions using DefaultCatalog

// ListTemplates returns curated templates filtered by category.
func ListTemplates(category string) []Template {
	return DefaultCatalog().ListTemplates(category)
}

// GetTemplate fetches a template by ID.
func GetTemplate(id string) (*Template, error) {
	return DefaultCatalog().GetTemplate(id)
}

// SearchTemplates searches templates by category and text query.
func SearchTemplates(category, query string) []Template {
	return DefaultCatalog().SearchTemplates(category, query)
}

func normalizeCategory(cat string) string {
	c := strings.ToLower(strings.TrimSpace(cat))
	if c == "" || c == "all" || c == "*" {
		return ""
	}
	switch c {
	case "productivity", "devtools", "dev", "tool", "tools", "productivity & devtools":
		return CategoryProductivityDevTools
	case "analytics", "cms", "content", "blog", "analytics & cms":
		return CategoryAnalyticsCMS
	case "database", "databases", "db", "storage":
		return CategoryDatabases
	default:
		return cat
	}
}

func matchesCategory(tplCategory, targetCategory string) bool {
	if strings.EqualFold(tplCategory, targetCategory) {
		return true
	}
	return strings.Contains(strings.ToLower(tplCategory), strings.ToLower(targetCategory))
}

func matchesSearch(tpl Template, q string) bool {
	if strings.Contains(strings.ToLower(tpl.ID), q) ||
		strings.Contains(strings.ToLower(tpl.Name), q) ||
		strings.Contains(strings.ToLower(tpl.Description), q) ||
		strings.Contains(strings.ToLower(tpl.Category), q) {
		return true
	}
	for _, tag := range tpl.Tags {
		if strings.Contains(strings.ToLower(tag), q) {
			return true
		}
	}
	for _, svc := range tpl.Services {
		if strings.Contains(strings.ToLower(svc.Image), q) || strings.Contains(strings.ToLower(svc.Name), q) {
			return true
		}
	}
	return false
}

func newCuratedCatalog() *Catalog {
	now := time.Now().UTC()
	cat := &Catalog{
		templates: make(map[string]Template),
		order:     make([]string, 0, 25),
	}

	rawList := []Template{
		// ==========================================
		// 1. Productivity & DevTools (11 templates)
		// ==========================================
		{
			ID:               "pocketbase",
			Name:             "PocketBase",
			Category:         CategoryProductivityDevTools,
			Description:      "Open Source backend in 1 file with embedded SQLite, realtime subscriptions, and built-in admin UI.",
			Icon:             "pocketbase",
			Version:          "0.22",
			DocumentationURL: "https://pocketbase.io/docs",
			Tags:             []string{"backend", "baas", "sqlite", "realtime", "auth"},
			DefaultPort:      8090,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "pb_data", MountPath: "/pb/pb_data"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:          "POCKETBASE_ENCRYPTION_KEY",
					Label:        "Encryption Key",
					Description:  "Internal cryptographic key for data field encryption",
					IsSecret:     true,
					AutoGenerate: "hex_32",
				},
			},
			Services: []TemplateService{
				{
					Name:  "pocketbase",
					Image: "ghcr.io/muchobien/pocketbase:0.22.20",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 8090, HostPort: 8090, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "pb_data", MountPath: "/pb/pb_data"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "n8n",
			Name:             "n8n",
			Category:         CategoryProductivityDevTools,
			Description:      "Fair-code workflow automation platform with multi-service integrations and visual pipeline builder.",
			Icon:             "n8n",
			Version:          "1.45",
			DocumentationURL: "https://docs.n8n.io",
			Tags:             []string{"automation", "workflow", "integration", "nocode"},
			DefaultPort:      5678,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "n8n_data", MountPath: "/home/node/.n8n"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:          "N8N_ENCRYPTION_KEY",
					Label:        "Encryption Key",
					Description:  "256-bit encryption key to secure stored webhook and account credentials",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "hex_32",
				},
				{
					Key:         "WEBHOOK_URL",
					Label:       "Webhook URL",
					Description: "Public webhook endpoint root URL for triggering external workflows",
					Default:     "http://localhost:5678/",
					Required:    false,
				},
				{
					Key:         "GENERIC_TIMEZONE",
					Label:       "Timezone",
					Description: "Timezone for scheduled cron workflows",
					Default:     "UTC",
				},
				{
					Key:         "N8N_PORT",
					Label:       "Port",
					Description: "Internal container port",
					Default:     "5678",
				},
			},
			Services: []TemplateService{
				{
					Name:  "n8n",
					Image: "docker.n8n.io/n8nio/n8n:1.59.0",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 5678, HostPort: 5678, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "n8n_data", MountPath: "/home/node/.n8n"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "vaultwarden",
			Name:             "Vaultwarden",
			Category:         CategoryProductivityDevTools,
			Description:      "Lightweight Bitwarden compatible password manager server written in Rust with minimal memory footprint.",
			Icon:             "vaultwarden",
			Version:          "1.30",
			DocumentationURL: "https://github.com/dani-garcia/vaultwarden/wiki",
			Tags:             []string{"security", "password-manager", "rust", "bitwarden"},
			DefaultPort:      80,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "vaultwarden_data", MountPath: "/data"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:          "ADMIN_TOKEN",
					Label:        "Admin Token",
					Description:  "Secret token to access the /admin configuration panel",
					IsSecret:     true,
					AutoGenerate: "hex_32",
				},
				{
					Key:         "SIGNUPS_ALLOWED",
					Label:       "Allow Signups",
					Description: "Permit new users to register accounts",
					Default:     "true",
				},
				{
					Key:         "WEBSOCKET_ENABLED",
					Label:       "Enable WebSocket",
					Description: "Enable live vault sync notifications via WebSocket",
					Default:     "true",
				},
			},
			Services: []TemplateService{
				{
					Name:  "vaultwarden",
					Image: "vaultwarden/server:1.32.0",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 80, HostPort: 80, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "vaultwarden_data", MountPath: "/data"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "meilisearch",
			Name:             "Meilisearch",
			Category:         CategoryProductivityDevTools,
			Description:      "Lightning-fast, ultra-relevant search engine for every developer with instant typo-tolerance.",
			Icon:             "meilisearch",
			Version:          "1.6",
			DocumentationURL: "https://www.meilisearch.com/docs",
			Tags:             []string{"search", "rust", "indexer", "typo-tolerance"},
			DefaultPort:      7700,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "meili_data", MountPath: "/meili_data"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:          "MEILI_MASTER_KEY",
					Label:        "Master Key",
					Description:  "Master API authentication key to protect Meilisearch instance",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "hex_32",
				},
				{
					Key:         "MEILI_ENV",
					Label:       "Environment",
					Description: "Deployment mode",
					Default:     "production",
				},
				{
					Key:         "MEILI_NO_ANALYTICS",
					Label:       "Disable Analytics",
					Description: "Deactivate Meilisearch telemetry reporting",
					Default:     "true",
				},
			},
			Services: []TemplateService{
				{
					Name:  "meilisearch",
					Image: "getmeili/meilisearch:v1.6",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 7700, HostPort: 7700, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "meili_data", MountPath: "/meili_data"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "directus",
			Name:             "Directus",
			Category:         CategoryProductivityDevTools,
			Description:      "Instant real-time REST and GraphQL API layer and visual data studio for SQL databases.",
			Icon:             "directus",
			Version:          "10.10",
			DocumentationURL: "https://docs.directus.io",
			Tags:             []string{"cms", "headless", "rest", "graphql", "sql"},
			DefaultPort:      8055,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "directus_uploads", MountPath: "/directus/uploads"},
				{Name: "directus_extensions", MountPath: "/directus/extensions"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:          "KEY",
					Label:        "Project Key",
					Description:  "Unique system identifier for telemetry and session cookies",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "hex_32",
				},
				{
					Key:          "SECRET",
					Label:        "Project Secret",
					Description:  "Cryptographic salt used for signing access and refresh tokens",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "hex_32",
				},
				{
					Key:         "ADMIN_EMAIL",
					Label:       "Admin Email",
					Description: "Default administrative account email",
					Default:     "admin@example.com",
					Required:    true,
				},
				{
					Key:          "ADMIN_PASSWORD",
					Label:        "Admin Password",
					Description:  "Default administrative account password",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
				{
					Key:         "DB_CLIENT",
					Label:       "Database Client",
					Description: "Database driver type",
					Default:     "sqlite3",
				},
				{
					Key:         "DB_FILENAME",
					Label:       "Database File",
					Description: "SQLite storage file path",
					Default:     "/directus/database/data.db",
				},
			},
			Services: []TemplateService{
				{
					Name:  "directus",
					Image: "directus/directus:11.1.0",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 8055, HostPort: 8055, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "directus_uploads", MountPath: "/directus/uploads"},
						{Name: "directus_extensions", MountPath: "/directus/extensions"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "supabase-studio",
			Name:             "Supabase Studio",
			Category:         CategoryProductivityDevTools,
			Description:      "Official visual dashboard and schema management console for Postgres and Supabase stacks.",
			Icon:             "supabase",
			Version:          "2.0",
			DocumentationURL: "https://supabase.com/docs",
			Tags:             []string{"database", "postgres", "gui", "studio", "developer-tools"},
			DefaultPort:      3000,
			CreatedAt:        now,
			UpdatedAt:        now,
			EnvVars: []TemplateEnvVar{
				{
					Key:         "STUDIO_PG_META_URL",
					Label:       "PG Meta URL",
					Description: "Endpoint URL for postgres-meta REST API service",
					Default:     "http://localhost:8080",
				},
				{
					Key:          "POSTGRES_PASSWORD",
					Label:        "Database Password",
					Description:  "Postgres superuser database password",
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
				{
					Key:         "SUPABASE_PUBLIC_URL",
					Label:       "Public URL",
					Description: "Publicly accessible endpoint for Supabase Gateway",
					Default:     "http://localhost:8000",
				},
			},
			Services: []TemplateService{
				{
					Name:  "supabase-studio",
					Image: "supabase/studio:20240729-219198b",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 3000, HostPort: 3000, Protocol: "tcp"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "minio",
			Name:             "MinIO",
			Category:         CategoryProductivityDevTools,
			Description:      "High-performance S3 compatible object storage suite with integrated web console.",
			Icon:             "minio",
			Version:          "RELEASE.2024",
			DocumentationURL: "https://min.io/docs/minio/linux/index.html",
			Tags:             []string{"s3", "storage", "object-store", "backup", "blobs"},
			DefaultPort:      9000,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "minio_data", MountPath: "/data"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:         "MINIO_ROOT_USER",
					Label:       "Root Access Key",
					Description: "Administrative root access key",
					Default:     "minioadmin",
					Required:    true,
				},
				{
					Key:          "MINIO_ROOT_PASSWORD",
					Label:        "Root Secret Key",
					Description:  "Administrative root secret key",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
			},
			Services: []TemplateService{
				{
					Name:  "minio",
					Image: "minio/minio:RELEASE.2024-08-29T01-40-52Z",
					Command: []string{
						"server",
						"/data",
						"--console-address",
						":9001",
					},
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 9000, HostPort: 9000, Protocol: "tcp"},
						{ContainerPort: 9001, HostPort: 9001, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "minio_data", MountPath: "/data"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "grafana",
			Name:             "Grafana",
			Category:         CategoryProductivityDevTools,
			Description:      "Operational dashboards, metrics visualization, alerting, and telemetry explorer platform.",
			Icon:             "grafana",
			Version:          "10.4",
			DocumentationURL: "https://grafana.com/docs/grafana/latest/",
			Tags:             []string{"observability", "metrics", "dashboard", "alerting"},
			DefaultPort:      3000,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "grafana_data", MountPath: "/var/lib/grafana"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:         "GF_SECURITY_ADMIN_USER",
					Label:       "Admin Username",
					Description: "Master administrative user name",
					Default:     "admin",
					Required:    true,
				},
				{
					Key:          "GF_SECURITY_ADMIN_PASSWORD",
					Label:        "Admin Password",
					Description:  "Master administrative password",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
				{
					Key:         "GF_USERS_ALLOW_SIGN_UP",
					Label:       "Allow Signups",
					Description: "Allow public user registration without invite",
					Default:     "false",
				},
			},
			Services: []TemplateService{
				{
					Name:  "grafana",
					Image: "grafana/grafana:11.2.0",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 3000, HostPort: 3000, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "grafana_data", MountPath: "/var/lib/grafana"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "prometheus",
			Name:             "Prometheus",
			Category:         CategoryProductivityDevTools,
			Description:      "Leading open-source monitoring and time-series alerting toolkit with PromQL engine.",
			Icon:             "prometheus",
			Version:          "2.51",
			DocumentationURL: "https://prometheus.io/docs/introduction/overview/",
			Tags:             []string{"monitoring", "metrics", "timeseries", "alerting"},
			DefaultPort:      9090,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "prometheus_data", MountPath: "/prometheus"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:         "PROMETHEUS_STORAGE_RETENTION",
					Label:       "Data Retention",
					Description: "Metric storage retention duration (e.g. 15d, 30d)",
					Default:     "15d",
				},
			},
			Services: []TemplateService{
				{
					Name:  "prometheus",
					Image: "prom/prometheus:v2.54.1",
					Command: []string{
						"--config.file=/etc/prometheus/prometheus.yml",
						"--storage.tsdb.path=/prometheus",
						"--web.enable-lifecycle",
					},
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 9090, HostPort: 9090, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "prometheus_data", MountPath: "/prometheus"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "metabase",
			Name:             "Metabase",
			Category:         CategoryProductivityDevTools,
			Description:      "Self-service Business Intelligence, analytics, dashboards, and visual charts for teams.",
			Icon:             "metabase",
			Version:          "0.49",
			DocumentationURL: "https://www.metabase.com/docs/latest/",
			Tags:             []string{"bi", "analytics", "sql", "dashboard", "charts"},
			DefaultPort:      3000,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "metabase_data", MountPath: "/metabase-data"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:         "MB_DB_FILE",
					Label:       "Database Path",
					Description: "Embedded H2/SQLite application database path",
					Default:     "/metabase-data/metabase.db",
				},
				{
					Key:          "MB_ENCRYPTION_SECRET_KEY",
					Label:        "Encryption Key",
					Description:  "Cryptographic key for encrypting database connection strings",
					IsSecret:     true,
					AutoGenerate: "hex_32",
				},
			},
			Services: []TemplateService{
				{
					Name:  "metabase",
					Image: "metabase/metabase:v0.50.25",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 3000, HostPort: 3000, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "metabase_data", MountPath: "/metabase-data"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "rabbitmq",
			Name:             "RabbitMQ",
			Category:         CategoryProductivityDevTools,
			Description:      "Robust distributed message broker supporting AMQP protocol and built-in management UI.",
			Icon:             "rabbitmq",
			Version:          "3.13",
			DocumentationURL: "https://www.rabbitmq.com/docs",
			Tags:             []string{"queue", "amqp", "broker", "events", "messaging"},
			DefaultPort:      5672,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "rabbitmq_data", MountPath: "/var/lib/rabbitmq"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:         "RABBITMQ_DEFAULT_USER",
					Label:       "Default User",
					Description: "Administrative username for management web UI and AMQP connections",
					Default:     "admin",
					Required:    true,
				},
				{
					Key:          "RABBITMQ_DEFAULT_PASS",
					Label:        "Default Password",
					Description:  "Administrative password for management console",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
			},
			Services: []TemplateService{
				{
					Name:  "rabbitmq",
					Image: "rabbitmq:3.13-management",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 5672, HostPort: 5672, Protocol: "tcp"},
						{ContainerPort: 15672, HostPort: 15672, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "rabbitmq_data", MountPath: "/var/lib/rabbitmq"},
					},
					Restart: "unless-stopped",
				},
			},
		},

		// ==========================================
		// 2. Analytics & CMS (4 templates)
		// ==========================================
		{
			ID:               "plausible",
			Name:             "Plausible Analytics",
			Category:         CategoryAnalyticsCMS,
			Description:      "Simple, lightweight, open-source and privacy-friendly Google Analytics alternative.",
			Icon:             "plausible",
			Version:          "2.0",
			DocumentationURL: "https://plausible.io/docs",
			Tags:             []string{"analytics", "privacy", "gdpr", "traffic"},
			DefaultPort:      8000,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "plausible_data", MountPath: "/var/lib/plausible"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:          "SECRET_KEY_BASE",
					Label:        "Secret Key Base",
					Description:  "64-character base64 secret token for session signing",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "base64_32",
				},
				{
					Key:         "BASE_URL",
					Label:       "Base URL",
					Description: "Domain URL where Plausible is hosted",
					Default:     "http://localhost:8000",
					Required:    true,
				},
				{
					Key:         "ADMIN_USER_EMAIL",
					Label:       "Admin Email",
					Description: "Primary administrator email",
					Default:     "admin@example.com",
				},
				{
					Key:         "ADMIN_USER_NAME",
					Label:       "Admin Name",
					Description: "Display name for administrator account",
					Default:     "admin",
				},
				{
					Key:          "ADMIN_USER_PWD",
					Label:        "Admin Password",
					Description:  "Initial administrator password",
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
			},
			Services: []TemplateService{
				{
					Name:  "plausible",
					Image: "ghcr.io/plausible/community-edition:v2.1.1",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 8000, HostPort: 8000, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "plausible_data", MountPath: "/var/lib/plausible"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "umami",
			Name:             "Umami Analytics",
			Category:         CategoryAnalyticsCMS,
			Description:      "Privacy-focused open-source website analytics with sleek, modern UI dashboards.",
			Icon:             "umami",
			Version:          "2.11",
			DocumentationURL: "https://umami.is/docs",
			Tags:             []string{"analytics", "privacy", "nextjs", "metrics"},
			DefaultPort:      3000,
			CreatedAt:        now,
			UpdatedAt:        now,
			EnvVars: []TemplateEnvVar{
				{
					Key:          "APP_SECRET",
					Label:        "App Secret",
					Description:  "Cryptographic salt used for hashing tracking tokens and events",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "hex_32",
				},
				{
					Key:         "DATABASE_URL",
					Label:       "Database Connection URL",
					Description: "PostgreSQL database connection URI",
					Default:     "postgresql://umami:password@db:5432/umami",
					Required:    true,
				},
			},
			Services: []TemplateService{
				{
					Name:  "umami",
					Image: "ghcr.io/umami-software/umami:postgresql-v2.13.0",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 3000, HostPort: 3000, Protocol: "tcp"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "ghost",
			Name:             "Ghost",
			Category:         CategoryAnalyticsCMS,
			Description:      "Modern publishing platform and headless CMS built for independent creators and publishers.",
			Icon:             "ghost",
			Version:          "5.82",
			DocumentationURL: "https://ghost.org/docs/",
			Tags:             []string{"cms", "blog", "publishing", "newsletter"},
			DefaultPort:      2368,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "ghost_content", MountPath: "/var/lib/ghost/content"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:         "url",
					Label:       "Site URL",
					Description: "Publicly accessible site URL for canonical links and redirects",
					Default:     "http://localhost:2368",
					Required:    true,
				},
				{
					Key:         "NODE_ENV",
					Label:       "Node Environment",
					Description: "Execution environment mode",
					Default:     "production",
				},
				{
					Key:         "database__client",
					Label:       "DB Client",
					Description: "Database engine backend",
					Default:     "sqlite3",
				},
				{
					Key:         "database__connection__filename",
					Label:       "DB Path",
					Description: "SQLite database storage file location",
					Default:     "/var/lib/ghost/content/data/ghost.db",
				},
			},
			Services: []TemplateService{
				{
					Name:  "ghost",
					Image: "ghost:5-alpine",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 2368, HostPort: 2368, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "ghost_content", MountPath: "/var/lib/ghost/content"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "wordpress",
			Name:             "WordPress",
			Category:         CategoryAnalyticsCMS,
			Description:      "World-leading open-source content management system and website publishing software.",
			Icon:             "wordpress",
			Version:          "6.5",
			DocumentationURL: "https://wordpress.org/documentation/",
			Tags:             []string{"cms", "blog", "php", "website"},
			DefaultPort:      80,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "wp_data", MountPath: "/var/www/html"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:         "WORDPRESS_DB_HOST",
					Label:       "Database Host",
					Description: "MySQL/MariaDB connection host and port",
					Default:     "mysql:3306",
					Required:    true,
				},
				{
					Key:         "WORDPRESS_DB_USER",
					Label:       "Database User",
					Description: "Database user for WordPress",
					Default:     "wordpress",
					Required:    true,
				},
				{
					Key:          "WORDPRESS_DB_PASSWORD",
					Label:        "Database Password",
					Description:  "Database password for WordPress",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
				{
					Key:         "WORDPRESS_DB_NAME",
					Label:       "Database Name",
					Description: "Target database schema name",
					Default:     "wordpress",
					Required:    true,
				},
			},
			Services: []TemplateService{
				{
					Name:  "wordpress",
					Image: "wordpress:6.6.2-apache",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 80, HostPort: 80, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "wp_data", MountPath: "/var/www/html"},
					},
					Restart: "unless-stopped",
				},
			},
		},

		// ==========================================
		// 3. Databases (6 templates)
		// ==========================================
		{
			ID:               "postgres-16",
			Name:             "PostgreSQL 16",
			Category:         CategoryDatabases,
			Description:      "World's most advanced relational database with powerful JSON support, indexing, and ACID guarantees.",
			Icon:             "postgres",
			Version:          "16-alpine",
			DocumentationURL: "https://www.postgresql.org/docs/16/",
			Tags:             []string{"sql", "relational", "acid", "postgres", "database"},
			DefaultPort:      5432,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "postgres_data", MountPath: "/var/lib/postgresql/data"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:         "POSTGRES_USER",
					Label:       "Username",
					Description: "Root PostgreSQL user",
					Default:     "postgres",
					Required:    true,
				},
				{
					Key:          "POSTGRES_PASSWORD",
					Label:        "Password",
					Description:  "Root administrative user password",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
				{
					Key:         "POSTGRES_DB",
					Label:       "Default Database",
					Description: "Initial database created automatically",
					Default:     "postgres",
					Required:    true,
				},
				{
					Key:         "PGDATA",
					Label:       "Cluster Data Directory",
					Description: "PostgreSQL internal filesystem data path",
					Default:     "/var/lib/postgresql/data/pgdata",
				},
			},
			Services: []TemplateService{
				{
					Name:  "postgres",
					Image: "postgres:16-alpine",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 5432, HostPort: 5432, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "postgres_data", MountPath: "/var/lib/postgresql/data"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "mysql-8",
			Name:             "MySQL 8",
			Category:         CategoryDatabases,
			Description:      "Industry-standard open-source relational database management system with JSON support and replication.",
			Icon:             "mysql",
			Version:          "8.0",
			DocumentationURL: "https://dev.mysql.com/doc/refman/8.0/en/",
			Tags:             []string{"sql", "relational", "mysql", "database"},
			DefaultPort:      3306,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "mysql_data", MountPath: "/var/lib/mysql"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:          "MYSQL_ROOT_PASSWORD",
					Label:        "Root Password",
					Description:  "Administrative root user password",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
				{
					Key:         "MYSQL_DATABASE",
					Label:       "Database Name",
					Description: "Initial application database created on first boot",
					Default:     "pikpik",
				},
				{
					Key:         "MYSQL_USER",
					Label:       "Application User",
					Description: "Non-root user account",
					Default:     "pikpik",
				},
				{
					Key:          "MYSQL_PASSWORD",
					Label:        "Application Password",
					Description:  "Password for the application user",
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
			},
			Services: []TemplateService{
				{
					Name:  "mysql",
					Image: "mysql:8.0",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 3306, HostPort: 3306, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "mysql_data", MountPath: "/var/lib/mysql"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "redis-7",
			Name:             "Redis 7",
			Category:         CategoryDatabases,
			Description:      "High-speed in-memory data structure store used as a database, cache, message broker, and streaming engine.",
			Icon:             "redis",
			Version:          "7-alpine",
			DocumentationURL: "https://redis.io/docs/",
			Tags:             []string{"cache", "nosql", "in-memory", "key-value", "redis"},
			DefaultPort:      6379,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "redis_data", MountPath: "/data"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:          "REDIS_PASSWORD",
					Label:        "Auth Password",
					Description:  "Password required for Redis client AUTH command",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
			},
			Services: []TemplateService{
				{
					Name:  "redis",
					Image: "redis:7-alpine",
					Command: []string{
						"redis-server",
						"--appendonly",
						"yes",
						"--requirepass",
						"${REDIS_PASSWORD}",
					},
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 6379, HostPort: 6379, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "redis_data", MountPath: "/data"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "mongodb-7",
			Name:             "MongoDB 7",
			Category:         CategoryDatabases,
			Description:      "Leading document-oriented NoSQL database designed for developer agility and flexible JSON-like schemas.",
			Icon:             "mongodb",
			Version:          "7.0",
			DocumentationURL: "https://www.mongodb.com/docs/manual/",
			Tags:             []string{"nosql", "document", "json", "mongo", "database"},
			DefaultPort:      27017,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "mongo_data", MountPath: "/data/db"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:         "MONGO_INITDB_ROOT_USERNAME",
					Label:       "Root Username",
					Description: "Administrative root superuser",
					Default:     "admin",
					Required:    true,
				},
				{
					Key:          "MONGO_INITDB_ROOT_PASSWORD",
					Label:        "Root Password",
					Description:  "Administrative root superuser password",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
				{
					Key:         "MONGO_INITDB_DATABASE",
					Label:       "Initial Database",
					Description: "Default database created on boot",
					Default:     "pikpik",
				},
			},
			Services: []TemplateService{
				{
					Name:  "mongo",
					Image: "mongo:7.0",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 27017, HostPort: 27017, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "mongo_data", MountPath: "/data/db"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "clickhouse",
			Name:             "ClickHouse",
			Category:         CategoryDatabases,
			Description:      "Blazing-fast open-source column-oriented database management system for real-time analytical queries.",
			Icon:             "clickhouse",
			Version:          "24.3",
			DocumentationURL: "https://clickhouse.com/docs",
			Tags:             []string{"olap", "columnar", "analytics", "bigdata", "timeseries"},
			DefaultPort:      8123,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "clickhouse_data", MountPath: "/var/lib/clickhouse"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:         "CLICKHOUSE_DB",
					Label:       "Database Name",
					Description: "Default created database name",
					Default:     "default",
				},
				{
					Key:         "CLICKHOUSE_USER",
					Label:       "Username",
					Description: "Default user account name",
					Default:     "default",
				},
				{
					Key:          "CLICKHOUSE_PASSWORD",
					Label:        "User Password",
					Description:  "Password for default user account",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
				{
					Key:         "CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT",
					Label:       "Access Management",
					Description: "Enable SQL user and role management commands",
					Default:     "1",
				},
			},
			Services: []TemplateService{
				{
					Name:  "clickhouse",
					Image: "clickhouse/clickhouse-server:24.8.3",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 8123, HostPort: 8123, Protocol: "tcp"},
						{ContainerPort: 9000, HostPort: 9000, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "clickhouse_data", MountPath: "/var/lib/clickhouse"},
					},
					Restart: "unless-stopped",
				},
			},
		},
		{
			ID:               "mariadb",
			Name:             "MariaDB",
			Category:         CategoryDatabases,
			Description:      "High-performance community-developed fork of MySQL with modern replication and advanced engines.",
			Icon:             "mariadb",
			Version:          "11.3",
			DocumentationURL: "https://mariadb.org/documentation/",
			Tags:             []string{"sql", "relational", "mariadb", "mysql", "database"},
			DefaultPort:      3306,
			CreatedAt:        now,
			UpdatedAt:        now,
			Volumes: []TemplateVolume{
				{Name: "mariadb_data", MountPath: "/var/lib/mysql"},
			},
			EnvVars: []TemplateEnvVar{
				{
					Key:          "MARIADB_ROOT_PASSWORD",
					Label:        "Root Password",
					Description:  "Superuser root administrative password",
					Required:     true,
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
				{
					Key:         "MARIADB_DATABASE",
					Label:       "Database Name",
					Description: "Default database schema name",
					Default:     "pikpik",
				},
				{
					Key:         "MARIADB_USER",
					Label:       "Application User",
					Description: "Non-root user account",
					Default:     "pikpik",
				},
				{
					Key:          "MARIADB_PASSWORD",
					Label:        "Application Password",
					Description:  "Password for application user account",
					IsSecret:     true,
					AutoGenerate: "pass_16",
				},
			},
			Services: []TemplateService{
				{
					Name:  "mariadb",
					Image: "mariadb:11",
					Ports: []orchestration.PortMappingSpec{
						{ContainerPort: 3306, HostPort: 3306, Protocol: "tcp"},
					},
					Mounts: []TemplateVolume{
						{Name: "mariadb_data", MountPath: "/var/lib/mysql"},
					},
					Restart: "unless-stopped",
				},
			},
		},
	}

	for _, tpl := range rawList {
		cat.templates[tpl.ID] = tpl
		cat.order = append(cat.order, tpl.ID)
	}

	return cat
}
