package ingress

import (
	"context"
	"fmt"
	"strings"
)

// WeightedUpstream represents a single backend upstream dial address and its load balancing weight.
type WeightedUpstream struct {
	Dial   string `json:"dial"`
	Weight int    `json:"weight"`
}

// TrafficSplitConfig defines declarative parameters for blue-green and canary traffic splitting across a domain.
type TrafficSplitConfig struct {
	Domain         string            `json:"domain"`
	StableUpstream string            `json:"stable_upstream"`
	CanaryUpstream string            `json:"canary_upstream"`
	CanaryPercent  int               `json:"canary_percent"` // 0-100
	Headers        map[string]string `json:"headers,omitempty"`
	Paths          []string          `json:"paths,omitempty"`
}

// GenerateTrafficSplitRouteID produces a deterministic route ID for traffic splitting.
func GenerateTrafficSplitRouteID(domain string) string {
	slug := Slugify(domain)
	if slug == "" {
		slug = "default"
	}
	return fmt.Sprintf("route_split_%s", slug)
}

// BuildTrafficSplitRoute compiles a TrafficSplitConfig into a canonical CaddyRoute with weighted upstream load balancing.
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

	// 1. Response Headers Handler (HSTS + Security Headers + Custom Headers)
	headerSet := make(map[string][]string)
	headerSet["X-Content-Type-Options"] = []string{"nosniff"}
	headerSet["X-Frame-Options"] = []string{"SAMEORIGIN"}
	headerSet["Referrer-Policy"] = []string{"strict-origin-when-cross-origin"}
	headerSet["Strict-Transport-Security"] = []string{"max-age=31536000; includeSubDomains; preload"}

	for k, v := range cfg.Headers {
		headerSet[k] = []string{v}
	}

	innerHandlers := []CaddyRouteHandler{
		{
			Handler: "headers",
			Response: &CaddyHeadersResponse{
				Set: headerSet,
			},
		},
	}

	// 2. Compute Upstreams and Load Balancing Policy
	var upstreams []CaddyUpstream
	var policy *CaddySelectionPolicy

	if cfg.CanaryPercent <= 0 || cfg.CanaryUpstream == "" {
		upstreams = []CaddyUpstream{
			{Dial: cfg.StableUpstream, Weight: 100},
		}
		policy = &CaddySelectionPolicy{
			Policy: "round_robin",
		}
	} else if cfg.CanaryPercent >= 100 {
		upstreams = []CaddyUpstream{
			{Dial: cfg.CanaryUpstream, Weight: 100},
		}
		policy = &CaddySelectionPolicy{
			Policy: "round_robin",
		}
	} else {
		stableWeight := 100 - cfg.CanaryPercent
		canaryWeight := cfg.CanaryPercent
		upstreams = []CaddyUpstream{
			{Dial: cfg.StableUpstream, Weight: stableWeight},
			{Dial: cfg.CanaryUpstream, Weight: canaryWeight},
		}
		policy = &CaddySelectionPolicy{
			Policy: "weighted_round_robin",
			Weights: map[string]int{
				cfg.StableUpstream: stableWeight,
				cfg.CanaryUpstream: canaryWeight,
			},
		}
	}

	rpHandler := CaddyRouteHandler{
		Handler:   "reverse_proxy",
		Upstreams: upstreams,
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
			SelectionPolicy: policy,
			Retries:         3,
			TryDuration:     "5s",
		},
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

