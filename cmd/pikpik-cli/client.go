package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/gorilla/websocket"
)

// APIClient wraps typed HTTP and WebSocket communication with the remote pikpik control plane.
type APIClient struct {
	baseURL    string
	token      string
	httpClient *http.Client
	tlsSkip    bool
}

// NewAPIClient creates an APIClient from Context settings.
func NewAPIClient(c Context) *APIClient {
	timeout := time.Duration(c.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: c.TLSSkipVerify,
		},
	}

	return &APIClient{
		baseURL: strings.TrimSuffix(c.ServerURL, "/"),
		token:   c.Token,
		tlsSkip: c.TLSSkipVerify,
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
	}
}

func (c *APIClient) doRequest(ctx context.Context, method, path string, bodyIn any, respOut any) error {
	var bodyReader io.Reader
	if bodyIn != nil {
		data, err := json.Marshal(bodyIn)
		if err != nil {
			return fmt.Errorf("failed to serialize request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response failed: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp api.ErrorResponse
		if err := json.Unmarshal(bodyBytes, &errResp); err == nil && errResp.Error.Message != "" {
			return fmt.Errorf("API Error [%s]: %s (HTTP %d)", errResp.Error.Code, errResp.Error.Message, resp.StatusCode)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	if respOut != nil {
		if err := json.Unmarshal(bodyBytes, respOut); err != nil {
			return fmt.Errorf("parsing response JSON failed: %w", err)
		}
	}

	return nil
}

// Login authenticates with email/password.
func (c *APIClient) Login(ctx context.Context, email, password string) (*api.LoginResponse, error) {
	var res api.Response[api.LoginResponse]
	req := api.LoginRequest{Email: email, Password: password}
	if err := c.doRequest(ctx, "POST", "/api/v1/auth/login", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// GetMe retrieves the active user profile.
func (c *APIClient) GetMe(ctx context.Context) (*api.UserDTO, error) {
	var res api.Response[api.UserDTO]
	if err := c.doRequest(ctx, "GET", "/api/v1/auth/me", nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// ListNodes lists cluster nodes.
func (c *APIClient) ListNodes(ctx context.Context) ([]api.SwarmNode, error) {
	var res api.Response[[]api.SwarmNode]
	if err := c.doRequest(ctx, "GET", "/api/v1/nodes", nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// DrainNode marks a node as drained.
func (c *APIClient) DrainNode(ctx context.Context, nodeID string) error {
	req := api.UpdateNodeRequest{Availability: "drain"}
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "PATCH", "/api/v1/nodes/"+nodeID, req, &res)
}

// GetJoinTokens retrieves manager and worker join tokens.
func (c *APIClient) GetJoinTokens(ctx context.Context) (*api.JoinTokensResponse, error) {
	var res api.Response[api.JoinTokensResponse]
	if err := c.doRequest(ctx, "GET", "/api/v1/nodes/join-tokens", nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// ListApps lists deployed applications.
func (c *APIClient) ListApps(ctx context.Context) ([]api.App, error) {
	var res api.Response[[]api.App]
	if err := c.doRequest(ctx, "GET", "/api/v1/apps", nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// GetApp retrieves app details.
func (c *APIClient) GetApp(ctx context.Context, id string) (*api.App, error) {
	var res api.Response[api.App]
	if err := c.doRequest(ctx, "GET", "/api/v1/apps/"+id, nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// CreateApp creates a new application.
func (c *APIClient) CreateApp(ctx context.Context, req api.CreateAppRequest) (*api.App, error) {
	var res api.Response[api.App]
	if err := c.doRequest(ctx, "POST", "/api/v1/apps", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// DeployApp triggers a deployment.
func (c *APIClient) DeployApp(ctx context.Context, id, image string) error {
	req := api.DeployAppRequest{Image: image}
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "POST", "/api/v1/apps/"+id+"/deploy", req, &res)
}

// ListDatabases lists managed databases.
func (c *APIClient) ListDatabases(ctx context.Context) ([]api.Database, error) {
	var res api.Response[[]api.Database]
	if err := c.doRequest(ctx, "GET", "/api/v1/databases", nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// CreateDatabase provisions a database instance.
func (c *APIClient) CreateDatabase(ctx context.Context, req api.CreateDatabaseRequest) (*api.Database, error) {
	var res api.Response[api.Database]
	if err := c.doRequest(ctx, "POST", "/api/v1/databases", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// CreateBackup triggers a database backup.
func (c *APIClient) CreateBackup(ctx context.Context, serviceID string) (*api.Backup, error) {
	req := api.CreateBackupRequest{ServiceID: serviceID}
	var res api.Response[api.Backup]
	if err := c.doRequest(ctx, "POST", "/api/v1/backups", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// ListBackups lists backups.
func (c *APIClient) ListBackups(ctx context.Context) ([]api.Backup, error) {
	var res api.Response[[]api.Backup]
	if err := c.doRequest(ctx, "GET", "/api/v1/backups", nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// RestoreBackup restores a backup.
func (c *APIClient) RestoreBackup(ctx context.Context, backupID, targetServiceID string) error {
	req := api.RestoreBackupRequest{TargetServiceID: targetServiceID}
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "POST", "/api/v1/backups/"+backupID+"/restore", req, &res)
}

// PruneSystem executes cluster garbage collection.
func (c *APIClient) PruneSystem(ctx context.Context, all, volumes bool) (*api.PruneResult, error) {
	req := api.PruneRequest{All: all, Volumes: volumes}
	var res api.Response[api.PruneResult]
	if err := c.doRequest(ctx, "POST", "/api/v1/system/prune", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// WebSocketDialer returns a configured dialer for WSS connections.
func (c *APIClient) WebSocketDialer() *websocket.Dialer {
	return &websocket.Dialer{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: c.tlsSkip,
		},
		Subprotocols: []string{"pikpik-auth." + c.token},
	}
}

// GetWebSocketURL converts http(s) URL to ws(s) URL with path.
func (c *APIClient) GetWebSocketURL(path string) string {
	base := c.baseURL
	if strings.HasPrefix(base, "https://") {
		base = "wss://" + strings.TrimPrefix(base, "https://")
	} else if strings.HasPrefix(base, "http://") {
		base = "ws://" + strings.TrimPrefix(base, "http://")
	} else {
		base = "ws://" + base
	}
	return base + path
}
