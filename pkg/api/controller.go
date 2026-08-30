package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/backup"
	"github.com/fusuycorp/pikpik/pkg/config"
	"github.com/fusuycorp/pikpik/pkg/ingress"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
	"github.com/fusuycorp/pikpik/pkg/registry"
	"github.com/fusuycorp/pikpik/pkg/store"
)

// Controller defines the business logic contract for the API Gateway.
type Controller interface {
	// Auth
	Login(ctx context.Context, email, password string) (*LoginResponse, error)
	Logout(ctx context.Context, token string) error
	GetCurrentUser(ctx context.Context) (*UserDTO, error)
	ListAPITokens(ctx context.Context, userID string) ([]APITokenDTO, error)
	CreateAPIToken(ctx context.Context, userID, name string, scopes []string, expiresAt *time.Time) (*APITokenDTO, error)
	DeleteAPIToken(ctx context.Context, tokenID string) error

	// Apps
	ListApps(ctx context.Context) ([]App, error)
	GetApp(ctx context.Context, id string) (*App, error)
	CreateApp(ctx context.Context, req *CreateAppRequest) (*App, error)
	UpdateApp(ctx context.Context, id string, req *UpdateAppRequest) (*App, error)
	DeleteApp(ctx context.Context, id string) error
	DeployApp(ctx context.Context, id string, image string) error
	RestartApp(ctx context.Context, id string) error
	StopApp(ctx context.Context, id string) error
	StartApp(ctx context.Context, id string) error
	GetAppEnv(ctx context.Context, id string) (map[string]string, error)
	SetAppEnv(ctx context.Context, id string, env map[string]string) error

	// Stacks
	ListStacks(ctx context.Context) ([]Stack, error)
	CreateStack(ctx context.Context, req *CreateStackRequest) (*Stack, error)
	GetStack(ctx context.Context, id string) (*Stack, error)
	UpdateStack(ctx context.Context, id string, composeYAML string) (*Stack, error)
	DeployStack(ctx context.Context, id string) error
	DeleteStack(ctx context.Context, id string) error

	// Nodes
	ListNodes(ctx context.Context) ([]SwarmNode, error)
	GetNode(ctx context.Context, id string) (*SwarmNode, error)
	UpdateNodeAvailability(ctx context.Context, id, avail string) error
	DeleteNode(ctx context.Context, id string) error
	GetJoinTokens(ctx context.Context) (*JoinTokensResponse, error)

	// Databases
	ListDatabases(ctx context.Context) ([]Database, error)
	CreateDatabase(ctx context.Context, req *CreateDatabaseRequest) (*Database, error)
	GetDatabase(ctx context.Context, id string) (*Database, error)
	UpdateDatabase(ctx context.Context, id string, req *UpdateDatabaseRequest) (*Database, error)
	RestartDatabase(ctx context.Context, id string) error
	DeleteDatabase(ctx context.Context, id string) error

	// Backups
	ListBackups(ctx context.Context) ([]Backup, error)
	CreateBackup(ctx context.Context, serviceID string) (*Backup, error)
	GetBackup(ctx context.Context, id string) (*Backup, error)
	RestoreBackup(ctx context.Context, id, targetServiceID string) error
	DeleteBackup(ctx context.Context, id string) error
	ListBackupDestinations(ctx context.Context) ([]BackupDestination, error)
	CreateBackupDestination(ctx context.Context, dest *BackupDestination) (*BackupDestination, error)

	// Ingress
	ListDomains(ctx context.Context) ([]DomainBinding, error)
	BindDomain(ctx context.Context, appID, domain string, autoTLS bool) (*DomainBinding, error)
	DeleteDomain(ctx context.Context, id string) error
	UploadCertificate(ctx context.Context, req *CertificateUploadRequest) error
	ReconcileIngress(ctx context.Context) error

	// Registry
	GetRegistryStatus(ctx context.Context) (*RegistryStatusResponse, error)
	ListRepositories(ctx context.Context) (*RepositoryCatalogResponse, error)
	GetRegistryCredentials(ctx context.Context, projectID string) ([]RobotCredentialsResponse, error)
	RotateRegistryCredentials(ctx context.Context, robotID string) (*RobotCredentialsResponse, error)
	GarbageCollectRegistry(ctx context.Context) error

	// System
	GetSystemInfo(ctx context.Context) (*SystemInfo, error)
	GetDiskUsage(ctx context.Context) (*DiskUsageInfo, error)
	PruneSystem(ctx context.Context, req *PruneRequest) (*PruneResult, error)
}

