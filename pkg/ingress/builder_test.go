package ingress_test

import (
	"encoding/json"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/ingress"
)

func TestSlugifyAndRouteID(t *testing.T) {
	cases := []struct {
		svcID    string
		host     string
		expected string
	}{
		{"svc_123", "app.example.com", "route_svc_123_app_example_com"},
		{"svc_9f82c1", "api.pikpik.dev", "route_svc_9f82c1_api_pikpik_dev"},
		{"", "app.example.com", "route_svc_app_example_com"},
		{"control_plane", "", "route_control_plane"},
		{"svc-prod-web", "my-site.internal.domain", "route_svc_prod_web_my_site_internal_domain"},
	}

	for _, c := range cases {
		got := ingress.GenerateRouteID(c.svcID, c.host)
		if got != c.expected {
			t.Errorf("GenerateRouteID(%q, %q) = %q; want %q", c.svcID, c.host, got, c.expected)
		}
	}
}

func TestBuildCaddyRoute_Standard(t *testing.T) {
	spec := ingress.RouteSpec{
		ID:           "route_svc_web_example_com",
		ServiceID:    "svc_web",
		Hosts:        []string{"example.com", "www.example.com"},
		UpstreamDial: "web_container:80",
		EnableHSTS:   true,
	}

	route := ingress.BuildCaddyRoute(spec)
	if route.ID != spec.ID {
		t.Fatalf("expected ID %s, got %s", spec.ID, route.ID)
	}
	if !route.Terminal {
		t.Errorf("expected route.Terminal to be true")
	}
	if len(route.Match) != 1 || len(route.Match[0].Host) != 2 {
		t.Fatalf("expected 2 matched hosts, got %v", route.Match)
	}

	data, err := json.Marshal(route)
	if err != nil {
		t.Fatalf("failed to marshal route: %v", err)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if raw["@id"] != spec.ID {
		t.Errorf("expected @id %s in JSON, got %v", spec.ID, raw["@id"])
	}
}

func TestBuildCaddyRoute_WebSocket(t *testing.T) {
	spec := ingress.RouteSpec{
		ServiceID:    "svc_terminal",
		Hosts:        []string{"terminal.example.com"},
		UpstreamDial: "term_backend:9000",
		IsWebSocket:  true,
	}

	route := ingress.BuildCaddyRoute(spec)
	if len(route.Handle) == 0 {
		t.Fatalf("expected handle handlers")
	}
	subroute := route.Handle[0]
	if subroute.Handler != "subroute" || len(subroute.Routes) == 0 {
		t.Fatalf("expected subroute structure")
	}

	innerHandlers := subroute.Routes[0].Handle
	var foundRP bool
	for _, h := range innerHandlers {
		if h.Handler == "reverse_proxy" {
			foundRP = true
			if h.FlushInterval != "-1" {
				t.Errorf("expected FlushInterval -1 for websocket, got %s", h.FlushInterval)
			}
			if h.Transport.ReadTimeout != "0s" || h.Transport.WriteTimeout != "0s" {
				t.Errorf("expected 0s read/write timeouts for websocket")
			}
		}
	}
	if !foundRP {
		t.Errorf("reverse_proxy handler not found")
	}
}

func TestBuildCaddyRoute_CustomHeadersAndStripPrefix(t *testing.T) {
	spec := ingress.RouteSpec{
		ServiceID:       "svc_api",
		Hosts:           []string{"api.example.com"},
		PathPrefixes:    []string{"/v1/*"},
		StripPathPrefix: "/v1",
		UpstreamDial:    "api_backend:8080",
		CustomHeaders: map[string]string{
			"X-Custom-Auth": "pikpik-edge",
			"Server":        "pikpik-gateway",
		},
		EnableHSTS:      true,
		HealthCheckPath: "/healthz",
		ActiveProbeSec:  5,
		MaxIdleConns:    200,
	}

	route := ingress.BuildCaddyRoute(spec)
	subroute := route.Handle[0]
	inner := subroute.Routes[0].Handle

	var hasRewrite, hasHeaders, hasRP bool
	for _, h := range inner {
		switch h.Handler {
		case "rewrite":
			hasRewrite = true
			if h.StripPathPrefix != "/v1" {
				t.Errorf("expected strip_path_prefix /v1, got %s", h.StripPathPrefix)
			}
		case "headers":
			hasHeaders = true
			if h.Response == nil || h.Response.Set["X-Custom-Auth"][0] != "pikpik-edge" {
				t.Errorf("custom headers not properly set: %v", h.Response)
			}
			if h.Response.Set["Strict-Transport-Security"][0] != "max-age=31536000; includeSubDomains; preload" {
				t.Errorf("HSTS header missing or incorrect")
			}
		case "reverse_proxy":
			hasRP = true
			if h.HealthChecks == nil || h.HealthChecks.Active == nil || h.HealthChecks.Active.Interval != "5s" {
				t.Errorf("expected 5s probe interval, got %v", h.HealthChecks)
			}
			if h.Transport == nil || h.Transport.KeepAlive.MaxIdleConns != 200 {
				t.Errorf("expected 200 max idle conns, got %v", h.Transport)
			}
		}
	}

	if !hasRewrite || !hasHeaders || !hasRP {
		t.Errorf("missing handler stages: rewrite=%v, headers=%v, rp=%v", hasRewrite, hasHeaders, hasRP)
	}

	// Test Round-Trip conversion
	extracted, err := ingress.CaddyRouteToSpec(route)
	if err != nil {
		t.Fatalf("CaddyRouteToSpec failed: %v", err)
	}
	if extracted.StripPathPrefix != spec.StripPathPrefix {
		t.Errorf("extracted StripPathPrefix mismatch: %s vs %s", extracted.StripPathPrefix, spec.StripPathPrefix)
	}
	if extracted.UpstreamDial != spec.UpstreamDial {
		t.Errorf("extracted UpstreamDial mismatch: %s vs %s", extracted.UpstreamDial, spec.UpstreamDial)
	}
	if !extracted.EnableHSTS {
		t.Errorf("extracted EnableHSTS should be true")
	}
	if extracted.CustomHeaders["X-Custom-Auth"] != "pikpik-edge" {
		t.Errorf("extracted custom header mismatch")
	}
	if extracted.HealthCheckPath != "/healthz" || extracted.ActiveProbeSec != 5 {
		t.Errorf("extracted health check mismatch: %s, %d", extracted.HealthCheckPath, extracted.ActiveProbeSec)
	}
	if extracted.MaxIdleConns != 200 {
		t.Errorf("extracted MaxIdleConns mismatch: %d", extracted.MaxIdleConns)
	}
}

func TestBuildCaddyConfig_TLSVariations(t *testing.T) {
	routes := []ingress.RouteSpec{
		{
			ID:           "route_app",
			Hosts:        []string{"app.example.com"},
			UpstreamDial: "app:3000",
		},
	}

	tlsCfg := ingress.GlobalTLSConfig{
		AdminEmail:         "admin@example.com",
		CloudflareAPIToken: "test_cf_token_123",
		CloudflareOriginCert: &ingress.CustomCertPair{
			CertPath: "/etc/caddy/certs/cloudflare/origin.pem",
			KeyPath:  "/etc/caddy/certs/cloudflare/origin.key",
			Tags:     []string{"cf_wildcard_origin"},
		},
		WildcardDomains:     []string{"*.example.com", "example.com"},
		OnDemandAskEndpoint: "http://127.0.0.1:8080/api/internal/ingress/ask",
		CustomCertificates: []ingress.CustomCertPair{
			{
				CertPath: "/etc/ssl/custom.crt",
				KeyPath:  "/etc/ssl/custom.key",
				Tags:     []string{"custom_cert"},
			},
		},
	}

	config := ingress.BuildCaddyConfig(routes, tlsCfg)

	// Check Server config
	srv, ok := config.Apps.HTTP.Servers["srv0"]
	if !ok {
		t.Fatalf("srv0 server not found in HTTP apps")
	}
	if len(srv.Listen) != 2 || srv.Listen[0] != ":80" || srv.Listen[1] != ":443" {
		t.Errorf("unexpected listeners: %v", srv.Listen)
	}
	if len(srv.Routes) != 1 {
		t.Errorf("expected 1 route, got %d", len(srv.Routes))
	}

	// Check TLS certificates
	certs := config.Apps.TLS.Certificates.LoadFiles
	if len(certs) != 2 {
		t.Fatalf("expected 2 loaded certificates, got %d", len(certs))
	}
	if certs[0].Certificate != "/etc/caddy/certs/cloudflare/origin.pem" {
		t.Errorf("unexpected origin cert path: %s", certs[0].Certificate)
	}

	// Check TLS automation policies
	policies := config.Apps.TLS.Automation.Policies
	if len(policies) != 2 {
		t.Fatalf("expected 2 automation policies (wildcard + on-demand), got %d", len(policies))
	}

	// Policy 1: Wildcard DNS-01
	wildcardPolicy := policies[0]
	if len(wildcardPolicy.Subjects) != 2 || wildcardPolicy.Issuers[0].Challenges.DNS.Provider["api_token"] != "test_cf_token_123" {
		t.Errorf("unexpected wildcard policy: %+v", wildcardPolicy)
	}

	// Policy 2: On-Demand Dual Fallback (Let's Encrypt + ZeroSSL)
	onDemandPolicy := policies[1]
	if !onDemandPolicy.OnDemand || len(onDemandPolicy.Issuers) != 2 {
		t.Fatalf("unexpected on-demand policy: %+v", onDemandPolicy)
	}
	if onDemandPolicy.Issuers[0].Module != "acme" || onDemandPolicy.Issuers[0].Directory != "https://acme-v02.api.letsencrypt.org/directory" {
		t.Errorf("expected Let's Encrypt directory in primary issuer")
	}
	if onDemandPolicy.Issuers[1].Module != "zerossl" {
		t.Errorf("expected ZeroSSL as fallback issuer")
	}

	// Check On-Demand ask rule
	onDemandRule := config.Apps.TLS.Automation.OnDemand
	if onDemandRule == nil || onDemandRule.Ask != "http://127.0.0.1:8080/api/internal/ingress/ask" {
		t.Errorf("unexpected on-demand ask rule: %+v", onDemandRule)
	}
}
