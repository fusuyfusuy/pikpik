package ingress

import "encoding/json"

// CaddyConfig represents the root Caddy JSON configuration document.
type CaddyConfig struct {
	Admin   CaddyAdmin   `json:"admin"`
	Logging CaddyLogging `json:"logging"`
	Apps    CaddyApps    `json:"apps"`
}

// CaddyAdmin configures the Caddy Admin API.
type CaddyAdmin struct {
	Listen        string `json:"listen"`
	EnforceOrigin bool   `json:"enforce_origin"`
}

// CaddyLogging configures top-level logging in Caddy.
type CaddyLogging struct {
	Logs map[string]CaddyLogConfig `json:"logs"`
}

// CaddyLogConfig defines settings for a single named logger.
type CaddyLogConfig struct {
	Level   string          `json:"level"`
	Writer  CaddyLogWriter  `json:"writer"`
	Encoder CaddyLogEncoder `json:"encoder"`
}

// CaddyLogWriter configures the destination of log output.
type CaddyLogWriter struct {
	Output string `json:"output"`
}

// CaddyLogEncoder configures log encoding (e.g. json or console).
type CaddyLogEncoder struct {
	Format string `json:"format"`
}

// CaddyApps contains app-specific configurations (http, tls, etc.).
type CaddyApps struct {
	HTTP CaddyHTTPApp `json:"http"`
	TLS  CaddyTLSApp  `json:"tls"`
}

// CaddyHTTPApp defines the HTTP server module.
type CaddyHTTPApp struct {
	HTTPPort  int                        `json:"http_port,omitempty"`
	HTTPSPort int                        `json:"https_port,omitempty"`
	Servers   map[string]CaddyHTTPServer `json:"servers"`
}

// CaddyHTTPServer defines an HTTP/HTTPS server instance.
type CaddyHTTPServer struct {
	Listen         []string         `json:"listen"`
	Routes         []CaddyRoute     `json:"routes"`
	AutomaticHTTPS *CaddyAutoHTTPS  `json:"automatic_https,omitempty"`
	StrictSNIHost  bool             `json:"strict_sni_host"`
	Logs           *CaddyServerLogs `json:"logs,omitempty"`
}

// CaddyAutoHTTPS controls automatic certificate and redirect behaviors.
type CaddyAutoHTTPS struct {
	Disable          bool `json:"disable,omitempty"`
	DisableRedirects bool `json:"disable_redirects,omitempty"`
}

// CaddyServerLogs associates a logger with the HTTP server.
type CaddyServerLogs struct {
	DefaultLoggerName string `json:"default_logger_name,omitempty"`
}

// CaddyRoute represents a single routing rule with matchers and handlers.
type CaddyRoute struct {
	ID       string              `json:"@id,omitempty"`
	Match    []CaddyMatch        `json:"match,omitempty"`
	Handle   []CaddyRouteHandler `json:"handle"`
	Terminal bool                `json:"terminal"`
}

// CaddyMatch filters requests by host and/or path.
type CaddyMatch struct {
	Host []string `json:"host,omitempty"`
	Path []string `json:"path,omitempty"`
}

// CaddyRouteHandler defines a handler execution step within a route or subroute.
type CaddyRouteHandler struct {
	Handler         string                `json:"handler"` // "subroute", "headers", "reverse_proxy", "rewrite", "static_response"
	Routes          []CaddyRoute          `json:"routes,omitempty"`
	Response        *CaddyHeadersResponse `json:"response,omitempty"`
	Upstreams       []CaddyUpstream       `json:"upstreams,omitempty"`
	HealthChecks    *CaddyHealthChecks    `json:"health_checks,omitempty"`
	Transport       *CaddyTransport       `json:"transport,omitempty"`
	LoadBalancing   *CaddyLoadBalancing   `json:"load_balancing,omitempty"`
	FlushInterval   string                `json:"flush_interval,omitempty"`
	StripPathPrefix string                `json:"strip_path_prefix,omitempty"`
	StatusCode      int                   `json:"status_code,omitempty"`
	Body            string                `json:"body,omitempty"`
	RawConfig       json.RawMessage       `json:"-"`
}

// CaddyHeadersResponse defines response header manipulation rules.
type CaddyHeadersResponse struct {
	Set map[string][]string `json:"set,omitempty"`
}

// CaddyUpstream specifies a reverse proxy dial destination.
type CaddyUpstream struct {
	Dial   string `json:"dial"`
	Weight int    `json:"weight,omitempty"`
}

// CaddyHealthChecks configures active and passive health checks for reverse proxying.
type CaddyHealthChecks struct {
	Active  *CaddyActiveHealthCheck  `json:"active,omitempty"`
	Passive *CaddyPassiveHealthCheck `json:"passive,omitempty"`
}