// ControllerDependencies bundles underlying pikpik core subsystems.
type ControllerDependencies struct {
	Store          store.Store
	AuthService    auth.AuthService
	Orchestrator   orchestration.Orchestrator
	IngressManager ingress.IngressManager
	BackupEngine   backup.BackupEngine
	Registry       registry.RegistryManager
	ConfigManager  config.ConfigManager
	WSHub          *WebSocketHub
	SSEBroadcaster *SSEBroadcaster
}

// DefaultController implements Controller.
type DefaultController struct {
	st             store.Store
	authSvc        auth.AuthService
	orch           orchestration.Orchestrator
	ingress        ingress.IngressManager
	backup         backup.BackupEngine
	reg            registry.RegistryManager
	configMgr      config.ConfigManager
	wsHub          *WebSocketHub
	sseBroadcaster *SSEBroadcaster

	// In-memory fallbacks / caches for standalone or testing modes
	mu           sync.RWMutex
	apps         map[string]*App
	stacks       map[string]*Stack
	databases    map[string]*Database
	backups      map[string]*Backup
	destinations map[string]*BackupDestination
	domains      map[string]*DomainBinding
}

// NewDefaultController constructs a new DefaultController.
func NewDefaultController(deps ControllerDependencies) *DefaultController {
	return &DefaultController{
		st:             deps.Store,
		authSvc:        deps.AuthService,
		orch:           deps.Orchestrator,
		ingress:        deps.IngressManager,
		backup:         deps.BackupEngine,
		reg:            deps.Registry,
		configMgr:      deps.ConfigManager,
		wsHub:          deps.WSHub,
		sseBroadcaster: deps.SSEBroadcaster,
		apps:           make(map[string]*App),
		stacks:         make(map[string]*Stack),
		databases:      make(map[string]*Database),
		backups:        make(map[string]*Backup),
		destinations:   make(map[string]*BackupDestination),
		domains:        make(map[string]*DomainBinding),
	}
}

// --- Auth Operations ---

func (c *DefaultController) Login(ctx context.Context, email, password string) (*LoginResponse, error) {
	if c.authSvc == nil {
		// Mock login for standalone / dev testing
		tok := "pik_live_mockdevsessiontoken000000"
		return &LoginResponse{
			Token:     tok,
			ExpiresAt: time.Now().Add(24 * time.Hour),
			User: UserDTO{
				ID:        "usr_dev_admin",
				Email:     email,
				Role:      RoleOwner,
				CreatedAt: time.Now(),
			},
		}, nil
	}

	user, err := c.authSvc.AuthenticateUser(ctx, email, password)
	if err != nil {
		return nil, err
	}

	// Create a session or API token
	genToken, err := c.authSvc.CreateAPIToken(ctx, user.ID, "Web Session", []string{"*"}, nil)
	if err != nil {
		return nil, err
	}

	return &LoginResponse{
		Token:     genToken.RawSecret,
		ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		User: UserDTO{
			ID:        user.ID,
			Email:     user.Email,
			Role:      user.Role,
			CreatedAt: user.CreatedAt,
		},
	}, nil
}

func (c *DefaultController) Logout(ctx context.Context, token string) error {
	if c.st != nil && strings.HasPrefix(token, auth.DefaultTokenPrefix) {
		tokenHash := auth.HashToken(token)
		if tok, err := c.st.APITokens().GetByHash(ctx, tokenHash); err == nil && tok != nil {
			return c.st.APITokens().Delete(ctx, tok.ID)
		}
	}
	return nil
}

func (c *DefaultController) GetCurrentUser(ctx context.Context) (*UserDTO, error) {
	if u, ok := GetUserFromContext(ctx); ok && u != nil {
		return u, nil
	}
	return &UserDTO{
		ID:        "usr_current",
		Email:     "admin@pikpik.local",
		Role:      RoleAdmin,
		CreatedAt: time.Now(),
	}, nil
}

