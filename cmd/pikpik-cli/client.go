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
	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"github.com/fusuycorp/pikpik/pkg/templates"
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

// ListMachines lists all registered and managed host machines.
func (c *APIClient) ListMachines(ctx context.Context) ([]api.MachineDTO, error) {
	var res api.Response[[]api.MachineDTO]
	if err := c.doRequest(ctx, "GET", "/api/v1/machines", nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// GetMachine retrieves detailed machine metadata and telemetry.
func (c *APIClient) GetMachine(ctx context.Context, id string) (*api.MachineDTO, error) {
	var res api.Response[api.MachineDTO]
	if err := c.doRequest(ctx, "GET", "/api/v1/machines/"+id, nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// DeleteMachine deletes a machine registration.
func (c *APIClient) DeleteMachine(ctx context.Context, id string) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "DELETE", "/api/v1/machines/"+id, nil, &res)
}

// JoinSwarmMachine commands a remote machine to join the Docker Swarm mesh.
func (c *APIClient) JoinSwarmMachine(ctx context.Context, id string, req api.JoinSwarmRequest) (*api.SwarmNode, error) {
	var res api.Response[api.SwarmNode]
	if err := c.doRequest(ctx, "POST", "/api/v1/machines/"+id+"/join-swarm", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// GetMachineMetrics retrieves real-time telemetry metrics for a machine.
func (c *APIClient) GetMachineMetrics(ctx context.Context, id string) (*telemetry.HostMetrics, error) {
	var res api.Response[telemetry.HostMetrics]
	if err := c.doRequest(ctx, "GET", "/api/v1/machines/"+id+"/metrics", nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// GetMachineEnrollCommand gets copyable install snippet for enrolling new machines.
func (c *APIClient) GetMachineEnrollCommand(ctx context.Context) (*api.EnrollMachineResponse, error) {
	var res api.Response[api.EnrollMachineResponse]
	if err := c.doRequest(ctx, "GET", "/api/v1/machines/enroll", nil, &res); err != nil {
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

// GetAppTraffic retrieves active traffic split distribution for an app.
func (c *APIClient) GetAppTraffic(ctx context.Context, appID string) (*api.TrafficSplitResponse, error) {
	var res api.Response[api.TrafficSplitResponse]
	if err := c.doRequest(ctx, "GET", "/api/v1/apps/"+appID+"/traffic", nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// SetAppTraffic sets weighted traffic split distribution for an app.
func (c *APIClient) SetAppTraffic(ctx context.Context, appID string, req api.SetTrafficSplitRequest) (*api.TrafficSplitResponse, error) {
	var res api.Response[api.TrafficSplitResponse]
	if err := c.doRequest(ctx, "POST", "/api/v1/apps/"+appID+"/traffic", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
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

// ListProjects lists projects.
func (c *APIClient) ListProjects(ctx context.Context, orgID, tag string) ([]api.ProjectDTO, error) {
	url := "/api/v1/projects"
	var params []string
	if orgID != "" {
		params = append(params, "org_id="+orgID)
	}
	if tag != "" {
		params = append(params, "tag="+tag)
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}
	var res api.Response[[]api.ProjectDTO]
	if err := c.doRequest(ctx, "GET", url, nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// CreateProject creates a new project.
func (c *APIClient) CreateProject(ctx context.Context, req api.CreateProjectRequest) (*api.ProjectDTO, error) {
	var res api.Response[api.ProjectDTO]
	if err := c.doRequest(ctx, "POST", "/api/v1/projects", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// DeleteProject deletes a project.
func (c *APIClient) DeleteProject(ctx context.Context, id string) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "DELETE", "/api/v1/projects/"+id, nil, &res)
}

// ListTags retrieves unique tags across resources.
func (c *APIClient) ListTags(ctx context.Context) ([]api.TagSummary, error) {
	var res api.Response[[]api.TagSummary]
	if err := c.doRequest(ctx, "GET", "/api/v1/tags", nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// InspectCompose analyzes a compose YAML definition.
func (c *APIClient) InspectCompose(ctx context.Context, composeYAML string) (*api.InspectComposeResponse, error) {
	req := api.InspectComposeRequest{ComposeYAML: composeYAML}
	var res api.Response[api.InspectComposeResponse]
	if err := c.doRequest(ctx, "POST", "/api/v1/apps/inspect-compose", req, &res); err != nil {
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

// --- Stacks Methods ---

// ListStacks lists compose stacks.
func (c *APIClient) ListStacks(ctx context.Context) ([]api.Stack, error) {
	var res api.Response[[]api.Stack]
	if err := c.doRequest(ctx, "GET", "/api/v1/stacks", nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// GetStack retrieves details of a compose stack.
func (c *APIClient) GetStack(ctx context.Context, id string) (*api.Stack, error) {
	var res api.Response[api.Stack]
	if err := c.doRequest(ctx, "GET", "/api/v1/stacks/"+id, nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// CreateStack creates a new compose stack.
func (c *APIClient) CreateStack(ctx context.Context, req api.CreateStackRequest) (*api.Stack, error) {
	var res api.Response[api.Stack]
	if err := c.doRequest(ctx, "POST", "/api/v1/stacks", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// DeployStack triggers deployment of a compose stack.
func (c *APIClient) DeployStack(ctx context.Context, id string) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "POST", "/api/v1/stacks/"+id+"/deploy", nil, &res)
}

// RestartStack restarts all containers in a stack.
func (c *APIClient) RestartStack(ctx context.Context, id string) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "POST", "/api/v1/stacks/"+id+"/restart", nil, &res)
}

// StopStack stops all containers in a stack.
func (c *APIClient) StopStack(ctx context.Context, id string) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "POST", "/api/v1/stacks/"+id+"/stop", nil, &res)
}

// DeleteStack removes a stack and associated containers.
func (c *APIClient) DeleteStack(ctx context.Context, id string) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "DELETE", "/api/v1/stacks/"+id, nil, &res)
}

// GetStackLogs retrieves runtime logs/status for a stack.
func (c *APIClient) GetStackLogs(ctx context.Context, id string) (map[string]any, error) {
	var res api.Response[map[string]any]
	if err := c.doRequest(ctx, "GET", "/api/v1/stacks/"+id+"/logs", nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// --- Networks Methods ---

// ListNetworks lists managed networks.
func (c *APIClient) ListNetworks(ctx context.Context, projectID string) ([]api.NetworkDTO, error) {
	url := "/api/v1/networks"
	if projectID != "" {
		url += "?project_id=" + projectID
	}
	var res api.Response[[]api.NetworkDTO]
	if err := c.doRequest(ctx, "GET", url, nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// GetNetwork retrieves network details.
func (c *APIClient) GetNetwork(ctx context.Context, id string) (*api.NetworkDTO, error) {
	var res api.Response[api.NetworkDTO]
	if err := c.doRequest(ctx, "GET", "/api/v1/networks/"+id, nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// CreateNetwork creates a new managed network.
func (c *APIClient) CreateNetwork(ctx context.Context, req api.CreateNetworkRequest) (*api.NetworkDTO, error) {
	var res api.Response[api.NetworkDTO]
	if err := c.doRequest(ctx, "POST", "/api/v1/networks", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// DeleteNetwork removes a managed network.
func (c *APIClient) DeleteNetwork(ctx context.Context, id string) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "DELETE", "/api/v1/networks/"+id, nil, &res)
}

// PruneNetworks prunes unused networks.
func (c *APIClient) PruneNetworks(ctx context.Context, projectID string) (*api.PruneResult, error) {
	url := "/api/v1/networks/prune"
	if projectID != "" {
		url += "?project_id=" + projectID
	}
	var res api.Response[api.PruneResult]
	if err := c.doRequest(ctx, "POST", url, nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// --- Volumes Methods ---

// ListVolumes lists managed persistent volumes.
func (c *APIClient) ListVolumes(ctx context.Context, projectID string) ([]api.VolumeDTO, error) {
	url := "/api/v1/volumes"
	if projectID != "" {
		url += "?project_id=" + projectID
	}
	var res api.Response[[]api.VolumeDTO]
	if err := c.doRequest(ctx, "GET", url, nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// GetVolume retrieves volume details.
func (c *APIClient) GetVolume(ctx context.Context, id string) (*api.VolumeDTO, error) {
	var res api.Response[api.VolumeDTO]
	if err := c.doRequest(ctx, "GET", "/api/v1/volumes/"+id, nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// CreateVolume provisions a new managed volume.
func (c *APIClient) CreateVolume(ctx context.Context, req api.CreateVolumeRequest) (*api.VolumeDTO, error) {
	var res api.Response[api.VolumeDTO]
	if err := c.doRequest(ctx, "POST", "/api/v1/volumes", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// DeleteVolume deletes a managed volume.
func (c *APIClient) DeleteVolume(ctx context.Context, id string) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "DELETE", "/api/v1/volumes/"+id, nil, &res)
}

// PruneVolumes prunes unused volumes.
func (c *APIClient) PruneVolumes(ctx context.Context, projectID string) (*api.PruneResult, error) {
	url := "/api/v1/volumes/prune"
	if projectID != "" {
		url += "?project_id=" + projectID
	}
	var res api.Response[api.PruneResult]
	if err := c.doRequest(ctx, "POST", url, nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// --- Ingress / Domain Methods ---

// ListDomains lists all custom domain bindings across projects.
func (c *APIClient) ListDomains(ctx context.Context) ([]api.DomainBinding, error) {
	var res api.Response[[]api.DomainBinding]
	if err := c.doRequest(ctx, "GET", "/api/v1/ingress/domains", nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// BindDomain binds a domain name to an application.
func (c *APIClient) BindDomain(ctx context.Context, req api.BindDomainRequest) (*api.DomainBinding, error) {
	var res api.Response[api.DomainBinding]
	if err := c.doRequest(ctx, "POST", "/api/v1/ingress/domains", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// DeleteDomain deletes a domain binding.
func (c *APIClient) DeleteDomain(ctx context.Context, id string) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "DELETE", "/api/v1/ingress/domains/"+id, nil, &res)
}

// UploadCertificate uploads a custom TLS certificate and private key.
func (c *APIClient) UploadCertificate(ctx context.Context, req api.CertificateUploadRequest) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "POST", "/api/v1/ingress/certificates", req, &res)
}

// ReconcileIngress forces Caddy to re-sync all routes and TLS config.
func (c *APIClient) ReconcileIngress(ctx context.Context) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "POST", "/api/v1/ingress/reconcile", nil, &res)
}

// GetCaddyConfig retrieves active Caddy configuration and diagnostics.
func (c *APIClient) GetCaddyConfig(ctx context.Context) (*api.CaddyDiagnosticsDTO, error) {
	var res api.Response[api.CaddyDiagnosticsDTO]
	if err := c.doRequest(ctx, "GET", "/api/v1/ingress/caddy/config", nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// --- Registry Methods ---

// GetRegistryStatus retrieves the embedded container registry daemon status.
func (c *APIClient) GetRegistryStatus(ctx context.Context) (*api.RegistryStatusResponse, error) {
	var res api.Response[api.RegistryStatusResponse]
	if err := c.doRequest(ctx, "GET", "/api/v1/registry/status", nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// ListRepositories retrieves the image catalog and tags.
func (c *APIClient) ListRepositories(ctx context.Context) (*api.RepositoryCatalogResponse, error) {
	var res api.Response[api.RepositoryCatalogResponse]
	if err := c.doRequest(ctx, "GET", "/api/v1/registry/repositories", nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// GetRegistryCredentials retrieves robot credentials for automated push/pull.
func (c *APIClient) GetRegistryCredentials(ctx context.Context, projectID string) ([]api.RobotCredentialsResponse, error) {
	url := "/api/v1/registry/credentials"
	if projectID != "" {
		url += "?project_id=" + projectID
	}
	var res api.Response[[]api.RobotCredentialsResponse]
	if err := c.doRequest(ctx, "GET", url, nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// RotateRegistryCredentials rotates the robot credential token.
func (c *APIClient) RotateRegistryCredentials(ctx context.Context, id string) (*api.RobotCredentialsResponse, error) {
	url := "/api/v1/registry/credentials/rotate"
	if id != "" {
		url += "?id=" + id
	}
	var res api.Response[api.RobotCredentialsResponse]
	if err := c.doRequest(ctx, "POST", url, nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// GarbageCollectRegistry prunes unreferenced image blobs and manifests.
func (c *APIClient) GarbageCollectRegistry(ctx context.Context) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "POST", "/api/v1/registry/garbage-collect", nil, &res)
}

// --- Templates Methods ---

// ListTemplates lists marketplace templates with optional category/search filters.
func (c *APIClient) ListTemplates(ctx context.Context, category, search string) ([]templates.Template, error) {
	url := "/api/v1/templates"
	params := []string{}
	if category != "" && category != "All" {
		params = append(params, "category="+category)
	}
	if search != "" {
		params = append(params, "search="+search)
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}
	var res api.Response[[]templates.Template]
	if err := c.doRequest(ctx, "GET", url, nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// GetTemplate retrieves a template specification and environment variables schema.
func (c *APIClient) GetTemplate(ctx context.Context, id string) (*templates.Template, error) {
	var res api.Response[templates.Template]
	if err := c.doRequest(ctx, "GET", "/api/v1/templates/"+id, nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// DeployTemplate deploys a 1-click marketplace template stack.
func (c *APIClient) DeployTemplate(ctx context.Context, id string, req templates.DeployTemplateRequest) (*templates.DeployTemplateResponse, error) {
	var res api.Response[templates.DeployTemplateResponse]
	if err := c.doRequest(ctx, "POST", "/api/v1/templates/"+id+"/deploy", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// --- Backup Schedules Methods ---

// ListBackupSchedules lists automated cron backup schedules.
func (c *APIClient) ListBackupSchedules(ctx context.Context, serviceID string) ([]*store.BackupSchedule, error) {
	url := "/api/v1/backups/schedules"
	if serviceID != "" {
		url += "?service_id=" + serviceID
	}
	var res api.Response[[]*store.BackupSchedule]
	if err := c.doRequest(ctx, "GET", url, nil, &res); err != nil {
		return nil, err
	}
	return res.Data, nil
}

// CreateBackupSchedule provisions a new cron backup schedule.
func (c *APIClient) CreateBackupSchedule(ctx context.Context, req api.CreateBackupScheduleRequest) (*store.BackupSchedule, error) {
	var res api.Response[store.BackupSchedule]
	if err := c.doRequest(ctx, "POST", "/api/v1/backups/schedules", req, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// GetBackupSchedule retrieves a backup schedule by ID.
func (c *APIClient) GetBackupSchedule(ctx context.Context, id string) (*store.BackupSchedule, error) {
	var res api.Response[store.BackupSchedule]
	if err := c.doRequest(ctx, "GET", "/api/v1/backups/schedules/"+id, nil, &res); err != nil {
		return nil, err
	}
	return &res.Data, nil
}

// DeleteBackupSchedule removes a backup schedule.
func (c *APIClient) DeleteBackupSchedule(ctx context.Context, id string) error {
	var res api.Response[map[string]any]
	return c.doRequest(ctx, "DELETE", "/api/v1/backups/schedules/"+id, nil, &res)
}