// CaddyActiveHealthCheck defines periodic HTTP health probing.
type CaddyActiveHealthCheck struct {
	Path         string `json:"path,omitempty"`
	Interval     string `json:"interval,omitempty"`
	Timeout      string `json:"timeout,omitempty"`
	ExpectStatus int    `json:"expect_status,omitempty"`
}

// CaddyPassiveHealthCheck tracks upstream failure metrics during real requests.
type CaddyPassiveHealthCheck struct {
	MaxFails        int      `json:"max_fails,omitempty"`
	FailDuration    string   `json:"fail_duration,omitempty"`
	UnhealthyStatus []int    `json:"unhealthy_status,omitempty"`
}

// CaddyTransport configures HTTP transport parameters for proxy upstreams.
type CaddyTransport struct {
	Protocol     string          `json:"protocol,omitempty"`
	KeepAlive    *CaddyKeepAlive `json:"keep_alive,omitempty"`
	ReadTimeout  string          `json:"read_timeout,omitempty"`
	WriteTimeout string          `json:"write_timeout,omitempty"`
}

// CaddyKeepAlive configures connection reuse.
type CaddyKeepAlive struct {
	MaxIdleConns    int    `json:"max_idle_conns,omitempty"`
	IdleConnTimeout string `json:"idle_conn_timeout,omitempty"`
}

// CaddyLoadBalancing defines upstream selection algorithms and retry policies.
type CaddyLoadBalancing struct {
	SelectionPolicy *CaddySelectionPolicy `json:"selection_policy,omitempty"`
	Retries         int                   `json:"retries,omitempty"`
	TryDuration     string                `json:"try_duration,omitempty"`
}

// CaddySelectionPolicy specifies load balancing algorithm (e.g. round_robin, weighted_round_robin).
type CaddySelectionPolicy struct {
	Policy  string         `json:"policy"`
	Weights map[string]int `json:"weights,omitempty"`
}

// CaddyTLSApp defines TLS certificates and ACME automation configurations.
type CaddyTLSApp struct {
	Certificates CaddyTLSCertificates `json:"certificates"`
	Automation   CaddyTLSAutomation   `json:"automation"`
}

// CaddyTLSCertificates lists static file-loaded certificates.
type CaddyTLSCertificates struct {
	LoadFiles []CaddyTLSFileLoader `json:"load_files"`
}

// CaddyTLSFileLoader specifies certificate and private key file paths on disk.
type CaddyTLSFileLoader struct {
	Certificate string   `json:"certificate"`
	Key         string   `json:"key"`
	Tags        []string `json:"tags,omitempty"`
}

// CaddyTLSAutomation configures ACME issuance policies and On-Demand rules.
type CaddyTLSAutomation struct {
	Policies []CaddyTLSPolicy   `json:"policies"`
	OnDemand *CaddyOnDemandRule `json:"on_demand,omitempty"`
}

// CaddyTLSPolicy defines certificate issuance settings for matching subjects or on-demand requests.
type CaddyTLSPolicy struct {
	Subjects []string         `json:"subjects,omitempty"`
	Issuers  []CaddyTLSIssuer `json:"issuers,omitempty"`
	OnDemand bool             `json:"on_demand,omitempty"`
}

// CaddyTLSIssuer configures an ACME or ZeroSSL issuer.
type CaddyTLSIssuer struct {
	Module     string               `json:"module"` // "acme", "zerossl", "internal"
	Email      string               `json:"email,omitempty"`
	Directory  string               `json:"directory,omitempty"`
	Challenges *CaddyACMEChallenges `json:"challenges,omitempty"`
}

// CaddyACMEChallenges configures ACME challenge mechanisms (DNS-01, HTTP-01).
type CaddyACMEChallenges struct {
	DNS  *CaddyDNSChallenge  `json:"dns,omitempty"`
	HTTP *CaddyHTTPChallenge `json:"http,omitempty"`
}

// CaddyDNSChallenge configures DNS provider authentication for wildcard certificates.
type CaddyDNSChallenge struct {
	Provider map[string]interface{} `json:"provider"`
}

// CaddyHTTPChallenge configures or disables HTTP-01 challenges.
type CaddyHTTPChallenge struct {
	Disabled bool `json:"disabled,omitempty"`
}

// CaddyOnDemandRule configures On-Demand TLS rate limiting and security ask endpoint.
type CaddyOnDemandRule struct {
	Ask      string `json:"ask"`
	Interval string `json:"interval,omitempty"`
	Burst    int    `json:"burst,omitempty"`
}