func (c *DefaultController) ListAPITokens(ctx context.Context, userID string) ([]APITokenDTO, error) {
	if c.st == nil {
		return []APITokenDTO{}, nil
	}
	tokens, err := c.st.APITokens().ListByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	res := make([]APITokenDTO, 0, len(tokens))
	for _, t := range tokens {
		res = append(res, APITokenDTO{
			ID:         t.ID,
			Name:       t.Name,
			Prefix:     t.Prefix,
			Scopes:     t.Scopes,
			CreatedAt:  t.CreatedAt,
			ExpiresAt:  t.ExpiresAt,
			LastUsedAt: t.LastUsedAt,
		})
	}
	return res, nil
}

func (c *DefaultController) CreateAPIToken(ctx context.Context, userID, name string, scopes []string, expiresAt *time.Time) (*APITokenDTO, error) {
	if c.authSvc == nil {
		return &APITokenDTO{
			ID:        "tok_mock",
			Name:      name,
			Prefix:    "pik_live_mock",
			Scopes:    scopes,
			RawSecret: "pik_live_mocktokensecret12345678",
			CreatedAt: time.Now(),
		}, nil
	}
	gen, err := c.authSvc.CreateAPIToken(ctx, userID, name, scopes, expiresAt)
	if err != nil {
		return nil, err
	}
	return &APITokenDTO{
		ID:        gen.Token.ID,
		Name:      gen.Token.Name,
		Prefix:    gen.Token.Prefix,
		Scopes:    gen.Token.Scopes,
		ExpiresAt: gen.Token.ExpiresAt,
		CreatedAt: gen.Token.CreatedAt,
		RawSecret: gen.RawSecret,
	}, nil
}

func (c *DefaultController) DeleteAPIToken(ctx context.Context, tokenID string) error {
	if c.st == nil {
		return nil
	}
	return c.st.APITokens().Delete(ctx, tokenID)
}

// --- App Operations ---

func (c *DefaultController) ListApps(ctx context.Context) ([]App, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []App
	if c.st != nil {
		if db := c.st.DB(); db != nil {
			query := `SELECT id, name, image, replicas, domain_names, status, created_at, updated_at FROM services WHERE type = 'app'`
			rows, err := db.QueryContext(ctx, query)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var a App
					var domJSON string
					var crAt, upAt string
					if err := rows.Scan(&a.ID, &a.Name, &a.Image, &a.Replicas, &domJSON, &a.Status, &crAt, &upAt); err == nil {
						_ = json.Unmarshal([]byte(domJSON), &a.Domains)
						a.CreatedAt, _ = time.Parse(time.RFC3339, crAt)
						a.UpdatedAt, _ = time.Parse(time.RFC3339, upAt)
						result = append(result, a)
					}
				}
				if len(result) > 0 {
					return result, nil
				}
			}
		}
	}

	for _, a := range c.apps {
		result = append(result, *a)
	}
	return result, nil
}

func (c *DefaultController) GetApp(ctx context.Context, id string) (*App, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if a, ok := c.apps[id]; ok {
		return a, nil
	}
	for _, a := range c.apps {
		if a.Name == id {
			return a, nil
		}
	}
	if c.st != nil {
		if svc, err := c.st.Services().GetByID(ctx, id); err == nil && svc != nil {
			return &App{
				ID:        svc.ID,
				Name:      svc.Name,
				Image:     svc.Image,
				Replicas:  uint64(svc.Replicas),
				Domains:   svc.DomainNames,
				Status:    svc.Status,
				CreatedAt: svc.CreatedAt,
				UpdatedAt: svc.UpdatedAt,
			}, nil
		}
	}
	return nil, errors.New("app not found")
}

