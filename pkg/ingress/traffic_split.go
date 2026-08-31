package ingress

import (
	"context"
	"fmt"
	"strings"
)

// UpstreamWeight defines a single upstream dial destination and its traffic weight.
type UpstreamWeight struct {
	Upstream string `json:"upstream"`
	Weight   int    `json:"weight"`
}

// TrafficSplitConfig defines declarative parameters for weighted upstream domain routing.
type TrafficSplitConfig struct {
	Domain         string           `json:"domain"`
	AppID          string           `json:"app_id,omitempty"`
	StableUpstream string           `json:"stable_upstream,omitempty"`
	Splits         []UpstreamWeight `json:"splits,omitempty"`
	Paths          []string         `json:"paths,omitempty"`
}

// GenerateTrafficSplitRouteID produces a deterministic route ID for traffic routing.
func GenerateTrafficSplitRouteID(domain string) string {
	slug := Slugify(domain)
	if slug == "" {
		slug = "default"
	}
	return fmt.Sprintf("route_split_%s", slug)
}

// BuildTrafficSplitRoute compiles a TrafficSplitConfig into a canonical CaddyRoute with weighted upstream routing.
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

	var upstreams []CaddyUpstream
	weightsMap := make(map[string]int)

	if len(cfg.Splits) > 0 {
		for _, s := range cfg.Splits {
			upstreams = append(upstreams, CaddyUpstream{
				Dial:   s.Upstream,
				Weight: s.Weight,
			})
			weightsMap[s.Upstream] = s.Weight
		}
	} else if cfg.StableUpstream != "" {
		upstreams = append(upstreams, CaddyUpstream{
			Dial: cfg.StableUpstream,
		})
	}

	var selectionPolicy *CaddySelectionPolicy
	if len(upstreams) > 1 {
		selectionPolicy = &CaddySelectionPolicy{
			Policy:  "weighted_round_robin",
			Weights: weightsMap,
		}
	} else {
		selectionPolicy = &CaddySelectionPolicy{
			Policy: "round_robin",
		}
	}

	innerHandlers := []CaddyRouteHandler{
		{
			Handler: "headers",
			Response: &CaddyHeadersResponse{
				Set: headerSet,
			},
		},
		{
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
				SelectionPolicy: selectionPolicy,
				Retries:         3,
				TryDuration:     "5s",
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

// SetTrafficSplit updates Caddy reverse proxy direct or weighted upstream routing via PUT /id/{route_id} (<15ms).
func (c *HTTPCaddyClient) SetTrafficSplit(ctx context.Context, domain string, cfg TrafficSplitConfig) error {
	if domain != "" && cfg.Domain == "" {
		cfg.Domain = domain
	}
	cfg.Domain = strings.TrimSpace(cfg.Domain)
	if cfg.Domain == "" {
		return fmt.Errorf("%w: domain cannot be empty", ErrInvalidRoutePayload)
	}

	if len(cfg.Splits) == 0 {
		cfg.StableUpstream = strings.TrimSpace(cfg.StableUpstream)
		if cfg.StableUpstream == "" {
			return fmt.Errorf("%w: at least one upstream target must be specified", ErrInvalidRoutePayload)
		}
		cfg.Splits = []UpstreamWeight{
			{Upstream: cfg.StableUpstream, Weight: 100},
		}
	} else {
		totalWeight := 0
		for i, s := range cfg.Splits {
			u := strings.TrimSpace(s.Upstream)
			if u == "" {
				return fmt.Errorf("%w: upstream target dial cannot be empty", ErrInvalidRoutePayload)
			}
			if s.Weight < 0 {
				return fmt.Errorf("%w: upstream weight cannot be negative (got %d for %s)", ErrInvalidRoutePayload, s.Weight, u)
			}
			cfg.Splits[i].Upstream = u
			totalWeight += s.Weight
		}
		if totalWeight <= 0 {
			return fmt.Errorf("%w: sum of upstream weights must be greater than 0", ErrInvalidRoutePayload)
		}
		if cfg.StableUpstream == "" && len(cfg.Splits) > 0 {
			cfg.StableUpstream = cfg.Splits[0].Upstream
		}
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
				var splits []UpstreamWeight
				for _, u := range h.Upstreams {
					w := u.Weight
					if w == 0 && h.LoadBalancing != nil && h.LoadBalancing.SelectionPolicy != nil {
						if pw, ok := h.LoadBalancing.SelectionPolicy.Weights[u.Dial]; ok {
							w = pw
						}
					}
					splits = append(splits, UpstreamWeight{
						Upstream: u.Dial,
						Weight:   w,
					})
				}
				cfg.Splits = splits
				if len(splits) > 0 {
					cfg.StableUpstream = splits[0].Upstream
				}
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
