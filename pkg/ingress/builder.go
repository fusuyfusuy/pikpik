package ingress

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// Slugify converts an arbitrary string (such as a domain or service name) into a safe identifier slug.
func Slugify(s string) string {
	slug := nonAlphanumericRegex.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "_")
	return strings.Trim(slug, "_")
}

// GenerateRouteID creates a deterministic route identifier based on service ID and host.
func GenerateRouteID(serviceID, host string) string {
	svcSlug := Slugify(serviceID)
	if svcSlug == "" {
		svcSlug = "svc"
	}
	hostSlug := Slugify(host)
	if hostSlug == "" {
		return fmt.Sprintf("route_%s", svcSlug)
	}
	return fmt.Sprintf("route_%s_%s", svcSlug, hostSlug)
}

// BuildCaddyRoute converts a high-level pikpik RouteSpec into a canonical CaddyRoute JSON tree.
func BuildCaddyRoute(spec RouteSpec) CaddyRoute {
	routeID := spec.ID
	if routeID == "" {
		firstHost := ""
		if len(spec.Hosts) > 0 {
			firstHost = spec.Hosts[0]
		}
		routeID = GenerateRouteID(spec.ServiceID, firstHost)
	}

	var matchers []CaddyMatch
	if len(spec.Hosts) > 0 || len(spec.PathPrefixes) > 0 {
		matchers = []CaddyMatch{
			{
				Host: spec.Hosts,
				Path: spec.PathPrefixes,
			},
		}
	}

	// 1. Rewrite handler (if path strip requested)
	var innerHandlers []CaddyRouteHandler
	if spec.StripPathPrefix != "" {
		innerHandlers = append(innerHandlers, CaddyRouteHandler{
			Handler:         "rewrite",
			StripPathPrefix: spec.StripPathPrefix,
		})
	}

	// 2. Response Headers Handler (HSTS + Security Headers + Custom Headers)
	headerSet := make(map[string][]string)
	headerSet["X-Content-Type-Options"] = []string{"nosniff"}
	headerSet["X-Frame-Options"] = []string{"SAMEORIGIN"}
	headerSet["Referrer-Policy"] = []string{"strict-origin-when-cross-origin"}

	if spec.EnableHSTS {
		headerSet["Strict-Transport-Security"] = []string{"max-age=31536000; includeSubDomains; preload"}
	}

	for k, v := range spec.CustomHeaders {
		headerSet[k] = []string{v}
	}

	innerHandlers = append(innerHandlers, CaddyRouteHandler{
		Handler: "headers",
		Response: &CaddyHeadersResponse{
			Set: headerSet,
		},
	})

	// 3. Reverse Proxy Handler
	maxIdle := spec.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 100
	}

	rpHandler := CaddyRouteHandler{
		Handler: "reverse_proxy",
		Upstreams: []CaddyUpstream{
			{Dial: spec.UpstreamDial},
		},
		Transport: &CaddyTransport{
			Protocol: "http",
			KeepAlive: &CaddyKeepAlive{
				MaxIdleConns:    maxIdle,
				IdleConnTimeout: "90s",
			},
			ReadTimeout:  "0s",
			WriteTimeout: "0s",
		},
		LoadBalancing: &CaddyLoadBalancing{
			SelectionPolicy: &CaddySelectionPolicy{
				Policy: "round_robin",
			},
			Retries:     3,
			TryDuration: "5s",
		},
	}

	if spec.HealthCheckPath != "" {
		probeInterval := "10s"
		if spec.ActiveProbeSec > 0 {
			probeInterval = fmt.Sprintf("%ds", spec.ActiveProbeSec)
		}
		rpHandler.HealthChecks = &CaddyHealthChecks{
			Active: &CaddyActiveHealthCheck{
				Path:         spec.HealthCheckPath,
				Interval:     probeInterval,
				Timeout:      "2s",
				ExpectStatus: 200,
			},
			Passive: &CaddyPassiveHealthCheck{
				MaxFails:        3,
				FailDuration:    "30s",
				UnhealthyStatus: []int{502, 503, 504},
			},
		}
	}

	if spec.IsWebSocket {
		rpHandler.FlushInterval = "-1"
	}

	innerHandlers = append(innerHandlers, rpHandler)

	subroute := CaddyRouteHandler{
		Handler: "subroute",
		Routes: []CaddyRoute{
			{
				Handle: innerHandlers,
			},
		},
	}

	return CaddyRoute{
		ID:       routeID,
		Match:    matchers,
		Handle:   []CaddyRouteHandler{subroute},
		Terminal: true,
	}
}

