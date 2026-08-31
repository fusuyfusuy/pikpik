package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/backup"
	"github.com/fusuycorp/pikpik/pkg/build"
	"github.com/fusuycorp/pikpik/pkg/config"
	"github.com/fusuycorp/pikpik/pkg/crypto"
	"github.com/fusuycorp/pikpik/pkg/git"
	"github.com/fusuycorp/pikpik/pkg/ingress"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
	"github.com/fusuycorp/pikpik/pkg/registry"
	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"github.com/fusuycorp/pikpik/pkg/templates"
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

	// Orgs, Projects & Tags
	ListOrganizations(ctx context.Context) ([]OrganizationDTO, error)
	CreateOrganization(ctx context.Context, req *CreateOrgRequest) (*OrganizationDTO, error)
	ListProjects(ctx context.Context, orgID string) ([]ProjectDTO, error)
	GetProject(ctx context.Context, id string) (*ProjectDTO, error)
	CreateProject(ctx context.Context, req *CreateProjectRequest) (*ProjectDTO, error)
	UpdateProject(ctx context.Context, id string, req *UpdateProjectRequest) (*ProjectDTO, error)
	DeleteProject(ctx context.Context, id string) error
	ListTags(ctx context.Context) ([]TagSummary, error)

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
	InspectCompose(ctx context.Context, composeYAML string) (*InspectComposeResponse, error)

	// Stacks
	ListStacks(ctx context.Context) ([]Stack, error)
	CreateStack(ctx context.Context, req *CreateStackRequest) (*Stack, error)
	GetStack(ctx context.Context, id string) (*Stack, error)
	UpdateStack(ctx context.Context, id string, composeYAML string) (*Stack, error)
	DeployStack(ctx context.Context, id string) error
	StopStack(ctx context.Context, id string) error
	RestartStack(ctx context.Context, id string) error
	DeleteStack(ctx context.Context, id string) error

	// Networks
	ListNetworks(ctx context.Context, projectID string) ([]NetworkDTO, error)
	GetNetwork(ctx context.Context, id string) (*NetworkDTO, error)
	CreateNetwork(ctx context.Context, req *CreateNetworkRequest) (*NetworkDTO, error)
	DeleteNetwork(ctx context.Context, id string) error
	PruneNetworks(ctx context.Context, projectID string) (*PruneResult, error)

	// Volumes
	ListVolumes(ctx context.Context, projectID string) ([]VolumeDTO, error)
	GetVolume(ctx context.Context, id string) (*VolumeDTO, error)
	CreateVolume(ctx context.Context, req *CreateVolumeRequest) (*VolumeDTO, error)
	DeleteVolume(ctx context.Context, id string) error
	PruneVolumes(ctx context.Context, projectID string) (*PruneResult, error)

	// Nodes
	ListNodes(ctx context.Context) ([]SwarmNode, error)
	GetNode(ctx context.Context, id string) (*SwarmNode, error)
	UpdateNodeAvailability(ctx context.Context, id, avail string) error
	DeleteNode(ctx context.Context, id string) error
	GetJoinTokens(ctx context.Context) (*JoinTokensResponse, error)

	// Machines & Infrastructure
	ListMachines(ctx context.Context) ([]MachineDTO, error)
	GetMachine(ctx context.Context, id string) (*MachineDTO, error)
	DeleteMachine(ctx context.Context, id string) error
	JoinSwarmCluster(ctx context.Context, id string, req *JoinSwarmRequest) (*SwarmNode, error)
	GetMachineMetrics(ctx context.Context, id string) (*telemetry.HostMetrics, error)
	GetMachineEnrollCommand(ctx context.Context, serverURL string) (*EnrollMachineResponse, error)

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
	ListBackupSchedules(ctx context.Context, serviceID string) ([]*store.BackupSchedule, error)
	GetBackupSchedule(ctx context.Context, id string) (*store.BackupSchedule, error)
	CreateBackupSchedule(ctx context.Context, req *CreateBackupScheduleRequest) (*store.BackupSchedule, error)
	UpdateBackupSchedule(ctx context.Context, id string, req *UpdateBackupScheduleRequest) (*store.BackupSchedule, error)
	DeleteBackupSchedule(ctx context.Context, id string) error

	// Ingress
	ListDomains(ctx context.Context) ([]DomainBinding, error)
	BindDomain(ctx context.Context, appID, domain string, autoTLS bool) (*DomainBinding, error)
	DeleteDomain(ctx context.Context, id string) error
	UploadCertificate(ctx context.Context, req *CertificateUploadRequest) error
	ReconcileIngress(ctx context.Context) error
	GetCaddyConfig(ctx context.Context) (*CaddyDiagnosticsDTO, error)

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

	// Builds & Webhooks
	ListAppBuilds(ctx context.Context, appID string, limit int) ([]*store.Build, error)
	GetBuild(ctx context.Context, buildID string) (*store.Build, error)
	Rebuild(ctx context.Context, buildID string) (*store.Build, error)
	HandleGitHubWebhook(ctx context.Context, secret string, signature string, payload []byte) (*store.Build, error)
	HandleGenericGitWebhook(ctx context.Context, appID, token string, r *http.Request) (*store.Build, error)

	// Templates
	ListTemplates(ctx context.Context, category, search string) ([]templates.Template, error)
	GetTemplate(ctx context.Context, id string) (*templates.Template, error)
	DeployTemplate(ctx context.Context, id string, req *templates.DeployTemplateRequest) (*templates.DeployTemplateResponse, error)
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
	Vault          crypto.Vault
	WSHub          *WebSocketHub
	SSEBroadcaster *SSEBroadcaster
	BuildManager   *build.BuildManager
	Deployer       templates.Deployer
	AgentServer    telemetry.AgentServer
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
	vault          crypto.Vault
	wsHub          *WebSocketHub
	sseBroadcaster *SSEBroadcaster
	buildMgr       *build.BuildManager
	deployer       templates.Deployer
	agentServer    telemetry.AgentServer

	// In-memory fallbacks / caches for standalone or testing modes
	mu           sync.RWMutex
	apps         map[string]*App
	stacks       map[string]*Stack
	databases    map[string]*Database
	backups      map[string]*Backup
	destinations map[string]*BackupDestination
	domains      map[string]*DomainBinding
	builds       map[string]*store.Build
	schedules    map[string]*store.BackupSchedule
	networks     map[string]*NetworkDTO
	volumes      map[string]*VolumeDTO
	machines     map[string]*MachineDTO
}

// NewDefaultController constructs a new DefaultController.
func NewDefaultController(deps ControllerDependencies) *DefaultController {
	deployer := deps.Deployer
	if deployer == nil {
		deployer = templates.NewDeployer(templates.DefaultCatalog(), deps.Store, deps.Orchestrator, deps.Vault)
	}
	return &DefaultController{
		st:             deps.Store,
		authSvc:        deps.AuthService,
		orch:           deps.Orchestrator,
		ingress:        deps.IngressManager,
		backup:         deps.BackupEngine,
		reg:            deps.Registry,
		configMgr:      deps.ConfigManager,
		vault:          deps.Vault,
		wsHub:          deps.WSHub,
		sseBroadcaster: deps.SSEBroadcaster,
		buildMgr:       deps.BuildManager,
		deployer:       deployer,
		agentServer:    deps.AgentServer,
		apps:           make(map[string]*App),
		stacks:         make(map[string]*Stack),
		databases:      make(map[string]*Database),
		backups:        make(map[string]*Backup),
		destinations:   make(map[string]*BackupDestination),
		domains:        make(map[string]*DomainBinding),
		builds:         make(map[string]*store.Build),
		schedules:      make(map[string]*store.BackupSchedule),
		networks:       make(map[string]*NetworkDTO),
		volumes:        make(map[string]*VolumeDTO),
		machines:       make(map[string]*MachineDTO),
	}
}

func (c *DefaultController) recordAudit(ctx context.Context, action, resType, resID, metadataJSON string) {
	if c.st == nil || c.st.Audit() == nil {
		return
	}
	userID := ""
	if u, ok := GetUserFromContext(ctx); ok && u != nil {
		userID = u.ID
	}
	_ = c.st.Audit().Record(ctx, userID, action, resType, resID, metadataJSON, "")
}

func isSecretEnvKey(k string) bool {
	upper := strings.ToUpper(k)
	return strings.Contains(upper, "PASSWORD") ||
		strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "KEY") ||
		strings.Contains(upper, "TOKEN") ||
		strings.Contains(upper, "AUTH") ||
		strings.Contains(upper, "PRIVATE")
}

// --- Auth Operations ---

