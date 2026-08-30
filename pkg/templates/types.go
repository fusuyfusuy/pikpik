package templates

import (
	"time"

	"github.com/fusuycorp/pikpik/pkg/orchestration"
)

// Template represents a curated marketplace application ready for 1-click deployment.
type Template struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Category         string                 `json:"category"`
	Description      string                 `json:"description"`
	Icon             string                 `json:"icon"`
	Version          string                 `json:"version"`
	DocumentationURL string                 `json:"documentation_url,omitempty"`
	Tags             []string               `json:"tags,omitempty"`
	DefaultPort      int                    `json:"default_port"`
	Services         []TemplateService      `json:"services"`
	EnvVars          []TemplateEnvVar       `json:"env_vars,omitempty"`
	Volumes          []TemplateVolume       `json:"volumes,omitempty"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
}

// TemplateService defines an individual container service in a template stack.
type TemplateService struct {
	Name        string                             `json:"name"`
	Image       string                             `json:"image"`
	Command     []string                           `json:"command,omitempty"`
	Entrypoint  []string                           `json:"entrypoint,omitempty"`
	Ports       []orchestration.PortMappingSpec    `json:"ports,omitempty"`
	Environment map[string]string                  `json:"environment,omitempty"`
	Mounts      []TemplateVolume                   `json:"mounts,omitempty"`
	DependsOn   []string                           `json:"depends_on,omitempty"`
	Resources   orchestration.ResourceRequirements `json:"resources,omitempty"`
	HealthCheck *orchestration.HealthCheckConfig   `json:"health_check,omitempty"`
	Restart     string                             `json:"restart,omitempty"`
	Labels      map[string]string                  `json:"labels,omitempty"`
}

// TemplateEnvVar specifies an environment variable configuration and auto-generation schema.
type TemplateEnvVar struct {
	Key          string `json:"key"`
	Label        string `json:"label"`
	Description  string `json:"description"`
	Default      string `json:"default,omitempty"`
	Required     bool   `json:"required"`
	IsSecret     bool   `json:"is_secret"`
	AutoGenerate string `json:"auto_generate,omitempty"` // "hex_32", "pass_16", "base64_32", or ""
}

// TemplateVolume defines a storage volume binding for a template.
type TemplateVolume struct {
	Name      string `json:"name"`
	MountPath string `json:"mount_path"`
	HostPath  string `json:"host_path,omitempty"`
	ReadOnly  bool   `json:"read_only"`
}

// DeployTemplateRequest carries user parameters for instantiating a template.
type DeployTemplateRequest struct {
	Name                string            `json:"name,omitempty"`
	ProjectID           string            `json:"project_id,omitempty"`
	StageID             string            `json:"stage_id,omitempty"`
	Variables           map[string]string `json:"variables,omitempty"`
	Domain              string            `json:"domain,omitempty"`
	AutoGenerateMissing bool              `json:"auto_generate_missing,omitempty"`
}

// DeployTemplateResponse contains the deployed application metadata and runtime status.
type DeployTemplateResponse struct {
	AppID             string            `json:"app_id"`
	Name              string            `json:"name"`
	TemplateID        string            `json:"template_id"`
	Category          string            `json:"category"`
	Status            string            `json:"status"`
	Services          []string          `json:"services"`
	Containers        []string          `json:"containers,omitempty"`
	Volumes           []string          `json:"volumes,omitempty"`
	Network           string            `json:"network"`
	Endpoints         []string          `json:"endpoints,omitempty"`
	ResolvedVariables map[string]string `json:"resolved_variables,omitempty"`
	DeployedAt        time.Time         `json:"deployed_at"`
	Message           string            `json:"message,omitempty"`
}
