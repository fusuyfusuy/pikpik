package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/fusuycorp/pikpik/pkg/store"
)

// DefaultIngressManager orchestrates Caddy dynamic route operations and state reconciliation.
type DefaultIngressManager struct {
	client    CaddyClient
	validator DomainValidator
	mu        sync.RWMutex
	routes    map[string]RouteSpec
}

// NewIngressManager constructs a new IngressManager instance.
func NewIngressManager(client CaddyClient, validator DomainValidator) *DefaultIngressManager {
	return &DefaultIngressManager{
		client:    client,
		validator: validator,
		routes:    make(map[string]RouteSpec),
	}
}

// ApplyRoute atomically adds or replaces a route in Caddy (<15ms).
func (m *DefaultIngressManager) ApplyRoute(ctx context.Context, spec RouteSpec) error {
	if spec.UpstreamDial == "" {
		return fmt.Errorf("%w: upstream dial cannot be empty", ErrInvalidRoutePayload)
	}
	if len(spec.Hosts) == 0 && len(spec.PathPrefixes) == 0 {
		return fmt.Errorf("%w: at least one host or path prefix must be specified", ErrInvalidRoutePayload)
	}

	if spec.ID == "" {
		firstHost := ""
		if len(spec.Hosts) > 0 {
			firstHost = spec.Hosts[0]
		}
		spec.ID = GenerateRouteID(spec.ServiceID, firstHost)
	}

	caddyRoute := BuildCaddyRoute(spec)
	if err := m.client.PutRoute(ctx, spec.ID, caddyRoute); err != nil {
		return err
	}

	m.mu.Lock()
	m.routes[spec.ID] = spec
	m.mu.Unlock()

	return nil
}

// RemoveRoute atomically deletes a route by its ID.
func (m *DefaultIngressManager) RemoveRoute(ctx context.Context, routeID string) error {
	if routeID == "" {
		return fmt.Errorf("%w: route id cannot be empty", ErrInvalidRoutePayload)
	}

	if err := m.client.DeleteRoute(ctx, routeID); err != nil {
		return err
	}

	m.mu.Lock()
	delete(m.routes, routeID)
	m.mu.Unlock()

	return nil
}

// GetRoute retrieves the active route specification by ID.
func (m *DefaultIngressManager) GetRoute(ctx context.Context, routeID string) (*RouteSpec, error) {
	if routeID == "" {
		return nil, fmt.Errorf("%w: route id cannot be empty", ErrInvalidRoutePayload)
	}

	m.mu.RLock()
	cached, ok := m.routes[routeID]
	m.mu.RUnlock()
	if ok {
		return &cached, nil
	}

	caddyRoute, err := m.client.GetRoute(ctx, routeID)
	if err != nil {
		return nil, err
	}

	return CaddyRouteToSpec(*caddyRoute)
}

// ListRoutes retrieves all active routes currently loaded in Caddy.
func (m *DefaultIngressManager) ListRoutes(ctx context.Context) ([]RouteSpec, error) {
	caddyRoutes, err := m.client.ListRoutes(ctx)
	if err != nil {
		return nil, err
	}

	specs := make([]RouteSpec, 0, len(caddyRoutes))
	for _, cr := range caddyRoutes {
		spec, err := CaddyRouteToSpec(cr)
		if err != nil {
			continue
		}
		specs = append(specs, *spec)
	}

	return specs, nil
}

// ReconcileAll rebuilds the full Caddy routing and TLS state.
func (m *DefaultIngressManager) ReconcileAll(ctx context.Context, routes []RouteSpec, tlsCfg GlobalTLSConfig) error {
	config := BuildCaddyConfig(routes, tlsCfg)
	if err := m.client.LoadFullConfig(ctx, config); err != nil {
		return err
	}

	m.mu.Lock()
	m.routes = make(map[string]RouteSpec, len(routes))
	for _, r := range routes {
		m.routes[r.ID] = r
	}
	m.mu.Unlock()

	return nil
}

// ReconcileFromStore queries active services from the persistent store and synchronizes Caddy.
func (m *DefaultIngressManager) ReconcileFromStore(ctx context.Context, st store.Store, tlsCfg GlobalTLSConfig) error {
	return ReconcileFromStore(ctx, m, st, tlsCfg)
}

// VerifyDomain checks if a domain is allowed to obtain an On-Demand certificate.
func (m *DefaultIngressManager) VerifyDomain(ctx context.Context, domain string) (bool, error) {
	if m.validator == nil {
		return false, ErrDomainNotWhitelisted
	}
	allowed, err := m.validator.VerifyDomain(ctx, domain)
	if err != nil {
		return false, err
	}
	if !allowed {
		return false, ErrDomainNotWhitelisted
	}
	return true, nil
}

// HealthCheck returns nil if Caddy Admin API is responding within SLA (<5ms).
func (m *DefaultIngressManager) HealthCheck(ctx context.Context) error {
	return m.client.Ping(ctx)
}

// ReconcileFromStore synchronizes all active services and domains from a store.Store into the IngressManager.
func ReconcileFromStore(ctx context.Context, mgr IngressManager, st store.Store, tlsCfg GlobalTLSConfig) error {
	var routes []RouteSpec

	if db := st.DB(); db != nil {
		query := `SELECT id, name, slug, type, replicas, container_port, domain_names, status FROM services WHERE status NOT IN ('stopped', 'failed')`
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("ingress: store query failed: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var id, name, slug, svcType, status, domainsJSON string
			var replicas, containerPort int
			if err := rows.Scan(&id, &name, &slug, &svcType, &replicas, &containerPort, &domainsJSON, &status); err != nil {
				continue
			}
			if containerPort <= 0 {
				continue
			}

			var domainList []string
			if err := json.Unmarshal([]byte(domainsJSON), &domainList); err != nil {
				continue
			}

			for _, d := range domainList {
				d = strings.TrimSpace(d)
				if d == "" {
					continue
				}
				routeID := GenerateRouteID(id, d)
				upstreamDial := fmt.Sprintf("%s:%d", slug, containerPort)
				routes = append(routes, RouteSpec{
					ID:           routeID,
					ServiceID:    id,
					Hosts:        []string{d},
					UpstreamDial: upstreamDial,
					EnableHSTS:   true,
				})
			}
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("ingress: error scanning store services: %w", err)
		}
	}

	return mgr.ReconcileAll(ctx, routes, tlsCfg)
}