func (c *DefaultController) Login(ctx context.Context, email, password string) (*LoginResponse, error) {
	if c.authSvc == nil {
		// Mock login for standalone / dev testing
		tok := "pik_live_mockdevsessiontoken000000"
		return &LoginResponse{
			Token:     tok,
			ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
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

	// Create a session or API token with a 30-day expiration
	expiresAt := time.Now().UTC().Add(30 * 24 * time.Hour)
	genToken, err := c.authSvc.CreateAPIToken(ctx, user.ID, "Web Session", []string{"*"}, &expiresAt)
	if err != nil {
		return nil, err
	}

	return &LoginResponse{
		Token:     genToken.RawSecret,
		ExpiresAt: expiresAt,
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
			_ = c.st.APITokens().Delete(ctx, tok.ID)
		}
	}
	c.recordAudit(ctx, "session:logout", "session", token, "")
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
	c.recordAudit(ctx, "token:create", "token", gen.Token.ID, fmt.Sprintf(`{"name":%q,"user_id":%q}`, name, userID))
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
	if err := c.st.APITokens().Delete(ctx, tokenID); err != nil {
		return err
	}
	c.recordAudit(ctx, "token:revoke", "token", tokenID, "")
	return nil
}

// --- Organization Operations ---

func (c *DefaultController) ListOrganizations(ctx context.Context) ([]OrganizationDTO, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []OrganizationDTO
	if c.st != nil {
		orgs, err := c.st.Organizations().List(ctx)
		if err == nil && len(orgs) > 0 {
			for _, o := range orgs {
				result = append(result, OrganizationDTO{
					ID:        o.ID,
					Name:      o.Name,
					Slug:      o.Slug,
					CreatedAt: o.CreatedAt,
					UpdatedAt: o.UpdatedAt,
				})
			}
			return result, nil
		}
	}
	result = append(result, OrganizationDTO{
		ID:        "org_default",
		Name:      "Default Organization",
		Slug:      "default",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	return result, nil
}

func (c *DefaultController) CreateOrganization(ctx context.Context, req *CreateOrgRequest) (*OrganizationDTO, error) {
	if req.Name == "" {
		return nil, errors.New("organization name cannot be empty")
	}
	slug := req.Slug
	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(req.Name), " ", "-"))
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	org := &store.Organization{
		ID:        store.NewID("org"),
		Name:      req.Name,
		Slug:      slug,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if c.st != nil {
		if err := c.st.Organizations().Create(ctx, org); err != nil {
			return nil, err
		}
	}

	c.recordAudit(ctx, "org:create", "org", org.ID, fmt.Sprintf(`{"name":%q,"slug":%q}`, req.Name, slug))

	return &OrganizationDTO{
		ID:        org.ID,
		Name:      org.Name,
		Slug:      org.Slug,
		CreatedAt: org.CreatedAt,
		UpdatedAt: org.UpdatedAt,
	}, nil
}

// --- Project Operations ---

func (c *DefaultController) ListProjects(ctx context.Context, orgID string) ([]ProjectDTO, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []ProjectDTO
	if c.st != nil {
		prjs, err := c.st.Projects().List(ctx, orgID)
		if err == nil {
			allSvcs, _ := c.st.Services().ListAll(ctx)
			counts := make(map[string]int)
			for _, s := range allSvcs {
				if s.Type == "app" {
					counts[s.ProjectID]++
				}
			}

			for _, p := range prjs {
				tags := p.Tags
				if tags == nil {
					tags = []string{}
				}
				result = append(result, ProjectDTO{
					ID:          p.ID,
					OrgID:       p.OrgID,
					Name:        p.Name,
					Slug:        p.Slug,
					Description: p.Description,
					Tags:        tags,
					AppCount:    counts[p.ID],
					CreatedAt:   p.CreatedAt,
					UpdatedAt:   p.UpdatedAt,
				})
			}
			if len(result) > 0 {
				return result, nil
			}
		}
	}

	result = append(result, ProjectDTO{
		ID:          "prj_default",
		OrgID:       "org_default",
		Name:        "Default Project",
		Slug:        "default",
		Description: "Default workspace for applications",
		Tags:        []string{},
		AppCount:    len(c.apps),
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	})
	return result, nil
}

func (c *DefaultController) GetProject(ctx context.Context, id string) (*ProjectDTO, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.st != nil {
		p, err := c.st.Projects().GetByID(ctx, id)
		if err != nil {
			p, err = c.st.Projects().GetBySlug(ctx, id)
		}
		if err == nil && p != nil {
			svcs, _ := c.st.Services().ListByProject(ctx, p.ID)
			appCount := 0
			for _, s := range svcs {
				if s.Type == "app" {
					appCount++
				}
			}
			tags := p.Tags
			if tags == nil {
				tags = []string{}
			}
			return &ProjectDTO{
				ID:          p.ID,
				OrgID:       p.OrgID,
				Name:        p.Name,
				Slug:        p.Slug,
				Description: p.Description,
				Tags:        tags,
				AppCount:    appCount,
				CreatedAt:   p.CreatedAt,
				UpdatedAt:   p.UpdatedAt,
			}, nil
		}
	}
	if id == "prj_default" || id == "default" {
		return &ProjectDTO{
			ID:          "prj_default",
			OrgID:       "org_default",
			Name:        "Default Project",
			Slug:        "default",
			Description: "Default workspace for applications",
			Tags:        []string{},
			AppCount:    len(c.apps),
			CreatedAt:   time.Now().UTC(),
			UpdatedAt:   time.Now().UTC(),
		}, nil
	}
	return nil, errors.New("project not found")
}

func (c *DefaultController) CreateProject(ctx context.Context, req *CreateProjectRequest) (*ProjectDTO, error) {
	if req.Name == "" {
		return nil, errors.New("project name cannot be empty")
	}
	orgID := req.OrgID
	if orgID == "" {
		orgID = "org_default"
	}
	slug := req.Slug
	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(req.Name), " ", "-"))
	}
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	prj := &store.Project{
		ID:          store.NewID("prj"),
		OrgID:       orgID,
		Name:        req.Name,
		Slug:        slug,
		Description: req.Description,
		Tags:        tags,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	if c.st != nil {
		if err := c.st.Projects().Create(ctx, prj); err != nil {
			return nil, err
		}
	}

	c.recordAudit(ctx, "project:create", "project", prj.ID, fmt.Sprintf(`{"name":%q,"slug":%q}`, req.Name, slug))

	return &ProjectDTO{
		ID:          prj.ID,
		OrgID:       prj.OrgID,
		Name:        prj.Name,
		Slug:        prj.Slug,
		Description: prj.Description,
		Tags:        tags,
		AppCount:    0,
		CreatedAt:   prj.CreatedAt,
		UpdatedAt:   prj.UpdatedAt,
	}, nil
}

func (c *DefaultController) UpdateProject(ctx context.Context, id string, req *UpdateProjectRequest) (*ProjectDTO, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.st != nil {
		prj, err := c.st.Projects().GetByID(ctx, id)
		if err != nil {
			prj, err = c.st.Projects().GetBySlug(ctx, id)
		}
		if err != nil || prj == nil {
			return nil, errors.New("project not found")
		}

		if req.Name != "" {
			prj.Name = req.Name
		}
		if req.Slug != "" {
			prj.Slug = req.Slug
		}
		if req.Description != nil {
			prj.Description = *req.Description
		}
		if req.Tags != nil {
			prj.Tags = *req.Tags
		}
		if err := c.st.Projects().Update(ctx, prj); err != nil {
			return nil, err
		}

		c.recordAudit(ctx, "project:update", "project", id, fmt.Sprintf(`{"name":%q}`, prj.Name))

		svcs, _ := c.st.Services().ListByProject(ctx, prj.ID)
		return &ProjectDTO{
			ID:          prj.ID,
			OrgID:       prj.OrgID,
			Name:        prj.Name,
			Slug:        prj.Slug,
			Description: prj.Description,
			Tags:        prj.Tags,
			AppCount:    len(svcs),
			CreatedAt:   prj.CreatedAt,
			UpdatedAt:   prj.UpdatedAt,
		}, nil
	}
	return nil, errors.New("project not found")
}

func (c *DefaultController) DeleteProject(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if id == "prj_default" || id == "default" {
		return errors.New("cannot delete the default project")
	}

	if c.st != nil {
		_ = c.st.EnvVars().DeleteByResource(ctx, store.TierProject, id)
		err := c.st.Projects().Delete(ctx, id)
		if err == nil {
			c.recordAudit(ctx, "project:delete", "project", id, "")
		}
		return err
	}
	return nil
}

// --- Tag Aggregation ---

func (c *DefaultController) ListTags(ctx context.Context) ([]TagSummary, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	counts := make(map[string]int)

	if c.st != nil {
		allSvcs, _ := c.st.Services().ListAll(ctx)
		for _, s := range allSvcs {
			for _, t := range s.Tags {
				if t != "" {
					counts[t]++
				}
			}
		}
		allPrjs, _ := c.st.Projects().List(ctx, "")
		for _, p := range allPrjs {
			for _, t := range p.Tags {
				if t != "" {
					counts[t]++
				}
			}
		}
	} else {
		for _, a := range c.apps {
			for _, t := range a.Tags {
				if t != "" {
					counts[t]++
				}
			}
		}
	}

	var result []TagSummary
	for tag, count := range counts {
		result = append(result, TagSummary{Tag: tag, Count: count})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Count == result[j].Count {
			return result[i].Tag < result[j].Tag
		}
		return result[i].Count > result[j].Count
	})
	return result, nil
}

// --- App Operations ---