// BuildCaddyConfig constructs the full server bootstrap and reconciliation configuration.
func BuildCaddyConfig(routes []RouteSpec, tlsCfg GlobalTLSConfig) CaddyConfig {
	caddyRoutes := make([]CaddyRoute, 0, len(routes))
	for _, spec := range routes {
		caddyRoutes = append(caddyRoutes, BuildCaddyRoute(spec))
	}

	loadFiles := make([]CaddyTLSFileLoader, 0)
	if tlsCfg.CloudflareOriginCert != nil {
		tags := tlsCfg.CloudflareOriginCert.Tags
		if len(tags) == 0 {
			tags = []string{"cf_wildcard_origin"}
		}
		loadFiles = append(loadFiles, CaddyTLSFileLoader{
			Certificate: tlsCfg.CloudflareOriginCert.CertPath,
			Key:         tlsCfg.CloudflareOriginCert.KeyPath,
			Tags:        tags,
		})
	}
	for _, cert := range tlsCfg.CustomCertificates {
		loadFiles = append(loadFiles, CaddyTLSFileLoader{
			Certificate: cert.CertPath,
			Key:         cert.KeyPath,
			Tags:        cert.Tags,
		})
	}

	policies := make([]CaddyTLSPolicy, 0)
	if len(tlsCfg.WildcardDomains) > 0 {
		var wildcardIssuers []CaddyTLSIssuer
		if tlsCfg.CloudflareAPIToken != "" {
			wildcardIssuers = []CaddyTLSIssuer{
				{
					Module: "acme",
					Email:  tlsCfg.AdminEmail,
					Challenges: &CaddyACMEChallenges{
						DNS: &CaddyDNSChallenge{
							Provider: map[string]interface{}{
								"name":      "cloudflare",
								"api_token": tlsCfg.CloudflareAPIToken,
							},
						},
					},
				},
			}
		} else {
			wildcardIssuers = []CaddyTLSIssuer{
				{
					Module: "acme",
					Email:  tlsCfg.AdminEmail,
				},
			}
		}
		policies = append(policies, CaddyTLSPolicy{
			Subjects: tlsCfg.WildcardDomains,
			Issuers:  wildcardIssuers,
		})
	}

	// Always append On-Demand dual ACME/ZeroSSL fallback policy
	onDemandIssuers := []CaddyTLSIssuer{
		{
			Module:    "acme",
			Email:     tlsCfg.AdminEmail,
			Directory: "https://acme-v02.api.letsencrypt.org/directory",
		},
		{
			Module: "zerossl",
			Email:  tlsCfg.AdminEmail,
		},
	}
	policies = append(policies, CaddyTLSPolicy{
		OnDemand: true,
		Issuers:  onDemandIssuers,
	})

	var onDemandRule *CaddyOnDemandRule
	if tlsCfg.OnDemandAskEndpoint != "" {
		onDemandRule = &CaddyOnDemandRule{
			Ask:      tlsCfg.OnDemandAskEndpoint,
			Interval: "2m",
			Burst:    5,
		}
	}

	return CaddyConfig{
		Admin: CaddyAdmin{
			Listen:        "127.0.0.1:2019",
			EnforceOrigin: false,
		},
		Logging: CaddyLogging{
			Logs: map[string]CaddyLogConfig{
				"default": {
					Level: "INFO",
					Writer: CaddyLogWriter{
						Output: "stdout",
					},
					Encoder: CaddyLogEncoder{
						Format: "json",
					},
				},
			},
		},
		Apps: CaddyApps{
			HTTP: CaddyHTTPApp{
				HTTPPort:  80,
				HTTPSPort: 443,
				Servers: map[string]CaddyHTTPServer{
					"srv0": {
						Listen: []string{":80", ":443"},
						Routes: caddyRoutes,
						AutomaticHTTPS: &CaddyAutoHTTPS{
							Disable:          false,
							DisableRedirects: false,
						},
						StrictSNIHost: false,
						Logs: &CaddyServerLogs{
							DefaultLoggerName: "default",
						},
					},
				},
			},
			TLS: CaddyTLSApp{
				Certificates: CaddyTLSCertificates{
					LoadFiles: loadFiles,
				},
				Automation: CaddyTLSAutomation{
					Policies: policies,
					OnDemand: onDemandRule,
				},
			},
		},
	}
}

// CaddyRouteToSpec extracts a RouteSpec from a compiled CaddyRoute.
func CaddyRouteToSpec(r CaddyRoute) (*RouteSpec, error) {
	spec := &RouteSpec{
		ID:            r.ID,
		CustomHeaders: make(map[string]string),
	}

	if len(r.Match) > 0 {
		spec.Hosts = r.Match[0].Host
		spec.PathPrefixes = r.Match[0].Path
	}

	for _, h := range r.Handle {
		if h.Handler == "subroute" {
			for _, subRoute := range h.Routes {
				for _, innerH := range subRoute.Handle {
					switch innerH.Handler {
					case "rewrite":
						spec.StripPathPrefix = innerH.StripPathPrefix
					case "headers":
						if innerH.Response != nil && innerH.Response.Set != nil {
							if hsts, ok := innerH.Response.Set["Strict-Transport-Security"]; ok && len(hsts) > 0 {
								spec.EnableHSTS = true
							}
							for k, vals := range innerH.Response.Set {
								if k == "Strict-Transport-Security" || k == "X-Content-Type-Options" ||
									k == "X-Frame-Options" || k == "Referrer-Policy" {
									continue
								}
								if len(vals) > 0 {
									spec.CustomHeaders[k] = vals[0]
								}
							}
						}
					case "reverse_proxy":
						if len(innerH.Upstreams) > 0 {
							spec.UpstreamDial = innerH.Upstreams[0].Dial
						}
						if innerH.HealthChecks != nil && innerH.HealthChecks.Active != nil {
							spec.HealthCheckPath = innerH.HealthChecks.Active.Path
							intervalStr := strings.TrimSuffix(innerH.HealthChecks.Active.Interval, "s")
							if sec, err := strconv.Atoi(intervalStr); err == nil {
								spec.ActiveProbeSec = sec
							}
						}
						if innerH.Transport != nil && innerH.Transport.KeepAlive != nil {
							spec.MaxIdleConns = innerH.Transport.KeepAlive.MaxIdleConns
						}
						if innerH.FlushInterval == "-1" {
							spec.IsWebSocket = true
						}
					}
				}
			}
		}
	}

	return spec, nil
}
