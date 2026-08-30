package ingress

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HTTPCaddyClient implements CaddyClient using HTTP calls to Caddy Admin API.
type HTTPCaddyClient struct {
	baseURL    string
	httpClient *http.Client
	mu         sync.RWMutex
	splits     map[string]TrafficSplitConfig
}

// NewCaddyClient creates a new HTTPCaddyClient configured with baseURL and timeout.
func NewCaddyClient(baseURL string, timeout time.Duration) *HTTPCaddyClient {
	if baseURL == "" {
		baseURL = "http://127.0.0.1:2019"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	return &HTTPCaddyClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        50,
				MaxIdleConnsPerHost: 50,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		splits: make(map[string]TrafficSplitConfig),
	}
}

// PutRoute pushes a route atomically using PUT /id/{id} (<15ms).
func (c *HTTPCaddyClient) PutRoute(ctx context.Context, routeID string, route CaddyRoute) error {
	if routeID == "" {
		return fmt.Errorf("%w: route id cannot be empty", ErrInvalidRoutePayload)
	}

	payload, err := json.Marshal(route)
	if err != nil {
		return fmt.Errorf("%w: marshal error: %v", ErrInvalidRoutePayload, err)
	}

	url := fmt.Sprintf("%s/id/%s", c.baseURL, routeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCaddyUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: status %d: %s", ErrCaddyMutationFailed, resp.StatusCode, string(body))
	}

	return nil
}

// DeleteRoute deletes a route atomically by @id.
func (c *HTTPCaddyClient) DeleteRoute(ctx context.Context, routeID string) error {
	if routeID == "" {
		return fmt.Errorf("%w: route id cannot be empty", ErrInvalidRoutePayload)
	}

	url := fmt.Sprintf("%s/id/%s", c.baseURL, routeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCaddyUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		c.mu.Lock()
		for domain := range c.splits {
			if GenerateTrafficSplitRouteID(domain) == routeID {
				delete(c.splits, domain)
			}
		}
		c.mu.Unlock()
		return ErrRouteNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: delete status %d: %s", ErrCaddyMutationFailed, resp.StatusCode, string(body))
	}

	c.mu.Lock()
	for domain := range c.splits {
		if GenerateTrafficSplitRouteID(domain) == routeID {
			delete(c.splits, domain)
		}
	}
	c.mu.Unlock()

	return nil
}

// GetRoute retrieves the active route specification by ID.
func (c *HTTPCaddyClient) GetRoute(ctx context.Context, routeID string) (*CaddyRoute, error) {
	if routeID == "" {
		return nil, fmt.Errorf("%w: route id cannot be empty", ErrInvalidRoutePayload)
	}

	url := fmt.Sprintf("%s/id/%s", c.baseURL, routeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCaddyUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrRouteNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: status %d: %s", ErrCaddyMutationFailed, resp.StatusCode, string(body))
	}

	var route CaddyRoute
	if err := json.NewDecoder(resp.Body).Decode(&route); err != nil {
		return nil, fmt.Errorf("failed to decode caddy route: %w", err)
	}
	return &route, nil
}

// ListRoutes returns all active routes from the default HTTP server.
func (c *HTTPCaddyClient) ListRoutes(ctx context.Context) ([]CaddyRoute, error) {
	url := fmt.Sprintf("%s/config/apps/http/servers/srv0/routes", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCaddyUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return []CaddyRoute{}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: status %d: %s", ErrCaddyMutationFailed, resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(body) == 0 || string(body) == "null" {
		return []CaddyRoute{}, nil
	}

	var routes []CaddyRoute
	if err := json.Unmarshal(body, &routes); err != nil {
		return nil, fmt.Errorf("failed to decode routes: %w", err)
	}
	return routes, nil
}

// LoadFullConfig performs a complete atomic swap of Caddy config via POST /load.
func (c *HTTPCaddyClient) LoadFullConfig(ctx context.Context, config CaddyConfig) error {
	payload, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("%w: config marshal failed: %v", ErrInvalidRoutePayload, err)
	}

	url := fmt.Sprintf("%s/load", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCaddyUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%w: load returned status %d: %s", ErrCaddyMutationFailed, resp.StatusCode, string(body))
	}

	return nil
}

// Ping checks if the Caddy Admin API is live and responding.
func (c *HTTPCaddyClient) Ping(ctx context.Context) error {
	url := fmt.Sprintf("%s/config/", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCaddyUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: unexpected ping status %d", ErrCaddyUnreachable, resp.StatusCode)
	}
	return nil
}

// GetRawConfig retrieves the raw JSON configuration from Caddy Admin API (GET /config/).
func (c *HTTPCaddyClient) GetRawConfig(ctx context.Context) (json.RawMessage, error) {
	url := fmt.Sprintf("%s/config/", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCaddyUnreachable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: status %d: %s", ErrCaddyMutationFailed, resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(body), nil
}
