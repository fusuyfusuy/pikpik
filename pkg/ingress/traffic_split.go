package ingress

import (
	"context"
	"fmt"
	"strings"
)

// TrafficSplitConfig defines declarative parameters for 1:1 upstream domain routing.
type TrafficSplitConfig struct {
	Domain         string   `json:"domain"`
	StableUpstream string   `json:"stable_upstream"`
	Paths          []string `json:"paths,omitempty"`
}

// GenerateTrafficSplitRouteID produces a deterministic route ID for traffic routing.
func GenerateTrafficSplitRouteID(domain string) string {
	slug := Slugify(domain)
	if slug == "" {
		slug = "default"
	}
	return fmt.Sprintf("route_split_%s", slug)
}

// BuildTrafficSplitRoute compiles a TrafficSplitConfig into a canonical CaddyRoute with direct 1:1 upstream routing.
func BuildTrafficSplitRoute(cfg TrafficSplitConfig) CaddyRoute {
	routeID := GenerateTrafficSplitRouteID(cfg.Domain)

	var matchers []CaddyMatch
	if cfg.Domain != "" || len(cfg.Paths) > 0 {
		var hosts []string
		if cfg.Domain != "" {
			hosts = []string{cfg.Domain}
		}
		matchers = []CaddyMatch{
			{
				Host: hosts,
				Path: cfg.Paths,
			},
		}
	}

	// 1. Response Headers Handler (HSTS + Security Headers)
	headerSet := make(map[string][]string)
	headerSet["X-Content-Type-Options"] = []string{"nosniff"}
	headerSet["X-Frame-Options"] = []string{"SAMEORIGIN"}
	headerSet["Referrer-Policy"] = []string{"strict-origin-when-cross-origin"}
	headerSet["Strict-Transport-Security"] = []string{"max-age=31536000; includeSubDomains; preload"}

	innerHandlers := []CaddyRouteHandler{
		{
			Handler: "headers",
			Response: &CaddyHeadersResponse{
				Set: headerSet,
			},
		},
		{
			Handler: "reverse_proxy",
			Upstreams: []CaddyUpstream{
				{Dial: cfg.StableUpstream},
			},
			Transport: &CaddyTransport{
				Protocol: "http",
				KeepAlive: &CaddyKeepAlive{
					MaxIdleConns:    100,
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
		},
	}

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

// SetTrafficSplit updates Caddy reverse proxy direct 1:1 upstream routing via PUT /id/{route_id} (<15ms).
func (c *HTTPCaddyClient) SetTrafficSplit(ctx context.Context, domain string, cfg TrafficSplitConfig) error {
	if domain != "" && cfg.Domain == "" {
		cfg.Domain = domain
	}
	cfg.Domain = strings.TrimSpace(cfg.Domain)
	if cfg.Domain == "" {
		return fmt.Errorf("%w: domain cannot be empty", ErrInvalidRoutePayload)
	}
	cfg.StableUpstream = strings.TrimSpace(cfg.StableUpstream)
	if cfg.StableUpstream == "" {
		return fmt.Errorf("%w: stable upstream cannot be empty", ErrInvalidRoutePayload)
	}

	route := BuildTrafficSplitRoute(cfg)
	if err := c.PutRoute(ctx, route.ID, route); err != nil {
		return err
	}

	c.mu.Lock()
	c.splits[cfg.Domain] = cfg
	c.mu.Unlock()

	return nil
}

// GetTrafficSplit returns the active routing configuration for the specified domain.
func (c *HTTPCaddyClient) GetTrafficSplit(ctx context.Context, domain string) (*TrafficSplitConfig, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return nil, fmt.Errorf("%w: domain cannot be empty", ErrInvalidRoutePayload)
	}

	c.mu.RLock()
	cfg, ok := c.splits[domain]
	c.mu.RUnlock()
	if ok {
		return &cfg, nil
	}

	routeID := GenerateTrafficSplitRouteID(domain)
	caddyRoute, err := c.GetRoute(ctx, routeID)
	if err != nil {
		return nil, err
	}

	cfg = TrafficSplitConfig{
		Domain: domain,
	}
	if len(caddyRoute.Handle) > 0 && len(caddyRoute.Handle[0].Routes) > 0 {
		for _, h := range caddyRoute.Handle[0].Routes[0].Handle {
			if h.Handler == "reverse_proxy" && len(h.Upstreams) > 0 {
				cfg.StableUpstream = h.Upstreams[0].Dial
			}
		}
	}

	return &cfg, nil
}

// RemoveTrafficSplit removes active direct routing for a domain and deletes its split route.
func (c *HTTPCaddyClient) RemoveTrafficSplit(ctx context.Context, domain string) error {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return fmt.Errorf("%w: domain cannot be empty", ErrInvalidRoutePayload)
	}

	c.mu.Lock()
	delete(c.splits, domain)
	c.mu.Unlock()

	routeID := GenerateTrafficSplitRouteID(domain)
	return c.DeleteRoute(ctx, routeID)
}

// SetTrafficSplit delegates routing to the underlying Caddy client and preserves active split.
func (m *DefaultIngressManager) SetTrafficSplit(ctx context.Context, domain string, cfg TrafficSplitConfig) error {
	if domain != "" && cfg.Domain == "" {
		cfg.Domain = domain
	}
	if err := m.client.SetTrafficSplit(ctx, domain, cfg); err != nil {
		return err
	}
	m.mu.Lock()
	if m.splits == nil {
		m.splits = make(map[string]TrafficSplitConfig)
	}
	m.splits[cfg.Domain] = cfg
	m.mu.Unlock()
	return nil
}

// GetTrafficSplit delegates traffic routing retrieval to the underlying Caddy client.
func (m *DefaultIngressManager) GetTrafficSplit(ctx context.Context, domain string) (*TrafficSplitConfig, error) {
	return m.client.GetTrafficSplit(ctx, domain)
}

// RemoveTrafficSplit removes active routing for a domain and cleans up local state.
func (m *DefaultIngressManager) RemoveTrafficSplit(ctx context.Context, domain string) error {
	domain = strings.TrimSpace(domain)
	m.mu.Lock()
	if m.splits != nil {
		delete(m.splits, domain)
	}
	m.mu.Unlock()
	return m.client.RemoveTrafficSplit(ctx, domain)
}