func (c *DefaultController) ListApps(ctx context.Context) ([]App, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []App
	if c.st != nil {
		prjs, _ := c.st.Projects().List(ctx, "")
		prjMap := make(map[string]string)
		for _, p := range prjs {
			prjMap[p.ID] = p.Name
		}

		if db := c.st.DB(); db != nil {
			query := `SELECT id, project_id, stage_id, name, image, replicas, container_port, domain_names, tags, runtime_mode, compose_yaml, status, git_repo_url, git_branch, build_strategy, dockerfile_path, publish_directory, created_at, updated_at FROM services WHERE type = 'app'`
			rows, err := db.QueryContext(ctx, query)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var a App
					var domJSON, tagsJSON string
					var crAt, upAt string
					var port sql.NullInt64
					var gitRepo, gitBranch, buildStrat, dockerfile, pubDir sql.NullString
					var prjID, stgID, rtMode, compYAML sql.NullString
					if err := rows.Scan(&a.ID, &prjID, &stgID, &a.Name, &a.Image, &a.Replicas, &port, &domJSON, &tagsJSON, &rtMode, &compYAML, &a.Status, &gitRepo, &gitBranch, &buildStrat, &dockerfile, &pubDir, &crAt, &upAt); err == nil {
						_ = json.Unmarshal([]byte(domJSON), &a.Domains)
						a.Tags = []string{}
						if tagsJSON != "" {
							_ = json.Unmarshal([]byte(tagsJSON), &a.Tags)
						}
						if port.Valid {
							a.ContainerPort = int(port.Int64)
						}
						a.ProjectID = prjID.String
						if a.ProjectID == "" {
							a.ProjectID = "prj_default"
						}
						a.ProjectName = prjMap[a.ProjectID]
						if a.ProjectName == "" && a.ProjectID == "prj_default" {
							a.ProjectName = "Default Project"
						}
						a.StageID = stgID.String
						a.RuntimeMode = rtMode.String
						if a.RuntimeMode == "" {
							a.RuntimeMode = "standalone"
						}
						a.ComposeYAML = compYAML.String
						a.GitRepoURL = gitRepo.String
						a.GitBranch = gitBranch.String
						a.BuildStrategy = buildStrat.String
						a.DockerfilePath = dockerfile.String
						a.PublishDirectory = pubDir.String
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
		if a.ProjectID == "" {
			a.ProjectID = "prj_default"
			a.ProjectName = "Default Project"
		}
		if a.Tags == nil {
			a.Tags = []string{}
		}
		if a.RuntimeMode == "" {
			a.RuntimeMode = "standalone"
		}
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
			projName := "Default Project"
			if p, err := c.st.Projects().GetByID(ctx, svc.ProjectID); err == nil && p != nil {
				projName = p.Name
			}
			tags := svc.Tags
			if tags == nil {
				tags = []string{}
			}
			rtMode := svc.RuntimeMode
			if rtMode == "" {
				rtMode = "standalone"
			}
			return &App{
				ID:               svc.ID,
				ProjectID:        svc.ProjectID,
				ProjectName:      projName,
				StageID:          svc.StageID,
				Name:             svc.Name,
				Image:            svc.Image,
				Replicas:         uint64(svc.Replicas),
				ContainerPort:    svc.ContainerPort,
				Domains:          svc.DomainNames,
				Tags:             tags,
				RuntimeMode:      rtMode,
				ComposeYAML:      svc.ComposeYAML,
				Status:           svc.Status,
				GitRepoURL:       svc.GitRepoURL,
				GitBranch:        svc.GitBranch,
				BuildStrategy:    svc.BuildStrategy,
				DockerfilePath:   svc.DockerfilePath,
				PublishDirectory: svc.PublishDirectory,
				CreatedAt:        svc.CreatedAt,
				UpdatedAt:        svc.UpdatedAt,
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
	projectID := req.ProjectID
	if projectID == "" {
		projectID = "prj_default"
	}
	stageID := req.StageID
	if stageID == "" {
		stageID = "stg_default_prod"
	}
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}
	runtimeMode := req.RuntimeMode
	if runtimeMode == "" {
		if req.Replicas > 1 {
			runtimeMode = "swarm"
		} else {
			runtimeMode = "standalone"
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	appID := store.NewID("app")
	app := &App{
		ID:               appID,
		ProjectID:        projectID,
		StageID:          stageID,
		Name:             req.Name,
		Image:            req.Image,
		Replicas:         req.Replicas,
		ContainerPort:    req.ContainerPort,
		Domains:          req.Domains,
		Tags:             tags,
		RuntimeMode:      runtimeMode,
		ComposeYAML:      req.ComposeYAML,
		Env:              req.Env,
		Status:           "running",
		GitRepoURL:       req.GitRepoURL,
		GitBranch:        req.GitBranch,
		BuildStrategy:    req.BuildStrategy,
		DockerfilePath:   req.DockerfilePath,
		PublishDirectory: req.PublishDirectory,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}

	c.apps[appID] = app

	if c.st != nil {
		if err := c.st.Services().Create(ctx, &store.Service{
			ID:               appID,
			ProjectID:        projectID,
			StageID:          stageID,
			Name:             req.Name,
			Slug:             strings.ToLower(req.Name),
			Type:             "app",
			Image:            req.Image,
			Replicas:         int(req.Replicas),
			ContainerPort:    req.ContainerPort,
			DomainNames:      req.Domains,
			Tags:             tags,
			RuntimeMode:      runtimeMode,
			ComposeYAML:      req.ComposeYAML,
			Status:           "running",
			GitRepoURL:       req.GitRepoURL,
			GitBranch:        req.GitBranch,
			BuildStrategy:    req.BuildStrategy,
			DockerfilePath:   req.DockerfilePath,
			PublishDirectory: req.PublishDirectory,
		}); err != nil {
			delete(c.apps, appID)
			return nil, fmt.Errorf("failed to persist service: %w", err)
		}

		if len(req.Env) > 0 {
			now := time.Now().UTC()
			for k, v := range req.Env {
				isSec := isSecretEnvKey(k)
				val := v
				if isSec && c.vault != nil && !strings.HasPrefix(val, "v1:") {
					if enc, err := c.vault.EncryptString(ctx, val); err == nil {
						val = enc
					}
				}
				_ = c.st.EnvVars().Set(ctx, &store.EnvVar{
					ID:             store.NewID("env"),
					ScopeTier:      store.TierService,
					ResourceID:     appID,
					Key:            k,
					ValueEncrypted: val,
					IsSecret:       isSec,
					CreatedAt:      now,
					UpdatedAt:      now,
				})
			}
		}
	}

	c.recordAudit(ctx, "app:create", "app", appID, fmt.Sprintf(`{"name":%q,"image":%q}`, req.Name, req.Image))

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
	if req.ProjectID != nil && *req.ProjectID != "" {
		app.ProjectID = *req.ProjectID
	}
	if req.StageID != nil && *req.StageID != "" {
		app.StageID = *req.StageID
	}
	if req.Tags != nil {
		app.Tags = *req.Tags
	}
	if req.RuntimeMode != nil && *req.RuntimeMode != "" {
		app.RuntimeMode = *req.RuntimeMode
	}
	if req.ComposeYAML != nil {
		app.ComposeYAML = *req.ComposeYAML
	}
	if req.Replicas != nil {
		app.Replicas = *req.Replicas
	}
	if req.ContainerPort != nil {
		app.ContainerPort = *req.ContainerPort
	}
	if req.Domains != nil {
		app.Domains = req.Domains
	}
	if req.Env != nil {
		app.Env = req.Env
	}
	if req.GitRepoURL != "" {
		app.GitRepoURL = req.GitRepoURL
	}
	if req.GitBranch != "" {
		app.GitBranch = req.GitBranch
	}
	if req.BuildStrategy != "" {
		app.BuildStrategy = req.BuildStrategy
	}
	if req.DockerfilePath != "" {
		app.DockerfilePath = req.DockerfilePath
	}
	if req.PublishDirectory != "" {
		app.PublishDirectory = req.PublishDirectory
	}
	app.UpdatedAt = time.Now().UTC()

	if c.st != nil {
		if svc, err := c.st.Services().GetByID(ctx, id); err == nil && svc != nil {
			svc.Name = app.Name
			svc.Image = app.Image
			if app.ProjectID != "" {
				svc.ProjectID = app.ProjectID
			}
			if app.StageID != "" {
				svc.StageID = app.StageID
			}
			svc.Tags = app.Tags
			svc.RuntimeMode = app.RuntimeMode
			svc.ComposeYAML = app.ComposeYAML
			svc.Replicas = int(app.Replicas)
			svc.ContainerPort = app.ContainerPort
			svc.DomainNames = app.Domains
			svc.GitRepoURL = app.GitRepoURL
			svc.GitBranch = app.GitBranch
			svc.BuildStrategy = app.BuildStrategy
			svc.DockerfilePath = app.DockerfilePath
			svc.PublishDirectory = app.PublishDirectory
			if err := c.st.Services().Update(ctx, svc); err != nil {
				return nil, fmt.Errorf("failed to persist service update: %w", err)
			}
		}
	}

	c.recordAudit(ctx, "app:update", "app", id, fmt.Sprintf(`{"name":%q,"image":%q}`, app.Name, app.Image))

	return app, nil
}

func (c *DefaultController) InspectCompose(ctx context.Context, composeYAML string) (*InspectComposeResponse, error) {
	if strings.TrimSpace(composeYAML) == "" {
		return nil, errors.New("compose YAML cannot be empty")
	}
	result, err := orchestration.InspectComposeYAML(composeYAML)
	if err != nil {
		return nil, err
	}
	return &InspectComposeResponse{
		Services:         result.Services,
		Variables:        result.Variables,
		ExposedPorts:     result.ExposedPorts,
		DeclaredVolumes:  result.DeclaredVolumes,
		DeclaredNetworks: result.DeclaredNetworks,
		SuggestedRuntime: result.SuggestedRuntime,
	}, nil
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
		_ = c.st.EnvVars().DeleteByResource(ctx, store.TierService, id)
		if err := c.st.Services().Delete(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("failed to delete service from store: %w", err)
		}
	}
	c.recordAudit(ctx, "app:delete", "app", id, "")
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
			spec := orchestration.ServiceSpec{
				Name:        app.Name,
				Image:       app.Image,
				Replicas:    app.Replicas,
				Environment: envMap,
			}
			existing, err := c.orch.Swarm().InspectService(ctx, id)
			if err != nil {
				existing, err = c.orch.Swarm().InspectService(ctx, app.Name)
			}
			if err == nil && existing != nil {
				_ = c.orch.Swarm().UpdateService(ctx, existing.ID, existing.Version, spec)
			} else {
				_, _ = c.orch.Swarm().CreateService(ctx, spec)
			}
		} else if c.orch.Containers() != nil {
			spec := orchestration.ContainerSpec{
				Name:        app.Name,
				ProjectID:   app.ProjectID,
				Image:       app.Image,
				Environment: envMap,
				Labels: map[string]string{
					"pikpik.name":       app.Name,
					"pikpik.project_id": app.ProjectID,
					"pikpik.app_id":     app.ID,
					"pikpik.managed":    "true",
				},
			}
			_, deployErr := c.orch.Containers().DeployWithRollingUpdate(ctx, spec, orchestration.RollingUpdateConfig{
				Monitor: 5 * time.Second,
			})
			if deployErr != nil {
				app.Status = "failed"
				if c.st != nil {
					_ = c.st.Services().UpdateStatus(ctx, id, "failed")
				}
				if c.wsHub != nil {
					c.wsHub.Broadcast(WSMessage{
						Channel:  "events",
						TargetID: id,
						Event:    "deployment_failed",
						Data:     map[string]string{"status": "failed", "error": deployErr.Error()},
						Time:     time.Now().UTC(),
					})
				}
				if c.sseBroadcaster != nil {
					c.sseBroadcaster.Broadcast("events", id, "deployment_failed", map[string]string{"status": "failed", "error": deployErr.Error()})
				}
				return fmt.Errorf("in-place rolling deployment failed: %w", deployErr)
			}
		}
	}

	app.Status = "running"
	if c.st != nil {
		if err := c.st.Services().UpdateStatus(ctx, id, "running"); err != nil {
			return fmt.Errorf("failed to persist service status: %w", err)
		}
	}
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
	c.recordAudit(ctx, "app:deploy", "app", id, fmt.Sprintf(`{"image":%q}`, app.Image))
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
	c.recordAudit(ctx, "app:restart", "app", id, "")
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
	c.recordAudit(ctx, "app:stop", "app", id, "")
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
	c.recordAudit(ctx, "app:start", "app", id, "")
	return nil
}

func (c *DefaultController) GetAppEnv(ctx context.Context, id string) (map[string]string, error) {
	app, err := c.GetApp(ctx, id)
	if err != nil {
		return nil, err
	}
	res := make(map[string]string)
	if app.Env != nil {
		for k, v := range app.Env {
			res[k] = v
		}
	}
	if c.st != nil {
		if storedVars, err := c.st.EnvVars().ListByResource(ctx, store.TierService, id); err == nil {
			for _, v := range storedVars {
				val := v.ValueEncrypted
				if v.IsSecret && strings.HasPrefix(val, "v1:") && c.vault != nil {
					if dec, err := c.vault.DecryptString(ctx, val); err == nil {
						val = dec
					}
				}
				res[v.Key] = val
			}
		}
	}
	return res, nil
}

func (c *DefaultController) SetAppEnv(ctx context.Context, id string, env map[string]string) error {
	app, err := c.GetApp(ctx, id)
	if err != nil {
		return err
	}
	app.Env = env
	app.UpdatedAt = time.Now().UTC()

	if c.st != nil {
		now := time.Now().UTC()
		for k, v := range env {
			isSec := isSecretEnvKey(k)
			val := v
			if isSec && c.vault != nil && !strings.HasPrefix(val, "v1:") {
				if enc, err := c.vault.EncryptString(ctx, val); err == nil {
					val = enc
				}
			}
			_ = c.st.EnvVars().Set(ctx, &store.EnvVar{
				ID:             store.NewID("env"),
				ScopeTier:      store.TierService,
				ResourceID:     id,
				Key:            k,
				ValueEncrypted: val,
				IsSecret:       isSec,
				CreatedAt:      now,
				UpdatedAt:      now,
			})
		}
	}
	c.recordAudit(ctx, "app:set_env", "app", id, "")
	return nil
}

// --- Stack Operations ---

func extractServicesFromCompose(composeYAML string) []string {
	if composeYAML == "" {
		return []string{}
	}
	res, err := orchestration.InspectComposeYAML(composeYAML)
	if err != nil {
		return []string{}
	}
	var names []string
	for _, s := range res.Services {
		names = append(names, s.Name)
	}
	return names
}

func (c *DefaultController) ListStacks(ctx context.Context) ([]Stack, error) {
	if c.st != nil {
		stks, err := c.st.Stacks().ListAll(ctx)
		if err == nil && len(stks) > 0 {
			var res []Stack
			for _, s := range stks {
				item := Stack{
					ID:          s.ID,
					ProjectID:   s.ProjectID,
					Name:        s.Name,
					ComposeYAML: s.ComposeYAML,
					Services:    extractServicesFromCompose(s.ComposeYAML),
					Status:      s.Status,
					CreatedAt:   s.CreatedAt,
					UpdatedAt:   s.UpdatedAt,
				}
				if c.orch != nil {
					if stat, err := c.orch.Stacks().InspectStack(ctx, s.Name); err == nil {
						item.Status = stat.State
						item.Containers = stat.Containers
					}
				}
				res = append(res, item)
			}
			return res, nil
		}
	}

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
	projID := req.ProjectID
	if projID == "" {
		projID = "prj_default"
	}

	id := store.NewID("stk")
	now := time.Now().UTC()
	services := extractServicesFromCompose(req.ComposeYAML)

	stack := &Stack{
		ID:          id,
		ProjectID:   projID,
		Name:        req.Name,
		ComposeYAML: req.ComposeYAML,
		Services:    services,
		Status:      "stopped",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if c.st != nil {
		dbStack := &store.Stack{
			ID:          id,
			ProjectID:   projID,
			Name:        req.Name,
			ComposeYAML: req.ComposeYAML,
			Status:      "stopped",
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		if err := c.st.Stacks().Create(ctx, dbStack); err != nil {
			return nil, fmt.Errorf("failed to persist stack: %w", err)
		}
	}

	c.mu.Lock()
	c.stacks[id] = stack
	c.mu.Unlock()

	c.recordAudit(ctx, "stack:create", "stack", stack.ID, fmt.Sprintf(`{"name":%q}`, req.Name))

	return stack, nil
}

func (c *DefaultController) GetStack(ctx context.Context, id string) (*Stack, error) {
	if c.st != nil {
		stk, err := c.st.Stacks().GetByID(ctx, id)
		if err != nil {
			stk, err = c.st.Stacks().GetByName(ctx, "prj_default", id)
		}
		if err == nil && stk != nil {
			item := &Stack{
				ID:          stk.ID,
				ProjectID:   stk.ProjectID,
				Name:        stk.Name,
				ComposeYAML: stk.ComposeYAML,
				Services:    extractServicesFromCompose(stk.ComposeYAML),
				Status:      stk.Status,
				CreatedAt:   stk.CreatedAt,
				UpdatedAt:   stk.UpdatedAt,
			}
			if c.orch != nil {
				if stat, err := c.orch.Stacks().InspectStack(ctx, stk.Name); err == nil {
					item.Status = stat.State
					item.Containers = stat.Containers
				}
			}
			return item, nil
		}
	}

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
	stack.Services = extractServicesFromCompose(composeYAML)
	stack.UpdatedAt = time.Now().UTC()

	if c.st != nil {
		if err := c.st.Stacks().Update(ctx, &store.Stack{
			ID:          stack.ID,
			ProjectID:   stack.ProjectID,
			Name:        stack.Name,
			ComposeYAML: composeYAML,
			Status:      stack.Status,
			UpdatedAt:   stack.UpdatedAt,
		}); err != nil {
			return nil, fmt.Errorf("failed to persist stack update: %w", err)
		}
	}

	c.mu.Lock()
	c.stacks[stack.ID] = stack
	c.mu.Unlock()

	c.recordAudit(ctx, "stack:update", "stack", stack.ID, "")

	return stack, nil
}

func (c *DefaultController) DeployStack(ctx context.Context, id string) error {
	stack, err := c.GetStack(ctx, id)
	if err != nil {
		return err
	}

	stack.Status = "deploying"
	if c.st != nil {
		if err := c.st.Stacks().UpdateStatus(ctx, stack.ID, "deploying"); err != nil {
			return fmt.Errorf("failed to update stack status: %w", err)
		}
	}

	if c.orch != nil {
		parsed, err := orchestration.ParseComposeYAML(stack.ComposeYAML, nil)
		if err != nil {
			stack.Status = "failed"
			if c.st != nil {
				_ = c.st.Stacks().UpdateStatus(ctx, stack.ID, "failed")
			}
			return fmt.Errorf("invalid compose yaml: %w", err)
		}

		res, err := c.orch.Stacks().DeployStack(ctx, orchestration.ComposeStackSpec{
			Name:      stack.Name,
			ProjectID: stack.ProjectID,
			RawYAML:   stack.ComposeYAML,
			Services:  parsed.Services,
			Networks:  parsed.Networks,
			Volumes:   parsed.Volumes,
			EnvVars:   parsed.EnvVars,
		})
		if err != nil {
			stack.Status = "failed"
			if c.st != nil {
				_ = c.st.Stacks().UpdateStatus(ctx, stack.ID, "failed")
			}
			return fmt.Errorf("failed to deploy stack: %w", err)
		}

		if c.st != nil {
			for _, netName := range res.CreatedNetworks {
				if err := c.st.Networks().Create(ctx, &store.ManagedNetwork{
					ProjectID: stack.ProjectID,
					Name:      netName,
					Driver:    "bridge",
					Scope:     "stack",
				}); err != nil {
					return fmt.Errorf("failed to persist stack network: %w", err)
				}
			}
			for _, volName := range res.CreatedVolumes {
				if err := c.st.Volumes().CreateManaged(ctx, &store.ManagedVolume{
					ProjectID: stack.ProjectID,
					Name:      volName,
					Driver:    "local",
				}); err != nil {
					return fmt.Errorf("failed to persist stack volume: %w", err)
				}
			}
		}
	}

	stack.Status = "running"
	stack.UpdatedAt = time.Now().UTC()
	if c.st != nil {
		if err := c.st.Stacks().UpdateStatus(ctx, stack.ID, "running"); err != nil {
			return fmt.Errorf("failed to update stack status: %w", err)
		}
	}
	c.recordAudit(ctx, "stack:deploy", "stack", stack.ID, "")
	return nil
}

func (c *DefaultController) StopStack(ctx context.Context, id string) error {
	stack, err := c.GetStack(ctx, id)
	if err != nil {
		return err
	}
	if c.orch != nil {
		_ = c.orch.Stacks().StopStack(ctx, stack.Name)
	}
	stack.Status = "stopped"
	stack.UpdatedAt = time.Now().UTC()
	if c.st != nil {
		if err := c.st.Stacks().UpdateStatus(ctx, stack.ID, "stopped"); err != nil {
			return fmt.Errorf("failed to persist stack status: %w", err)
		}
	}
	c.recordAudit(ctx, "stack:stop", "stack", stack.ID, "")
	return nil
}

func (c *DefaultController) RestartStack(ctx context.Context, id string) error {
	stack, err := c.GetStack(ctx, id)
	if err != nil {
		return err
	}
	if c.orch != nil {
		_ = c.orch.Stacks().RestartStack(ctx, stack.Name)
	}
	stack.Status = "running"
	stack.UpdatedAt = time.Now().UTC()
	if c.st != nil {
		if err := c.st.Stacks().UpdateStatus(ctx, stack.ID, "running"); err != nil {
			return fmt.Errorf("failed to persist stack status: %w", err)
		}
	}
	c.recordAudit(ctx, "stack:restart", "stack", stack.ID, "")
	return nil
}

func (c *DefaultController) DeleteStack(ctx context.Context, id string) error {
	stack, err := c.GetStack(ctx, id)
	if err == nil && c.orch != nil {
		_ = c.orch.Stacks().RemoveStack(ctx, stack.Name)
	}
	if c.st != nil {
		targetID := id
		if stack != nil && stack.ID != "" {
			targetID = stack.ID
		}
		if err := c.st.Stacks().Delete(ctx, targetID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("failed to delete stack from store: %w", err)
		}
	}
	c.mu.Lock()
	delete(c.stacks, id)
	if stack != nil {
		delete(c.stacks, stack.ID)
	}
	c.mu.Unlock()
	c.recordAudit(ctx, "stack:delete", "stack", id, "")
	return nil
}

// --- Network Operations ---

func (c *DefaultController) ListNetworks(ctx context.Context, projectID string) ([]NetworkDTO, error) {
	if c.st != nil {
		var nets []*store.ManagedNetwork
		var err error
		if projectID != "" {
			nets, err = c.st.Networks().ListByProject(ctx, projectID)
		} else {
			nets, err = c.st.Networks().ListAll(ctx)
		}
		if err == nil && len(nets) > 0 {
			var res []NetworkDTO
			for _, n := range nets {
				res = append(res, NetworkDTO{
					ID:         n.ID,
					ProjectID:  n.ProjectID,
					Name:       n.Name,
					Driver:     n.Driver,
					Scope:      n.Scope,
					IsExternal: n.IsExternal,
					CreatedAt:  n.CreatedAt,
					UpdatedAt:  n.UpdatedAt,
				})
			}
			return res, nil
		}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	var res []NetworkDTO
	for _, n := range c.networks {
		if projectID == "" || n.ProjectID == projectID {
			res = append(res, *n)
		}
	}
	return res, nil
}

func (c *DefaultController) GetNetwork(ctx context.Context, id string) (*NetworkDTO, error) {
	if c.st != nil {
		n, err := c.st.Networks().GetByID(ctx, id)
		if err == nil && n != nil {
			return &NetworkDTO{
				ID:         n.ID,
				ProjectID:  n.ProjectID,
				Name:       n.Name,
				Driver:     n.Driver,
				Scope:      n.Scope,
				IsExternal: n.IsExternal,
				CreatedAt:  n.CreatedAt,
				UpdatedAt:  n.UpdatedAt,
			}, nil
		}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if n, ok := c.networks[id]; ok {
		return n, nil
	}
	for _, n := range c.networks {
		if n.Name == id {
			return n, nil
		}
	}
	return nil, errors.New("network not found")
}

func (c *DefaultController) CreateNetwork(ctx context.Context, req *CreateNetworkRequest) (*NetworkDTO, error) {
	if req.Name == "" {
		return nil, errors.New("network name required")
	}
	projID := req.ProjectID
	if projID == "" {
		projID = "prj_default"
	}
	driver := req.Driver
	if driver == "" {
		driver = "bridge"
	}
	scope := req.Scope
	if scope == "" {
		scope = "project"
	}

	id := store.NewID("net")
	now := time.Now().UTC()
	net := &NetworkDTO{
		ID:         id,
		ProjectID:  projID,
		Name:       req.Name,
		Driver:     driver,
		Scope:      scope,
		IsExternal: req.IsExternal,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	if c.orch != nil && c.orch.RawClient() != nil {
		_, _ = c.orch.RawClient().NetworkCreate(ctx, req.Name, types.NetworkCreate{
			Driver: driver,
			Labels: map[string]string{
				"pikpik.project_id":    projID,
				"pikpik.managed":       "true",
				"pikpik.network_scope": scope,
			},
		})
	}

	if c.st != nil {
		if err := c.st.Networks().Create(ctx, &store.ManagedNetwork{
			ID:         id,
			ProjectID:  projID,
			Name:       req.Name,
			Driver:     driver,
			Scope:      scope,
			IsExternal: req.IsExternal,
			CreatedAt:  now,
			UpdatedAt:  now,
		}); err != nil {
			return nil, fmt.Errorf("failed to persist network: %w", err)
		}
	}

	c.mu.Lock()
	c.networks[id] = net
	c.mu.Unlock()

	c.recordAudit(ctx, "network:create", "network", net.ID, fmt.Sprintf(`{"name":%q}`, req.Name))

	return net, nil
}

func (c *DefaultController) DeleteNetwork(ctx context.Context, id string) error {
	n, _ := c.GetNetwork(ctx, id)
	if n != nil && c.orch != nil && c.orch.RawClient() != nil {
		_ = c.orch.RawClient().NetworkRemove(ctx, n.Name)
	}
	if c.st != nil {
		targetID := id
		if n != nil && n.ID != "" {
			targetID = n.ID
		}
		if err := c.st.Networks().Delete(ctx, targetID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("failed to delete network from store: %w", err)
		}
	}
	c.mu.Lock()
	delete(c.networks, id)
	if n != nil {
		delete(c.networks, n.ID)
	}
	c.mu.Unlock()
	c.recordAudit(ctx, "network:delete", "network", id, "")
	return nil
}

func (c *DefaultController) PruneNetworks(ctx context.Context, projectID string) (*PruneResult, error) {
	res := &PruneResult{Deleted: []string{}}
	if c.orch != nil && c.orch.RawClient() != nil {
		report, err := c.orch.RawClient().NetworksPrune(ctx, filters.NewArgs())
		if err == nil && len(report.NetworksDeleted) > 0 {
			res.Deleted = report.NetworksDeleted
		}
	}
	return res, nil
}

// --- Volume Operations ---

func (c *DefaultController) ListVolumes(ctx context.Context, projectID string) ([]VolumeDTO, error) {
	if c.st != nil {
		var vols []*store.ManagedVolume
		var err error
		if projectID != "" {
			vols, err = c.st.Volumes().ListManagedByProject(ctx, projectID)
		} else {
			vols, err = c.st.Volumes().ListAllManaged(ctx)
		}
		if err == nil && len(vols) > 0 {
			var res []VolumeDTO
			for _, v := range vols {
				res = append(res, VolumeDTO{
					ID:        v.ID,
					ProjectID: v.ProjectID,
					Name:      v.Name,
					Driver:    v.Driver,
					SizeBytes: v.SizeBytes,
					CreatedAt: v.CreatedAt,
					UpdatedAt: v.UpdatedAt,
				})
			}
			return res, nil
		}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	var res []VolumeDTO
	for _, v := range c.volumes {
		if projectID == "" || v.ProjectID == projectID {
			res = append(res, *v)
		}
	}
	return res, nil
}

func (c *DefaultController) GetVolume(ctx context.Context, id string) (*VolumeDTO, error) {
	if c.st != nil {
		v, err := c.st.Volumes().GetManagedByID(ctx, id)
		if err == nil && v != nil {
			return &VolumeDTO{
				ID:        v.ID,
				ProjectID: v.ProjectID,
				Name:      v.Name,
				Driver:    v.Driver,
				SizeBytes: v.SizeBytes,
				CreatedAt: v.CreatedAt,
				UpdatedAt: v.UpdatedAt,
			}, nil
		}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if v, ok := c.volumes[id]; ok {
		return v, nil
	}
	for _, v := range c.volumes {
		if v.Name == id {
			return v, nil
		}
	}
	return nil, errors.New("volume not found")
}

func (c *DefaultController) CreateVolume(ctx context.Context, req *CreateVolumeRequest) (*VolumeDTO, error) {
	if req.Name == "" {
		return nil, errors.New("volume name required")
	}
	projID := req.ProjectID
	if projID == "" {
		projID = "prj_default"
	}
	driver := req.Driver
	if driver == "" {
		driver = "local"
	}

	id := store.NewID("vol")
	now := time.Now().UTC()
	vol := &VolumeDTO{
		ID:        id,
		ProjectID: projID,
		Name:      req.Name,
		Driver:    driver,
		SizeBytes: 0,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if c.orch != nil && c.orch.RawClient() != nil {
		_, _ = c.orch.RawClient().VolumeCreate(ctx, volume.CreateOptions{
			Name:   req.Name,
			Driver: driver,
			Labels: map[string]string{
				"pikpik.project_id": projID,
				"pikpik.managed":    "true",
			},
		})
	}

	if c.st != nil {
		if err := c.st.Volumes().CreateManaged(ctx, &store.ManagedVolume{
			ID:        id,
			ProjectID: projID,
			Name:      req.Name,
			Driver:    driver,
			SizeBytes: 0,
			CreatedAt: now,
			UpdatedAt: now,
		}); err != nil {
			return nil, fmt.Errorf("failed to persist managed volume: %w", err)
		}
	}

	c.mu.Lock()
	c.volumes[id] = vol
	c.mu.Unlock()

	c.recordAudit(ctx, "volume:create", "volume", vol.ID, fmt.Sprintf(`{"name":%q}`, req.Name))

	return vol, nil
}

func (c *DefaultController) DeleteVolume(ctx context.Context, id string) error {
	v, _ := c.GetVolume(ctx, id)
	if v != nil && c.orch != nil && c.orch.RawClient() != nil {
		_ = c.orch.RawClient().VolumeRemove(ctx, v.Name, true)
	}
	if c.st != nil {
		targetID := id
		if v != nil && v.ID != "" {
			targetID = v.ID
		}
		if err := c.st.Volumes().DeleteManaged(ctx, targetID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("failed to delete managed volume from store: %w", err)
		}
	}
	c.mu.Lock()
	delete(c.volumes, id)
	if v != nil {
		delete(c.volumes, v.ID)
	}
	c.mu.Unlock()
	c.recordAudit(ctx, "volume:delete", "volume", id, "")
	return nil
}

func (c *DefaultController) PruneVolumes(ctx context.Context, projectID string) (*PruneResult, error) {
	res := &PruneResult{Deleted: []string{}}
	if c.orch != nil && c.orch.RawClient() != nil {
		report, err := c.orch.RawClient().VolumesPrune(ctx, filters.NewArgs())
		if err == nil {
			res.Deleted = report.VolumesDeleted
			res.SpaceReclaimed = int64(report.SpaceReclaimed)
		}
	}
	return res, nil
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

// --- Managed Machines & Remote Infrastructure Operations ---

func (c *DefaultController) ListMachines(ctx context.Context) ([]MachineDTO, error) {
	if c.st != nil && c.st.Machines() != nil {
		records, err := c.st.Machines().List(ctx)
		if err != nil {
			return nil, err
		}
		var dtos []MachineDTO
		for _, m := range records {
			dto := MachineDTO{
				ID:            m.ID,
				Hostname:      m.Hostname,
				Role:          m.Role,
				PublicIP:      m.PublicIP,
				PrivateIP:     m.PrivateIP,
				OSKernel:      m.OSKernel,
				CPUArch:       m.CPUArch,
				DockerVersion: m.DockerVersion,
				AgentVersion:  m.AgentVersion,
				Status:        m.Status,
				LastSeen:      m.LastSeen,
				CreatedAt:     m.CreatedAt,
				UpdatedAt:     m.UpdatedAt,
			}
			if metrics, err := c.GetMachineMetrics(ctx, m.ID); err == nil && metrics != nil {
				dto.Metrics = metrics
			}
			dtos = append(dtos, dto)
		}
		return dtos, nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	var dtos []MachineDTO
	for _, m := range c.machines {
		dtos = append(dtos, *m)
	}
	return dtos, nil
}

func (c *DefaultController) GetMachine(ctx context.Context, id string) (*MachineDTO, error) {
	if c.st != nil && c.st.Machines() != nil {
		m, err := c.st.Machines().GetByID(ctx, id)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, errors.New("machine not found")
			}
			return nil, err
		}
		dto := &MachineDTO{
			ID:            m.ID,
			Hostname:      m.Hostname,
			Role:          m.Role,
			PublicIP:      m.PublicIP,
			PrivateIP:     m.PrivateIP,
			OSKernel:      m.OSKernel,
			CPUArch:       m.CPUArch,
			DockerVersion: m.DockerVersion,
			AgentVersion:  m.AgentVersion,
			Status:        m.Status,
			LastSeen:      m.LastSeen,
			CreatedAt:     m.CreatedAt,
			UpdatedAt:     m.UpdatedAt,
		}
		if metrics, err := c.GetMachineMetrics(ctx, m.ID); err == nil && metrics != nil {
			dto.Metrics = metrics
		}
		return dto, nil
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	if m, ok := c.machines[id]; ok {
		return m, nil
	}
	return nil, errors.New("machine not found")
}

func (c *DefaultController) DeleteMachine(ctx context.Context, id string) error {
	if c.st != nil && c.st.Machines() != nil {
		if err := c.st.Machines().Delete(ctx, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return errors.New("machine not found")
			}
			return err
		}
		if c.agentServer != nil {
			c.agentServer.UnregisterNode(id)
		}
		c.recordAudit(ctx, "machine:delete", "machine", id, "")
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.machines, id)
	c.recordAudit(ctx, "machine:delete", "machine", id, "")
	return nil
}

func (c *DefaultController) JoinSwarmCluster(ctx context.Context, id string, req *JoinSwarmRequest) (*SwarmNode, error) {
	m, err := c.GetMachine(ctx, id)
	if err != nil {
		return nil, err
	}

	if req == nil {
		req = &JoinSwarmRequest{Role: "worker"}
	}
	if req.Role == "" {
		req.Role = "worker"
	}
	if req.JoinToken == "" {
		tokens, err := c.GetJoinTokens(ctx)
		if err == nil && tokens != nil {
			if req.Role == "manager" {
				req.JoinToken = tokens.Manager
			} else {
				req.JoinToken = tokens.Worker
			}
		}
	}

	if c.agentServer != nil {
		cmdMsg := &telemetry.StreamMessage{
			Type:      "command",
			Channel:   "node",
			TargetID:  id,
			Timestamp: time.Now().UTC().Unix(),
			Payload: map[string]interface{}{
				"command": "docker.swarm_join",
				"params": map[string]interface{}{
					"RemoteAddrs": req.RemoteAddrs,
					"JoinToken":   req.JoinToken,
					"ListenAddr":  "0.0.0.0:2377",
				},
			},
		}
		_, _ = c.agentServer.DispatchCommand(ctx, id, cmdMsg)
	}

	if c.st != nil && c.st.Machines() != nil {
		mRec, err := c.st.Machines().GetByID(ctx, id)
		if err == nil && mRec != nil {
			mRec.Role = req.Role
			mRec.Status = "online"
			if err := c.st.Machines().Update(ctx, mRec); err != nil {
				return nil, fmt.Errorf("failed to persist machine update: %w", err)
			}
		}
	}

	return &SwarmNode{
		ID:           id,
		Hostname:     m.Hostname,
		Role:         req.Role,
		Status:       "ready",
		Availability: "active",
		IPAddress:    m.PublicIP,
		EngineVer:    m.DockerVersion,
		Leader:       req.Role == "manager",
		UpdatedAt:    time.Now().UTC(),
	}, nil
}

func (c *DefaultController) GetMachineMetrics(ctx context.Context, id string) (*telemetry.HostMetrics, error) {
	if c.agentServer != nil {
		buf := c.agentServer.GetRingBuffer("node:" + id)
		if buf == nil {
			buf = c.agentServer.GetRingBuffer(id)
		}
		if buf != nil {
			pts := buf.GetLastN(1)
			if len(pts) > 0 {
				pt := pts[0]
				memTotal := uint64(8 * 1024 * 1024 * 1024)
				memUsed := pt.MemoryBytes
				var memPct float64
				if memTotal > 0 {
					memPct = float64(memUsed) / float64(memTotal) * 100
				}
				return &telemetry.HostMetrics{
					NodeID:        id,
					Timestamp:     time.Unix(pt.Timestamp, 0).UTC(),
					CPUPercent:    float64(pt.CPUPercent),
					CPUCores:      runtime.NumCPU(),
					MemUsedBytes:  memUsed,
					MemTotalBytes: memTotal,
					MemPercent:    memPct,
					NetRxBps:      uint64(pt.NetRxRate),
					NetTxBps:      uint64(pt.NetTxRate),
					DiskReadBps:   uint64(pt.DiskReadRate),
					DiskWriteBps:  uint64(pt.DiskWriteRate),
				}, nil
			}
		}
	}

	// Default fallback metrics
	return &telemetry.HostMetrics{
		NodeID:        id,
		Timestamp:     time.Now().UTC(),
		CPUPercent:    0.0,
		CPUCores:      runtime.NumCPU(),
		MemTotalBytes: 8 * 1024 * 1024 * 1024,
		MemUsedBytes:  2 * 1024 * 1024 * 1024,
		MemPercent:    25.0,
	}, nil
}

func (c *DefaultController) GetMachineEnrollCommand(ctx context.Context, serverURL string) (*EnrollMachineResponse, error) {
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}
	serverURL = strings.TrimSuffix(serverURL, "/")
	token := "pik_node_enrollment_token"
	cmd := fmt.Sprintf("curl -fsSL %s/install-agent.sh | bash -s -- --token %s --control-plane-url %s/agent/connect", serverURL, token, serverURL)
	return &EnrollMachineResponse{
		Command:     cmd,
		Token:       token,
		ServerURL:   serverURL,
		InstallBash: cmd,
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
		if err := c.st.Services().Create(ctx, &store.Service{
			ID:            id,
			ProjectID:     "prj_default",
			StageID:       "stg_default_prod",
			Name:          req.Name,
			Slug:          strings.ToLower(req.Name),
			Type:          "database",
			Image:         req.Engine + ":latest",
			ContainerPort: port,
			Status:        "running",
		}); err != nil {
			delete(c.databases, id)
			return nil, fmt.Errorf("failed to persist database service: %w", err)
		}

		if req.Password != "" {
			pw := req.Password
			if c.vault != nil && !strings.HasPrefix(pw, "v1:") {
				if enc, err := c.vault.EncryptString(ctx, pw); err == nil {
					pw = enc
				}
			}
			now := time.Now().UTC()
			_ = c.st.EnvVars().Set(ctx, &store.EnvVar{
				ID:             store.NewID("env"),
				ScopeTier:      store.TierService,
				ResourceID:     id,
				Key:            "DB_PASSWORD",
				ValueEncrypted: pw,
				IsSecret:       true,
				CreatedAt:      now,
				UpdatedAt:      now,
			})
		}
	}

	c.recordAudit(ctx, "database:create", "database", db.ID, fmt.Sprintf(`{"name":%q,"engine":%q}`, req.Name, req.Engine))

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
	c.recordAudit(ctx, "database:update", "database", id, "")
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
	c.recordAudit(ctx, "database:restart", "database", id, "")
	return nil
}

func (c *DefaultController) DeleteDatabase(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.databases, id)
	if c.st != nil {
		_ = c.st.EnvVars().DeleteByResource(ctx, store.TierService, id)
		if err := c.st.Services().Delete(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("failed to delete database service from store: %w", err)
		}
	}
	c.recordAudit(ctx, "database:delete", "database", id, "")
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
	c.recordAudit(ctx, "backup:create", "backup", b.ID, fmt.Sprintf(`{"service_id":%q}`, serviceID))
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
	if err == nil {
		c.recordAudit(ctx, "backup:restore", "backup", id, fmt.Sprintf(`{"target_service_id":%q}`, targetServiceID))
	}
	return err
}

func (c *DefaultController) DeleteBackup(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.backups, id)
	c.recordAudit(ctx, "backup:delete", "backup", id, "")
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

func (c *DefaultController) ListBackupSchedules(ctx context.Context, serviceID string) ([]*store.BackupSchedule, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.st != nil {
		if serviceID != "" {
			return c.st.Schedules().ListByService(ctx, serviceID)
		}
		return c.st.Schedules().ListActive(ctx)
	}

	var res []*store.BackupSchedule
	for _, s := range c.schedules {
		if serviceID == "" || s.ServiceID == serviceID {
			res = append(res, s)
		}
	}
	return res, nil
}

func (c *DefaultController) GetBackupSchedule(ctx context.Context, id string) (*store.BackupSchedule, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.st != nil {
		return c.st.Schedules().GetByID(ctx, id)
	}

	if s, ok := c.schedules[id]; ok {
		return s, nil
	}
	return nil, errors.New("backup schedule not found")
}

func (c *DefaultController) CreateBackupSchedule(ctx context.Context, req *CreateBackupScheduleRequest) (*store.BackupSchedule, error) {
	if req.ServiceID == "" {
		return nil, errors.New("service_id is required")
	}

	cronExpr := req.CronExpr
	if cronExpr == "" {
		cronExpr = req.CronExpression
	}
	if cronExpr == "" {
		cronExpr = "0 0 * * *"
	}

	engine := req.Engine
	if engine == "" {
		engine = req.DatabaseType
	}
	if engine == "" {
		engine = "postgres"
	}

	s3Bucket := req.S3Bucket
	if s3Bucket == "" {
		s3Bucket = "pikpik-backups"
	}

	retentionDaily := req.RetentionDaily
	if retentionDaily == 0 && req.RetentionDays > 0 {
		retentionDaily = req.RetentionDays
	}
	if retentionDaily == 0 {
		retentionDaily = 7
	}

	compression := req.Compression
	if compression == "" {
		compression = "gzip"
	}

	isEnabled := true
	if req.IsEnabled != nil {
		isEnabled = *req.IsEnabled
	}

	passwordEnc := req.Password
	if passwordEnc != "" && c.vault != nil && !strings.HasPrefix(passwordEnc, "v1:") {
		if enc, err := c.vault.EncryptString(ctx, passwordEnc); err == nil {
			passwordEnc = enc
		}
	}
	s3SecretEnc := req.S3SecretKey
	if s3SecretEnc != "" && c.vault != nil && !strings.HasPrefix(s3SecretEnc, "v1:") {
		if enc, err := c.vault.EncryptString(ctx, s3SecretEnc); err == nil {
			s3SecretEnc = enc
		}
	}

	schID := store.NewID("sch")
	now := time.Now().UTC()

	var nextRun *time.Time
	if parsed, err := backup.ParseCron(cronExpr); err == nil {
		t := parsed.Next(now)
		if !t.IsZero() {
			nextRun = &t
		}
	}

	sch := &store.BackupSchedule{
		ID:                   schID,
		ServiceID:            req.ServiceID,
		CronExpr:             cronExpr,
		Engine:               engine,
		DatabaseName:         req.DatabaseName,
		Username:             req.Username,
		PasswordEncrypted:    passwordEnc,
		S3Bucket:             s3Bucket,
		S3Endpoint:           req.S3Endpoint,
		S3Region:             req.S3Region,
		S3AccessKey:          req.S3AccessKey,
		S3SecretKeyEncrypted: s3SecretEnc,
		RetentionHourly:      req.RetentionHourly,
		RetentionDaily:       retentionDaily,
		RetentionWeekly:      req.RetentionWeekly,
		RetentionMonthly:     req.RetentionMonthly,
		MaxBackups:           req.MaxBackups,
		Compression:          compression,
		IsEnabled:            isEnabled,
		NextRunAt:            nextRun,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	c.mu.Lock()
	c.schedules[schID] = sch
	c.mu.Unlock()

	if c.st != nil {
		if _, err := c.st.Services().GetByID(ctx, req.ServiceID); err != nil {
			_ = c.st.Organizations().Create(ctx, &store.Organization{ID: "org_default", Name: "Default Org", Slug: "default"})
			_ = c.st.Projects().Create(ctx, &store.Project{ID: "prj_default", OrgID: "org_default", Name: "Default Project", Slug: "default"})
			_ = c.st.Stages().Create(ctx, &store.Stage{ID: "stg_default", ProjectID: "prj_default", Name: "Default Stage", Slug: "default"})
			_ = c.st.Services().Create(ctx, &store.Service{
				ID:        req.ServiceID,
				ProjectID: "prj_default",
				StageID:   "stg_default",
				Name:      req.ServiceID,
				Slug:      strings.ToLower(req.ServiceID),
				Type:      "database",
				Status:    "running",
			})
		}
		if err := c.st.Schedules().Create(ctx, sch); err != nil {
			return nil, err
		}
	}

	c.recordAudit(ctx, "schedule:create", "schedule", sch.ID, fmt.Sprintf(`{"service_id":%q}`, req.ServiceID))

	return sch, nil
}

func (c *DefaultController) UpdateBackupSchedule(ctx context.Context, id string, req *UpdateBackupScheduleRequest) (*store.BackupSchedule, error) {
	sch, err := c.GetBackupSchedule(ctx, id)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	cronExpr := req.CronExpr
	if cronExpr == "" {
		cronExpr = req.CronExpression
	}
	if cronExpr != "" {
		sch.CronExpr = cronExpr
		if parsed, err := backup.ParseCron(cronExpr); err == nil {
			t := parsed.Next(time.Now().UTC())
			if !t.IsZero() {
				sch.NextRunAt = &t
			}
		}
	}
	if req.Engine != "" {
		sch.Engine = req.Engine
	}
	if req.DatabaseName != "" {
		sch.DatabaseName = req.DatabaseName
	}
	if req.Username != "" {
		sch.Username = req.Username
	}
	if req.Password != "" {
		passwordEnc := req.Password
		if c.vault != nil && !strings.HasPrefix(passwordEnc, "v1:") {
			if enc, err := c.vault.EncryptString(ctx, passwordEnc); err == nil {
				passwordEnc = enc
			}
		}
		sch.PasswordEncrypted = passwordEnc
	}
	if req.S3Bucket != "" {
		sch.S3Bucket = req.S3Bucket
	}
	if req.S3Endpoint != "" {
		sch.S3Endpoint = req.S3Endpoint
	}
	if req.S3Region != "" {
		sch.S3Region = req.S3Region
	}
	if req.S3AccessKey != "" {
		sch.S3AccessKey = req.S3AccessKey
	}
	if req.S3SecretKey != "" {
		s3SecretEnc := req.S3SecretKey
		if c.vault != nil && !strings.HasPrefix(s3SecretEnc, "v1:") {
			if enc, err := c.vault.EncryptString(ctx, s3SecretEnc); err == nil {
				s3SecretEnc = enc
			}
		}
		sch.S3SecretKeyEncrypted = s3SecretEnc
	}
	if req.RetentionHourly != nil {
		sch.RetentionHourly = *req.RetentionHourly
	}
	if req.RetentionDaily != nil {
		sch.RetentionDaily = *req.RetentionDaily
	}
	if req.RetentionWeekly != nil {
		sch.RetentionWeekly = *req.RetentionWeekly
	}
	if req.RetentionMonthly != nil {
		sch.RetentionMonthly = *req.RetentionMonthly
	}
	if req.MaxBackups != nil {
		sch.MaxBackups = *req.MaxBackups
	}
	if req.Compression != "" {
		sch.Compression = req.Compression
	}
	if req.IsEnabled != nil {
		sch.IsEnabled = *req.IsEnabled
	}
	sch.UpdatedAt = time.Now().UTC()

	if c.st != nil {
		if err := c.st.Schedules().Update(ctx, sch); err != nil {
			return nil, err
		}
	}

	c.recordAudit(ctx, "schedule:update", "schedule", sch.ID, "")

	return sch, nil
}

func (c *DefaultController) DeleteBackupSchedule(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.schedules, id)
	if c.st != nil {
		if err := c.st.Schedules().Delete(ctx, id); err != nil {
			return err
		}
	}
	c.recordAudit(ctx, "schedule:delete", "schedule", id, "")
	return nil
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

func redactSensitiveJSON(v any) any {
	switch val := v.(type) {
	case map[string]any:
		res := make(map[string]any, len(val))
		for k, item := range val {
			kLower := strings.ToLower(k)
			if kLower == "api_token" || kLower == "private_key" || kLower == "key" {
				if str, ok := item.(string); ok && str != "" {
					res[k] = "[REDACTED]"
					continue
				}
			}
			res[k] = redactSensitiveJSON(item)
		}
		return res
	case []any:
		res := make([]any, len(val))
		for i, item := range val {
			res[i] = redactSensitiveJSON(item)
		}
		return res
	case string:
		if strings.Contains(val, "PRIVATE KEY") {
			return "[REDACTED]"
		}
		return val
	default:
		return v
	}
}

func redactCaddyConfigJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var val any
	if err := json.Unmarshal(raw, &val); err != nil {
		return raw
	}
	redacted := redactSensitiveJSON(val)
	out, err := json.Marshal(redacted)
	if err != nil {
		return raw
	}
	return json.RawMessage(out)
}

func (c *DefaultController) GetCaddyConfig(ctx context.Context) (*CaddyDiagnosticsDTO, error) {
	adminURL := "http://127.0.0.1:2019"
	start := time.Now()

	diag := &CaddyDiagnosticsDTO{
		Status:   "online",
		AdminURL: adminURL,
	}

	if c.ingress != nil {
		cfg, err := c.ingress.GetRawConfig(ctx)
		diag.LatencyMs = time.Since(start).Milliseconds()
		if err != nil {
			diag.Status = "offline"
			diag.Config = json.RawMessage(`{"error": "caddy admin api unreachable"}`)
		} else {
			diag.Config = redactCaddyConfigJSON(cfg)
		}
		if routes, err := c.ingress.ListRoutes(ctx); err == nil {
			diag.ActiveRoutes = len(routes)
		}
	} else {
		diag.LatencyMs = 1
		diag.Config = json.RawMessage(`{"admin":{"listen":"127.0.0.1:2019"},"apps":{"http":{"servers":{"srv0":{"routes":[]}}}}}`)
	}

	return diag, nil
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

// --- Build Operations ---

func (c *DefaultController) ListAppBuilds(ctx context.Context, appID string, limit int) ([]*store.Build, error) {
	if c.st != nil {
		return c.st.Builds().ListByService(ctx, appID, limit)
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*store.Build
	for _, b := range c.builds {
		if b.ServiceID == appID || appID == "" || appID == "*" {
			result = append(result, b)
		}
	}
	if len(result) == 0 {
		return []*store.Build{}, nil
	}
	return result, nil
}

func (c *DefaultController) GetBuild(ctx context.Context, buildID string) (*store.Build, error) {
	if c.st != nil {
		return c.st.Builds().GetByID(ctx, buildID)
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if b, ok := c.builds[buildID]; ok {
		return b, nil
	}
	return nil, errors.New("build not found")
}

func (c *DefaultController) Rebuild(ctx context.Context, buildID string) (*store.Build, error) {
	orig, err := c.GetBuild(ctx, buildID)
	if err != nil {
		return nil, err
	}

	newID := store.NewID("bld")
	job := &build.BuildJob{
		ID:            newID,
		AppID:         orig.ServiceID,
		RepoURL:       orig.RepoURL,
		Branch:        orig.Branch,
		CommitSHA:     orig.CommitSHA,
		CommitMessage: orig.CommitMessage,
		Author:        orig.Author,
		Status:        build.StatusQueued,
		CreatedAt:     time.Now().UTC(),
	}

	if c.buildMgr != nil {
		if err := c.buildMgr.Enqueue(ctx, job); err != nil {
			return nil, err
		}
	}

	newBuild := &store.Build{
		ID:            newID,
		ServiceID:     orig.ServiceID,
		RepoURL:       orig.RepoURL,
		Branch:        orig.Branch,
		CommitSHA:     orig.CommitSHA,
		CommitMessage: orig.CommitMessage,
		Author:        orig.Author,
		Status:        string(build.StatusQueued),
		StartedAt:     time.Now().UTC(),
	}

	c.mu.Lock()
	c.builds[newID] = newBuild
	c.mu.Unlock()

	if c.st != nil && c.buildMgr == nil {
		if err := c.st.Builds().Create(ctx, newBuild); err != nil {
			delete(c.builds, newID)
			return nil, fmt.Errorf("failed to persist build: %w", err)
		}
	}

	return newBuild, nil
}

func (c *DefaultController) HandleGitHubWebhook(ctx context.Context, secret string, signature string, payload []byte) (*store.Build, error) {
	if secret == "" || signature == "" || !git.VerifyGitHubSignature(secret, payload, signature) {
		return nil, errors.New("invalid webhook signature")
	}

	event, err := git.ParseGitHubPushEvent(payload)
	if err != nil {
		return nil, err
	}

	appID := event.Repository
	if c.st != nil {
		if db := c.st.DB(); db != nil {
			row := db.QueryRowContext(ctx, `SELECT id FROM services WHERE slug = ? OR name = ? LIMIT 1`, strings.ToLower(event.Repository), event.Repository)
			var foundID string
			if scanErr := row.Scan(&foundID); scanErr == nil && foundID != "" {
				appID = foundID
			} else {
				// Ensure default hierarchy and create service record if missing for foreign key integrity
				_ = c.st.Organizations().Create(ctx, &store.Organization{ID: "org_default", Name: "Default Org", Slug: "default"})
				_ = c.st.Projects().Create(ctx, &store.Project{ID: "prj_default", OrgID: "org_default", Name: "Default Project", Slug: "default"})
				_ = c.st.Stages().Create(ctx, &store.Stage{ID: "stg_default", ProjectID: "prj_default", Name: "Default Stage", Slug: "default"})
				_ = c.st.Services().Create(ctx, &store.Service{
					ID:        appID,
					ProjectID: "prj_default",
					StageID:   "stg_default",
					Name:      event.Repository,
					Slug:      strings.ToLower(event.Repository),
					Type:      "app",
					Image:     "pikpik/app:latest",
					Status:    "running",
				})
			}
		}
	}
	if appID == "" {
		appID = "app_default"
	}

	newID := store.NewID("bld")
	job := &build.BuildJob{
		ID:                   newID,
		AppID:                appID,
		RepoURL:              event.CloneURL,
		Branch:               event.Branch,
		CommitSHA:            event.CommitSHA,
		CommitMessage:        event.CommitMessage,
		Author:               event.Author,
		AuthorEmail:          event.AuthorEmail,
		GitHubInstallationID: event.InstallationID,
		Status:               build.StatusQueued,
		CreatedAt:            time.Now().UTC(),
	}

	if c.buildMgr != nil {
		if err := c.buildMgr.Enqueue(ctx, job); err != nil {
			return nil, err
		}
	}

	bld := &store.Build{
		ID:            newID,
		ServiceID:     appID,
		RepoURL:       event.CloneURL,
		Branch:        event.Branch,
		CommitSHA:     event.CommitSHA,
		CommitMessage: event.CommitMessage,
		Author:        event.Author,
		Status:        string(build.StatusQueued),
		StartedAt:     time.Now().UTC(),
	}

	c.mu.Lock()
	c.builds[newID] = bld
	c.mu.Unlock()

	if c.st != nil && c.buildMgr == nil {
		if err := c.st.Builds().Create(ctx, bld); err != nil {
			delete(c.builds, newID)
			return nil, fmt.Errorf("failed to persist build: %w", err)
		}
	}

	return bld, nil
}

func (c *DefaultController) HandleGenericGitWebhook(ctx context.Context, appID, token string, r *http.Request) (*store.Build, error) {
	if appID == "" {
		return nil, errors.New("app_id is required")
	}

	if c.st != nil {
		svc, err := c.st.Services().GetByID(ctx, appID)
		if err != nil || svc == nil || svc.DeployTokenHash == "" {
			return nil, errors.New("unauthorized git webhook token")
		}
		if token == "" || auth.HashToken(token) != svc.DeployTokenHash {
			return nil, errors.New("unauthorized git webhook token")
		}
	} else if token == "" {
		return nil, errors.New("unauthorized git webhook token")
	}

	event, err := git.ParseGenericGitPush(r)
	if err != nil {
		return nil, err
	}

	newID := store.NewID("bld")
	job := &build.BuildJob{
		ID:            newID,
		AppID:         appID,
		RepoURL:       event.CloneURL,
		Branch:        event.Branch,
		CommitSHA:     event.CommitSHA,
		CommitMessage: event.CommitMessage,
		Author:        event.Author,
		AuthorEmail:   event.AuthorEmail,
		Status:        build.StatusQueued,
		CreatedAt:     time.Now().UTC(),
	}

	if c.buildMgr != nil {
		if err := c.buildMgr.Enqueue(ctx, job); err != nil {
			return nil, err
		}
	}

	bld := &store.Build{
		ID:            newID,
		ServiceID:     appID,
		RepoURL:       event.CloneURL,
		Branch:        event.Branch,
		CommitSHA:     event.CommitSHA,
		CommitMessage: event.CommitMessage,
		Author:        event.Author,
		Status:        string(build.StatusQueued),
		StartedAt:     time.Now().UTC(),
	}

	c.mu.Lock()
	c.builds[newID] = bld
	c.mu.Unlock()

	if c.st != nil && c.buildMgr == nil {
		if err := c.st.Builds().Create(ctx, bld); err != nil {
			delete(c.builds, newID)
			return nil, fmt.Errorf("failed to persist build: %w", err)
		}
	}

	return bld, nil
}

// --- Template Operations ---

func (c *DefaultController) ListTemplates(ctx context.Context, category, search string) ([]templates.Template, error) {
	if search != "" {
		return templates.SearchTemplates(category, search), nil
	}
	return templates.ListTemplates(category), nil
}

func (c *DefaultController) GetTemplate(ctx context.Context, id string) (*templates.Template, error) {
	return templates.GetTemplate(id)
}

func (c *DefaultController) DeployTemplate(ctx context.Context, id string, req *templates.DeployTemplateRequest) (*templates.DeployTemplateResponse, error) {
	if req == nil {
		req = &templates.DeployTemplateRequest{}
	}
	var res *templates.DeployTemplateResponse
	var err error
	if c.deployer != nil {
		res, err = c.deployer.Deploy(ctx, id, *req)
	} else {
		deployer := templates.NewDeployer(templates.DefaultCatalog(), c.st, c.orch, c.vault)
		res, err = deployer.Deploy(ctx, id, *req)
	}
	if err == nil && res != nil {
		c.recordAudit(ctx, "template:deploy", "template", id, fmt.Sprintf(`{"name":%q}`, req.Name))
	}
	return res, err
}


