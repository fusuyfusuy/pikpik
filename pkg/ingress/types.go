package ingress

import (
	"context"
	"errors"
)

// Common Error Types
var (
	ErrCaddyUnreachable     = errors.New("caddy admin api unreachable")
	ErrRouteNotFound        = errors.New("route with specified id not found")
	ErrInvalidRoutePayload  = errors.New("invalid route specification payload")
	ErrDomainNotWhitelisted = errors.New("domain is not whitelisted for on-demand tls")
	ErrCaddyMutationFailed  = errors.New("caddy mutation returned non-2xx status")
)

// TLSMode defines how certificates are provisioned for a given domain.
type TLSMode string

const (
	TLSModeCloudflareOrigin TLSMode = "cloudflare_origin"
	TLSModeACMEHTTP01       TLSMode = "acme_http01"
	TLSModeACMEDNS01        TLSMode = "acme_dns01"
	TLSModeOnDemand         TLSMode = "on_demand"
	TLSModeCustomCert       TLSMode = "custom_cert"
	TLSModeDisabled         TLSMode = "disabled"
)

// RouteSpec defines the high-level pikpik route intent.
type RouteSpec struct {
	ID              string            `json:"id"`                 // e.g. "route_svc_123_app_domain_com"
	ServiceID       string            `json:"service_id"`         // e.g. "svc_123"
	Hosts           []string          `json:"hosts"`              // e.g. ["app.example.com"]
	PathPrefixes    []string          `json:"path_prefixes"`      // Optional path routing e.g. ["/api"]
	UpstreamDial    string            `json:"upstream_dial"`      // e.g. "paas_svc_123:3000" or "172.20.0.5:8080"
	CustomHeaders   map[string]string `json:"custom_headers"`     // Custom response headers
	EnableHSTS      bool              `json:"enable_hsts"`        // Default true
	IsWebSocket     bool              `json:"is_websocket"`       // Configures zero timeouts
	HealthCheckPath string            `json:"health_check_path"`  // e.g. "/healthz"
	ActiveProbeSec  int               `json:"active_probe_sec"`   // Active probe interval
	MaxIdleConns    int               `json:"max_idle_conns"`     // Default 100
	StripPathPrefix string            `json:"strip_path_prefix"`  // Optional prefix stripping
	TLSMode         TLSMode           `json:"tls_mode,omitempty"` // Certificate issuance mode
}

// GlobalTLSConfig defines global certificate loaders and automation policies.
type GlobalTLSConfig struct {
	AdminEmail           string           `json:"admin_email"`
	CloudflareAPIToken   string           `json:"cloudflare_api_token,omitempty"`
	CloudflareOriginCert *CustomCertPair  `json:"cloudflare_origin_cert,omitempty"`
	WildcardDomains      []string         `json:"wildcard_domains,omitempty"`
	OnDemandAskEndpoint  string           `json:"on_demand_ask_endpoint"`
	CustomCertificates   []CustomCertPair `json:"custom_certificates,omitempty"`
}

// CustomCertPair represents a certificate/key file pair on disk.
type CustomCertPair struct {
	CertPath string   `json:"cert_path"`
	KeyPath  string   `json:"key_path"`
	Tags     []string `json:"tags,omitempty"`
}

// DomainValidator defines an interface to verify if a domain is allowed for On-Demand TLS.
type DomainValidator interface {
	VerifyDomain(ctx context.Context, domain string) (bool, error)
}

// IngressManager is the primary orchestrator boundary for all ingress operations.
type IngressManager interface {
	// ApplyRoute atomically adds or replaces a route in Caddy (<15ms).
	ApplyRoute(ctx context.Context, spec RouteSpec) error

	// RemoveRoute atomically deletes a route by its ID.
	RemoveRoute(ctx context.Context, routeID string) error

	// GetRoute retrieves the active route specification by ID.
	GetRoute(ctx context.Context, routeID string) (*RouteSpec, error)

	// ListRoutes retrieves all active routes currently loaded in Caddy.
	ListRoutes(ctx context.Context) ([]RouteSpec, error)

	// ReconcileAll rebuilds the full Caddy routing and TLS state.
	ReconcileAll(ctx context.Context, routes []RouteSpec, tlsCfg GlobalTLSConfig) error

	// VerifyDomain checks if a domain is allowed to obtain an On-Demand certificate.
	VerifyDomain(ctx context.Context, domain string) (bool, error)

	// HealthCheck returns nil if Caddy Admin API is responding within SLA (<5ms).
	HealthCheck(ctx context.Context) error
}

// CaddyClient defines the raw REST client talking to http://127.0.0.1:2019.
type CaddyClient interface {
	PutRoute(ctx context.Context, routeID string, route CaddyRoute) error
	DeleteRoute(ctx context.Context, routeID string) error
	GetRoute(ctx context.Context, routeID string) (*CaddyRoute, error)
	ListRoutes(ctx context.Context) ([]CaddyRoute, error)
	LoadFullConfig(ctx context.Context, config CaddyConfig) error
	Ping(ctx context.Context) error
}