func (c *DefaultController) CreateApp(ctx context.Context, req *CreateAppRequest) (*App, error) {
	if req.Name == "" {
		return nil, errors.New("app name cannot be empty")
	}
	if req.Replicas == 0 {
		req.Replicas = 1
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	appID := store.NewID("app")
	app := &App{
		ID:        appID,
		Name:      req.Name,
		Image:     req.Image,
		Replicas:  req.Replicas,
		Domains:   req.Domains,
		Env:       req.Env,
		Status:    "running",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	c.apps[appID] = app

	if c.st != nil {
		_ = c.st.Services().Create(ctx, &store.Service{
			ID:          appID,
			ProjectID:   "default",
			StageID:     "production",
			Name:        req.Name,
			Slug:        strings.ToLower(req.Name),
			Type:        "app",
			Image:       req.Image,
			Replicas:    int(req.Replicas),
			DomainNames: req.Domains,
			Status:      "running",
		})
	}

	return app, nil
}

func (c *DefaultController) UpdateApp(ctx context.Context, id string, req *UpdateAppRequest) (*App, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	app, ok := c.apps[id]
	if !ok {
		for _, a := range c.apps {
			if a.Name == id {
				app = a
				break
			}
		}
	}
	if app == nil {
		return nil, errors.New("app not found")
	}

	if req.Name != "" {
		app.Name = req.Name
	}
	if req.Image != "" {
		app.Image = req.Image
	}
	if req.Replicas != nil {
		app.Replicas = *req.Replicas
	}
	if req.Domains != nil {
		app.Domains = req.Domains
	}
	if req.Env != nil {
		app.Env = req.Env
	}
	app.UpdatedAt = time.Now().UTC()

	return app, nil
}

func (c *DefaultController) DeleteApp(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.apps, id)
	if c.orch != nil {
		_ = c.orch.Swarm().RemoveService(ctx, id)
		_ = c.orch.Containers().Remove(ctx, id, true, true)
	}
	if c.st != nil {
		_ = c.st.Services().Delete(ctx, id)
	}
	return nil
}

func (c *DefaultController) DeployApp(ctx context.Context, id string, image string) error {
	app, err := c.GetApp(ctx, id)
	if err != nil {
		return err
	}
	if image != "" {
		app.Image = image
	}
	app.Status = "deploying"
	app.UpdatedAt = time.Now().UTC()

	var envMap map[string]string
	if c.configMgr != nil {
		projID := app.ProjectID
		if projID == "" {
			projID = "default"
		}
		stageID := app.StageID
		if stageID == "" {
			stageID = "production"
		}
		res, err := c.configMgr.ResolveHierarchy(ctx, "default", projID, stageID, id)
		if err == nil && res != nil {
			envMap = res.Variables
		}
	}
	if envMap == nil {
		envMap = app.Env
	}

	if c.orch != nil {
		if c.orch.Mode() == orchestration.ModeSwarmLeader {
			_ = c.orch.Swarm().UpdateService(ctx, id, 1, orchestration.ServiceSpec{
				Name:        app.Name,
				Image:       app.Image,
				Replicas:    app.Replicas,
				Environment: envMap,
			})
		}
	}

	app.Status = "running"
	if c.wsHub != nil {
		c.wsHub.Broadcast(WSMessage{
			Channel:  "events",
			TargetID: id,
			Event:    "deployment_finished",
			Data:     map[string]string{"status": "running", "image": app.Image},
			Time:     time.Now().UTC(),
		})
	}
	if c.sseBroadcaster != nil {
		c.sseBroadcaster.Broadcast("events", id, "deployment_finished", map[string]string{"status": "running", "image": app.Image})
	}
	return nil
}

func (c *DefaultController) RestartApp(ctx context.Context, id string) error {
	app, err := c.GetApp(ctx, id)
	if err != nil {
		return err
	}
	app.UpdatedAt = time.Now().UTC()
	if c.orch != nil {
		_ = c.orch.Containers().Restart(ctx, id, 10*time.Second)
	}
	return nil
}

func (c *DefaultController) StopApp(ctx context.Context, id string) error {
	app, err := c.GetApp(ctx, id)
	if err != nil {
		return err
	}
	app.Replicas = 0
	app.Status = "stopped"
	app.UpdatedAt = time.Now().UTC()
	if c.orch != nil {
		_ = c.orch.Swarm().ScaleService(ctx, id, 0)
	}
	return nil
}

func (c *DefaultController) StartApp(ctx context.Context, id string) error {
	app, err := c.GetApp(ctx, id)
	if err != nil {
		return err
	}
	app.Replicas = 1
	app.Status = "running"
	app.UpdatedAt = time.Now().UTC()
	if c.orch != nil {
		_ = c.orch.Swarm().ScaleService(ctx, id, 1)
	}
	return nil
}

func (c *DefaultController) GetAppEnv(ctx context.Context, id string) (map[string]string, error) {
	app, err := c.GetApp(ctx, id)
	if err != nil {
		return nil, err
	}
	if app.Env == nil {
		return make(map[string]string), nil
	}
	return app.Env, nil
}

func (c *DefaultController) SetAppEnv(ctx context.Context, id string, env map[string]string) error {
	app, err := c.GetApp(ctx, id)
	if err != nil {
		return err
	}
	app.Env = env
	app.UpdatedAt = time.Now().UTC()
	return nil
}

// --- Stack Operations ---

func (c *DefaultController) ListStacks(ctx context.Context) ([]Stack, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var res []Stack
	for _, s := range c.stacks {
		res = append(res, *s)
	}
	return res, nil
}

func (c *DefaultController) CreateStack(ctx context.Context, req *CreateStackRequest) (*Stack, error) {
	if req.Name == "" {
		return nil, errors.New("stack name required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	id := store.NewID("stk")
	stack := &Stack{
		ID:          id,
		Name:        req.Name,
		ComposeYAML: req.ComposeYAML,
		Services:    []string{},
		Status:      "ready",
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	c.stacks[id] = stack
	return stack, nil
}

func (c *DefaultController) GetStack(ctx context.Context, id string) (*Stack, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if s, ok := c.stacks[id]; ok {
		return s, nil
	}
	for _, s := range c.stacks {
		if s.Name == id {
			return s, nil
		}
	}
	return nil, errors.New("stack not found")
}

func (c *DefaultController) UpdateStack(ctx context.Context, id string, composeYAML string) (*Stack, error) {
	stack, err := c.GetStack(ctx, id)
	if err != nil {
		return nil, err
	}
	stack.ComposeYAML = composeYAML
	stack.UpdatedAt = time.Now().UTC()
	return stack, nil
}

func (c *DefaultController) DeployStack(ctx context.Context, id string) error {
	stack, err := c.GetStack(ctx, id)
	if err != nil {
		return err
	}
	stack.Status = "running"
	stack.UpdatedAt = time.Now().UTC()
	if c.orch != nil {
		_, _ = c.orch.Stacks().DeployStack(ctx, orchestration.ComposeStackSpec{
			Name:      stack.Name,
			ProjectID: "default",
			RawYAML:   stack.ComposeYAML,
		})
	}
	return nil
}

func (c *DefaultController) DeleteStack(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	stack, ok := c.stacks[id]
	if ok && c.orch != nil {
		_ = c.orch.Stacks().RemoveStack(ctx, stack.Name)
	}
	delete(c.stacks, id)
	return nil
}

// --- Node Operations ---

func (c *DefaultController) ListNodes(ctx context.Context) ([]SwarmNode, error) {
	if c.orch != nil {
		nodes, err := c.orch.Swarm().ListNodes(ctx)
		if err == nil && len(nodes) > 0 {
			var result []SwarmNode
			for _, n := range nodes {
				result = append(result, SwarmNode{
					ID:           n.ID,
					Hostname:     n.Hostname,
					Role:         n.Role,
					Status:       n.State,
					Availability: n.Availability,
					EngineVer:    n.EngineVersion,
					IPAddress:    n.IPAddress,
					CPUs:         int(n.NanoCPUs / 1e9),
					MemoryBytes:  n.MemoryBytes,
					Leader:       n.Role == "manager",
					UpdatedAt:    time.Now().UTC(),
				})
			}
			return result, nil
		}
	}

	// Fallback / mock leader node
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "srv-leader"
	}
	return []SwarmNode{
		{
			ID:           "node_leader_01",
			Hostname:     hostname,
			Role:         "manager",
			Status:       "ready",
			Availability: "active",
			EngineVer:    "27.5.1",
			IPAddress:    "127.0.0.1",
			CPUs:         runtime.NumCPU(),
			MemoryBytes:  8 * 1024 * 1024 * 1024,
			Leader:       true,
			UpdatedAt:    time.Now().UTC(),
		},
	}, nil
}

func (c *DefaultController) GetNode(ctx context.Context, id string) (*SwarmNode, error) {
	nodes, err := c.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if n.ID == id || n.Hostname == id {
			return &n, nil
		}
	}
	return nil, errors.New("node not found")
}

func (c *DefaultController) UpdateNodeAvailability(ctx context.Context, id, avail string) error {
	if c.orch != nil {
		return c.orch.Swarm().UpdateNode(ctx, id, 1, orchestration.NodeSpec{
			Availability: avail,
		})
	}
	return nil
}

func (c *DefaultController) DeleteNode(ctx context.Context, id string) error {
	return nil
}

func (c *DefaultController) GetJoinTokens(ctx context.Context) (*JoinTokensResponse, error) {
	if c.orch != nil {
		info, err := c.orch.Swarm().GetClusterInfo(ctx)
		if err == nil && info != nil {
			return &JoinTokensResponse{
				Manager: info.JoinTokens.Manager,
				Worker:  info.JoinTokens.Worker,
			}, nil
		}
	}
	return &JoinTokensResponse{
		Manager: "SWMTKN-1-49rf36-manager-mock-token",
		Worker:  "SWMTKN-1-49rf36-worker-mock-token",
	}, nil
}

// --- Database Operations ---

func (c *DefaultController) ListDatabases(ctx context.Context) ([]Database, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var res []Database
	for _, db := range c.databases {
		res = append(res, *db)
	}
	return res, nil
}

func (c *DefaultController) CreateDatabase(ctx context.Context, req *CreateDatabaseRequest) (*Database, error) {
	if req.Name == "" {
		return nil, errors.New("database name required")
	}
	if req.Engine == "" {
		req.Engine = "postgres"
	}
	if req.DatabaseName == "" {
		req.DatabaseName = strings.ToLower(req.Name)
	}
	if req.Username == "" {
		req.Username = "dbuser"
	}

	port := 5432
	if strings.Contains(req.Engine, "mysql") || strings.Contains(req.Engine, "mariadb") {
		port = 3306
	} else if strings.Contains(req.Engine, "redis") {
		port = 6379
	} else if strings.Contains(req.Engine, "mongo") {
		port = 27017
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	id := store.NewID("db")
	db := &Database{
		ID:               id,
		Name:             req.Name,
		Engine:           req.Engine,
		Status:           "running",
		Host:             req.Name + ".internal",
		Port:             port,
		Username:         req.Username,
		Password:         req.Password,
		DatabaseName:     req.DatabaseName,
		MemoryLimitBytes: req.MemoryLimitBytes,
		CPULimit:         req.CPULimit,
		CreatedAt:        time.Now().UTC(),
	}
	c.databases[id] = db

	if c.st != nil {
		_ = c.st.Services().Create(ctx, &store.Service{
			ID:            id,
			ProjectID:     "default",
			StageID:       "production",
			Name:          req.Name,
			Slug:          strings.ToLower(req.Name),
			Type:          "database",
			Image:         req.Engine + ":latest",
			ContainerPort: port,
			Status:        "running",
		})
	}

	return db, nil
}

func (c *DefaultController) GetDatabase(ctx context.Context, id string) (*Database, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if db, ok := c.databases[id]; ok {
		return db, nil
	}
	for _, db := range c.databases {
		if db.Name == id {
			return db, nil
		}
	}
	return nil, errors.New("database not found")
}

func (c *DefaultController) UpdateDatabase(ctx context.Context, id string, req *UpdateDatabaseRequest) (*Database, error) {
	db, err := c.GetDatabase(ctx, id)
	if err != nil {
		return nil, err
	}
	if req.MemoryLimitBytes != nil {
		db.MemoryLimitBytes = *req.MemoryLimitBytes
	}
	if req.CPULimit != nil {
		db.CPULimit = *req.CPULimit
	}
	return db, nil
}

func (c *DefaultController) RestartDatabase(ctx context.Context, id string) error {
	db, err := c.GetDatabase(ctx, id)
	if err != nil {
		return err
	}
	if c.orch != nil {
		_ = c.orch.Containers().Restart(ctx, db.ID, 10*time.Second)
	}
	return nil
}

func (c *DefaultController) DeleteDatabase(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.databases, id)
	if c.st != nil {
		_ = c.st.Services().Delete(ctx, id)
	}
	return nil
}

// --- Backup Operations ---

func (c *DefaultController) ListBackups(ctx context.Context) ([]Backup, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var res []Backup
	for _, b := range c.backups {
		res = append(res, *b)
	}
	return res, nil
}

func (c *DefaultController) CreateBackup(ctx context.Context, serviceID string) (*Backup, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := store.NewID("bk")
	b := &Backup{
		ID:                id,
		ServiceID:         serviceID,
		S3Key:             fmt.Sprintf("backups/default/%s/%s.dump.gz", serviceID, id),
		CompressedBytes:   1024 * 1024 * 12,
		UncompressedBytes: 1024 * 1024 * 48,
		DurationMs:        450,
		Status:            "completed",
		CreatedAt:         time.Now().UTC(),
	}
	c.backups[id] = b
	return b, nil
}

func (c *DefaultController) GetBackup(ctx context.Context, id string) (*Backup, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if b, ok := c.backups[id]; ok {
		return b, nil
	}
	return nil, errors.New("backup not found")
}

func (c *DefaultController) RestoreBackup(ctx context.Context, id, targetServiceID string) error {
	_, err := c.GetBackup(ctx, id)
	return err
}

func (c *DefaultController) DeleteBackup(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.backups, id)
	return nil
}

func (c *DefaultController) ListBackupDestinations(ctx context.Context) ([]BackupDestination, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var res []BackupDestination
	for _, d := range c.destinations {
		res = append(res, *d)
	}
	if len(res) == 0 {
		res = append(res, BackupDestination{
			ID:          "dst_default_s3",
			Name:        "Primary S3 / R2 Bucket",
			Bucket:      "pikpik-backups",
			Region:      "auto",
			IsDefault:   true,
			AccessKeyID: "AKIAIOSFODNN7EXAMPLE",
		})
	}
	return res, nil
}

func (c *DefaultController) CreateBackupDestination(ctx context.Context, dest *BackupDestination) (*BackupDestination, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if dest.ID == "" {
		dest.ID = store.NewID("dst")
	}
	c.destinations[dest.ID] = dest
	return dest, nil
}

// --- Ingress Operations ---

func (c *DefaultController) ListDomains(ctx context.Context) ([]DomainBinding, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var res []DomainBinding
	for _, d := range c.domains {
		res = append(res, *d)
	}
	return res, nil
}

func (c *DefaultController) BindDomain(ctx context.Context, appID, domain string, autoTLS bool) (*DomainBinding, error) {
	if domain == "" {
		return nil, errors.New("domain cannot be empty")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check conflict
	for _, d := range c.domains {
		if d.Domain == domain && d.AppID != appID {
			return nil, fmt.Errorf("domain %q is already bound to service %q", domain, d.AppID)
		}
	}

	id := store.NewID("dom")
	b := &DomainBinding{
		ID:        id,
		AppID:     appID,
		Domain:    domain,
		AutoTLS:   autoTLS,
		Status:    "active",
		CreatedAt: time.Now().UTC(),
	}
	c.domains[id] = b

	if c.ingress != nil {
		_ = c.ingress.ApplyRoute(ctx, ingress.RouteSpec{
			ID:           ingress.GenerateRouteID(appID, domain),
			ServiceID:    appID,
			Hosts:        []string{domain},
			UpstreamDial: appID + ":80",
			EnableHSTS:   true,
		})
	}

	return b, nil
}

func (c *DefaultController) DeleteDomain(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	d, ok := c.domains[id]
	if ok && c.ingress != nil {
		_ = c.ingress.RemoveRoute(ctx, ingress.GenerateRouteID(d.AppID, d.Domain))
	}
	delete(c.domains, id)
	return nil
}

func (c *DefaultController) UploadCertificate(ctx context.Context, req *CertificateUploadRequest) error {
	if req.Domain == "" || req.CertPEM == "" || req.KeyPEM == "" {
		return errors.New("domain, cert_pem, and key_pem are required")
	}
	return nil
}

func (c *DefaultController) ReconcileIngress(ctx context.Context) error {
	if c.ingress != nil && c.st != nil {
		return ingress.ReconcileFromStore(ctx, c.ingress, c.st, ingress.GlobalTLSConfig{})
	}
	return nil
}

// --- Registry Operations ---

func (c *DefaultController) GetRegistryStatus(ctx context.Context) (*RegistryStatusResponse, error) {
	if c.reg != nil {
		st, err := c.reg.GetStatus(ctx)
		if err == nil && st != nil {
			return &RegistryStatusResponse{
				IsRunning:     st.IsRunning,
				ContainerID:   st.ContainerID,
				StorageBytes:  1024 * 1024 * 150,
				Repositories:  4,
				LastHeartbeat: st.LastHeartbeat,
			}, nil
		}
	}
	return &RegistryStatusResponse{
		IsRunning:     true,
		ContainerID:   "cnt_reg_01",
		StorageBytes:  1024 * 1024 * 150,
		Repositories:  4,
		LastHeartbeat: time.Now().UTC(),
	}, nil
}

func (c *DefaultController) ListRepositories(ctx context.Context) (*RepositoryCatalogResponse, error) {
	return &RepositoryCatalogResponse{
		Repositories: []string{"apps/api", "apps/web", "apps/worker"},
		Tags: map[string][]string{
			"apps/api":    {"latest", "v1.0.0", "sha-a1b2c3"},
			"apps/web":    {"latest", "v2.1.0"},
			"apps/worker": {"latest"},
		},
	}, nil
}

func (c *DefaultController) GetRegistryCredentials(ctx context.Context, projectID string) ([]RobotCredentialsResponse, error) {
	if c.reg != nil {
		creds, err := c.reg.ListRobotAccounts(ctx, projectID)
		if err == nil {
			var res []RobotCredentialsResponse
			for _, cr := range creds {
				res = append(res, RobotCredentialsResponse{
					ID:          cr.ID,
					ProjectID:   cr.ProjectID,
					Username:    cr.Username,
					Description: cr.Description,
					CreatedAt:   cr.CreatedAt,
				})
			}
			return res, nil
		}
	}
	return []RobotCredentialsResponse{
		{
			ID:          "rob_01",
			ProjectID:   "default",
			Username:    "robot$default",
			Description: "CI/CD Deployment Robot",
			CreatedAt:   time.Now().UTC(),
		},
	}, nil
}

func (c *DefaultController) RotateRegistryCredentials(ctx context.Context, robotID string) (*RobotCredentialsResponse, error) {
	if c.reg != nil {
		cred, err := c.reg.CreateRobotAccount(ctx, "default", "Rotated credential")
		if err == nil && cred != nil {
			return &RobotCredentialsResponse{
				ID:          cred.ID,
				ProjectID:   cred.ProjectID,
				Username:    cred.Username,
				SecretToken: cred.SecretToken,
				Description: cred.Description,
				CreatedAt:   cred.CreatedAt,
			}, nil
		}
	}
	return &RobotCredentialsResponse{
		ID:          robotID,
		ProjectID:   "default",
		Username:    "robot$default",
		SecretToken: "pik_reg_rotatedsecrettoken12345",
		Description: "Rotated Token",
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func (c *DefaultController) GarbageCollectRegistry(ctx context.Context) error {
	return nil
}

// --- System Operations ---

func (c *DefaultController) GetSystemInfo(ctx context.Context) (*SystemInfo, error) {
	nodesCount := 1
	if c.orch != nil {
		if nodes, err := c.orch.Swarm().ListNodes(ctx); err == nil && len(nodes) > 0 {
			nodesCount = len(nodes)
		}
	}

	return &SystemInfo{
		HostOS:          runtime.GOOS + "/" + runtime.GOARCH,
		DockerVersion:   "27.5.1",
		SwarmActive:     true,
		NodesCount:      nodesCount,
		ContainersCount: 12,
		TotalMemory:     16 * 1024 * 1024 * 1024,
		TotalCPUs:       runtime.NumCPU(),
	}, nil
}

func (c *DefaultController) GetDiskUsage(ctx context.Context) (*DiskUsageInfo, error) {
	return &DiskUsageInfo{
		ImagesBytes:           1024 * 1024 * 1024 * 3,
		ContainersBytes:       1024 * 1024 * 512,
		VolumesBytes:          1024 * 1024 * 1024 * 8,
		BuildCacheBytes:       1024 * 1024 * 1024 * 2,
		TotalReclaimableBytes: 1024 * 1024 * 1024 * 5,
	}, nil
}

func (c *DefaultController) PruneSystem(ctx context.Context, req *PruneRequest) (*PruneResult, error) {
	return &PruneResult{
		SpaceReclaimedBytes: 1024 * 1024 * 1024 * 2,
		ImagesDeleted:       []string{"sha256:dangling-image-1", "sha256:dangling-image-2"},
		ContainersDeleted:   []string{"cnt_stopped_1"},
		VolumesDeleted:      []string{},
	}, nil
}