// SetTrafficSplit updates Caddy reverse proxy upstream load balancing weights via PUT /id/{route_id} (<15ms).
func (c *HTTPCaddyClient) SetTrafficSplit(ctx context.Context, domain string, cfg TrafficSplitConfig) error {
	if domain != "" && cfg.Domain == "" {
		cfg.Domain = domain
	}
	cfg.Domain = strings.TrimSpace(cfg.Domain)
	if cfg.Domain == "" {
		return fmt.Errorf("%w: domain cannot be empty", ErrInvalidRoutePayload)
	}
	if cfg.CanaryPercent < 0 || cfg.CanaryPercent > 100 {
		return fmt.Errorf("%w: canary percent must be between 0 and 100, got %d", ErrInvalidRoutePayload, cfg.CanaryPercent)
	}
	if cfg.StableUpstream == "" && (cfg.CanaryPercent < 100 || cfg.CanaryUpstream == "") {
		return fmt.Errorf("%w: stable upstream cannot be empty", ErrInvalidRoutePayload)
	}
	if cfg.CanaryPercent > 0 && cfg.CanaryUpstream == "" {
		return fmt.Errorf("%w: canary upstream cannot be empty when canary percent > 0", ErrInvalidRoutePayload)
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

// SetCanaryWeight dynamically adjusts the traffic percentage assigned to the canary upstream.
func (c *HTTPCaddyClient) SetCanaryWeight(ctx context.Context, domain string, canaryPercent int) error {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return fmt.Errorf("%w: domain cannot be empty", ErrInvalidRoutePayload)
	}
	if canaryPercent < 0 || canaryPercent > 100 {
		return fmt.Errorf("%w: canary percent must be between 0 and 100, got %d", ErrInvalidRoutePayload, canaryPercent)
	}

	c.mu.RLock()
	cfg, exists := c.splits[domain]
	c.mu.RUnlock()

	if !exists {
		// Attempt to query from Caddy route ID
		routeID := GenerateTrafficSplitRouteID(domain)
		caddyRoute, err := c.GetRoute(ctx, routeID)
		if err != nil {
			return fmt.Errorf("%w: no active traffic split found for domain %q", ErrRouteNotFound, domain)
		}
		// Extract upstreams from CaddyRoute
		cfg = TrafficSplitConfig{
			Domain:        domain,
			CanaryPercent: canaryPercent,
		}
		if len(caddyRoute.Handle) > 0 && len(caddyRoute.Handle[0].Routes) > 0 {
			for _, h := range caddyRoute.Handle[0].Routes[0].Handle {
				if h.Handler == "reverse_proxy" {
					if len(h.Upstreams) > 0 {
						cfg.StableUpstream = h.Upstreams[0].Dial
					}
					if len(h.Upstreams) > 1 {
						cfg.CanaryUpstream = h.Upstreams[1].Dial
					}
				}
			}
		}
		if cfg.StableUpstream == "" {
			return fmt.Errorf("%w: unable to extract upstreams from caddy route for domain %q", ErrRouteNotFound, domain)
		}
	}

	cfg.CanaryPercent = canaryPercent
	return c.SetTrafficSplit(ctx, domain, cfg)
}

// GetTrafficSplit returns the active traffic split configuration for the specified domain.
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
			if h.Handler == "reverse_proxy" {
				if len(h.Upstreams) > 0 {
					cfg.StableUpstream = h.Upstreams[0].Dial
				}
				if len(h.Upstreams) > 1 {
					cfg.CanaryUpstream = h.Upstreams[1].Dial
					cfg.CanaryPercent = h.Upstreams[1].Weight
				}
			}
		}
	}

	return &cfg, nil
}

// RemoveTrafficSplit removes active traffic splitting for a domain and deletes its split route.
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

// SetTrafficSplit delegates traffic splitting to the underlying Caddy client and preserves active split.
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

// SetCanaryWeight delegates dynamic canary weight shifting to the underlying Caddy client and updates active split.
func (m *DefaultIngressManager) SetCanaryWeight(ctx context.Context, domain string, canaryPercent int) error {
	if err := m.client.SetCanaryWeight(ctx, domain, canaryPercent); err != nil {
		return err
	}
	m.mu.Lock()
	if m.splits != nil {
		if cfg, ok := m.splits[domain]; ok {
			cfg.CanaryPercent = canaryPercent
			m.splits[domain] = cfg
		}
	}
	m.mu.Unlock()
	return nil
}

// GetTrafficSplit delegates traffic split retrieval to the underlying Caddy client.
func (m *DefaultIngressManager) GetTrafficSplit(ctx context.Context, domain string) (*TrafficSplitConfig, error) {
	return m.client.GetTrafficSplit(ctx, domain)
}

// RemoveTrafficSplit removes active traffic splitting for a domain and cleans up local state.
func (m *DefaultIngressManager) RemoveTrafficSplit(ctx context.Context, domain string) error {
	domain = strings.TrimSpace(domain)
	m.mu.Lock()
	if m.splits != nil {
		delete(m.splits, domain)
	}
	m.mu.Unlock()
	return m.client.RemoveTrafficSplit(ctx, domain)
}
