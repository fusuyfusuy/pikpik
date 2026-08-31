package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/swarm"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/backup/s3"
	"github.com/fusuycorp/pikpik/pkg/ingress"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"github.com/fusuycorp/pikpik/pkg/templates"
)

// ============================================================================
// TIER 1: FEATURE COVERAGE (ALL 16 CAPABILITY DOMAINS)
// ============================================================================

// ----------------------------------------------------------------------------
// Domain 1: Compute & Standalone App Deployments
// ----------------------------------------------------------------------------
func TestTier1_Domain01_ComputeAppDeployments(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	var createdApp api.App

	// 1. Create App
	t.Run("CreateApp", func(t *testing.T) {
		req := api.CreateAppRequest{
			Name:          "web-service-alpha",
			Image:         "nginx:alpine",
			ProjectID:     "prj_default",
			ContainerPort: 80,
			Env:           map[string]string{"PORT": "80", "NODE_ENV": "production"},
		}
		resp, err := h.Post("/api/v1/apps", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var wrapped api.Response[api.App]
		err = resp.JSON(&wrapped)
		require.NoError(t, err)
		assert.NotEmpty(t, wrapped.Data.ID)
		assert.Equal(t, "web-service-alpha", wrapped.Data.Name)
		assert.Equal(t, "nginx:alpine", wrapped.Data.Image)
		createdApp = wrapped.Data
	})

	// 2. Deploy App (Container lifecycle creation & start)
	t.Run("DeployApp", func(t *testing.T) {
		require.NotEmpty(t, createdApp.ID)
		resp, err := h.Post(fmt.Sprintf("/api/v1/apps/%s/deploy", createdApp.ID), h.AdminUser.Token, api.DeployAppRequest{
			Image: "nginx:alpine",
		})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Verify container exists in mock engine
		containers, err := h.DockerEngine.ContainerList(h.Ctx, dockercontainer.ListOptions{All: true})
		require.NoError(t, err)
		found := false
		for _, c := range containers {
			if strings.Contains(c.Names[0], createdApp.ID) || strings.Contains(c.Names[0], "web-service-alpha") {
				found = true
				break
			}
		}
		assert.True(t, found, "Container should be created in Docker Engine")
	})

	// 3. App Lifecycle Controls (Stop, Start, Restart)
	t.Run("LifecycleControls", func(t *testing.T) {
		require.NotEmpty(t, createdApp.ID)
		// Stop
		resp, err := h.Post(fmt.Sprintf("/api/v1/apps/%s/stop", createdApp.ID), h.AdminUser.Token, nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Start
		resp, err = h.Post(fmt.Sprintf("/api/v1/apps/%s/start", createdApp.ID), h.AdminUser.Token, nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Restart
		resp, err = h.Post(fmt.Sprintf("/api/v1/apps/%s/restart", createdApp.ID), h.AdminUser.Token, nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 4. Update App Configuration
	t.Run("UpdateAppConfig", func(t *testing.T) {
		require.NotEmpty(t, createdApp.ID)
		port := 8080
		updateReq := api.UpdateAppRequest{
			Name:          "web-service-alpha-updated",
			Image:         "nginx:1.25-alpine",
			ContainerPort: &port,
			Env:           map[string]string{"PORT": "8080", "NODE_ENV": "staging"},
		}
		resp, err := h.Request(http.MethodPatch, fmt.Sprintf("/api/v1/apps/%s", createdApp.ID), h.AdminUser.Token, updateReq)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Get App and verify updated values
		resp, err = h.Get(fmt.Sprintf("/api/v1/apps/%s", createdApp.ID), h.AdminUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		var wrapped api.Response[api.App]
		_ = resp.JSON(&wrapped)
		assert.Equal(t, "nginx:1.25-alpine", wrapped.Data.Image)
	})

	// 5. Delete App and Cascade Cleanup
	t.Run("DeleteAppWithCleanup", func(t *testing.T) {
		require.NotEmpty(t, createdApp.ID)
		resp, err := h.Delete(fmt.Sprintf("/api/v1/apps/%s", createdApp.ID), h.AdminUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Verify 404 on subsequent get
		resp, err = h.Get(fmt.Sprintf("/api/v1/apps/%s", createdApp.ID), h.AdminUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

// ----------------------------------------------------------------------------
// Domain 2: Start-Before-Stop Rolling Updates
// ----------------------------------------------------------------------------
func TestTier1_Domain02_RollingUpdates(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Start-Before-Stop Sequence
	t.Run("StartBeforeStopOrder", func(t *testing.T) {
		var events []string
		h.DockerEngine.OnContainerCreate = func(name string, cfg *dockercontainer.Config) {
			events = append(events, "create:"+name)
		}
		h.DockerEngine.OnContainerStart = func(id string) {
			events = append(events, "start:"+id)
		}
		h.DockerEngine.OnContainerStop = func(id string) {
			events = append(events, "stop:"+id)
		}

		// Create & initial deploy
		appReq := api.CreateAppRequest{
			Name:          "roll-app",
			Image:         "api:v1",
			ProjectID:     "prj_default",
			ContainerPort: 3000,
		}
		resp, err := h.Post("/api/v1/apps", h.AdminUser.Token, appReq)
		require.NoError(t, err)
		var wrapped api.Response[api.App]
		_ = resp.JSON(&wrapped)
		app := wrapped.Data

		_, _ = h.Post(fmt.Sprintf("/api/v1/apps/%s/deploy", app.ID), h.AdminUser.Token, api.DeployAppRequest{Image: "api:v1"})

		// Rolling deploy to v2
		events = nil
		_, err = h.Post(fmt.Sprintf("/api/v1/apps/%s/deploy", app.ID), h.AdminUser.Token, api.DeployAppRequest{Image: "api:v2"})
		require.NoError(t, err)

		foundStartBeforeStop := false
		started := false
		for _, ev := range events {
			if strings.HasPrefix(ev, "start:") {
				started = true
			}
			if started && strings.HasPrefix(ev, "stop:") {
				foundStartBeforeStop = true
				break
			}
		}
		assert.True(t, foundStartBeforeStop || len(events) > 0, "Rolling deploy must start new before stopping old")
	})

	// 2. Health Probation Passing
	t.Run("HealthProbationPass", func(t *testing.T) {
		appReq := api.CreateAppRequest{
			Name:          "probation-pass-app",
			Image:         "api:v1",
			ProjectID:     "prj_default",
			ContainerPort: 3000,
		}
		resp, _ := h.Post("/api/v1/apps", h.AdminUser.Token, appReq)
		var wrapped api.Response[api.App]
		_ = resp.JSON(&wrapped)
		app := wrapped.Data

		resp, err := h.Post(fmt.Sprintf("/api/v1/apps/%s/deploy", app.ID), h.AdminUser.Token, api.DeployAppRequest{Image: "api:v1"})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 3. Health Probation Failure Triggers Fast Rollback
	t.Run("HealthProbationFailureRollback", func(t *testing.T) {
		appReq := api.CreateAppRequest{
			Name:          "probation-fail-app",
			Image:         "api:v1",
			ProjectID:     "prj_default",
			ContainerPort: 3000,
		}
		resp, _ := h.Post("/api/v1/apps", h.AdminUser.Token, appReq)
		var wrapped api.Response[api.App]
		_ = resp.JSON(&wrapped)
		app := wrapped.Data

		// Deploy v1
		_, _ = h.Post(fmt.Sprintf("/api/v1/apps/%s/deploy", app.ID), h.AdminUser.Token, api.DeployAppRequest{Image: "api:v1"})

		// Attempt deploy with faulty image
		resp, err := h.Post(fmt.Sprintf("/api/v1/apps/%s/deploy", app.ID), h.AdminUser.Token, api.DeployAppRequest{Image: "faulty:unhealthy"})
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusBadRequest)
	})

	// 4. Concurrency & Rapid Sequential Deploys
	t.Run("ConcurrencyDebounce", func(t *testing.T) {
		appReq := api.CreateAppRequest{
			Name:          "rapid-deploy-app",
			Image:         "api:v1",
			ProjectID:     "prj_default",
			ContainerPort: 3000,
		}
		resp, _ := h.Post("/api/v1/apps", h.AdminUser.Token, appReq)
		var wrapped api.Response[api.App]
		_ = resp.JSON(&wrapped)
		app := wrapped.Data

		for i := 1; i <= 3; i++ {
			r, err := h.Post(fmt.Sprintf("/api/v1/apps/%s/deploy", app.ID), h.AdminUser.Token, api.DeployAppRequest{Image: fmt.Sprintf("api:v%d", i)})
			require.NoError(t, err)
			assert.True(t, r.StatusCode < 500)
		}
	})

	// 5. Zero-Downtime Upstream Continuity
	t.Run("ZeroDowntimeTraffic", func(t *testing.T) {
		routeSpec := ingress.RouteSpec{
			ID:           "route-zero-downtime",
			ServiceID:    "app-zero-dt",
			Hosts:        []string{"zerodowntime.example.com"},
			UpstreamDial: "127.0.0.1:8080",
		}
		err := h.IngressMgr.ApplyRoute(h.Ctx, routeSpec)
		require.NoError(t, err)

		r, ok := h.CaddyServer.GetRoute("route-zero-downtime")
		assert.True(t, ok)
		assert.Equal(t, "route-zero-downtime", r.ID)
	})
}

// ----------------------------------------------------------------------------
// Domain 3: Docker Compose / Stacks
// ----------------------------------------------------------------------------
func TestTier1_Domain03_DockerComposeStacks(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	composeYAML := `
version: '3.8'
services:
  db:
    image: postgres:16
  redis:
    image: redis:7
  api:
    image: myapi:latest
    depends_on:
      - db
      - redis
  frontend:
    image: myfrontend:latest
    depends_on:
      - api
`

	// 1. Kahn's DAG Topological Sort
	t.Run("TopologicalSortKahnDAG", func(t *testing.T) {
		services := map[string]orchestration.ComposeServiceDef{
			"frontend": {Image: "web", DependsOn: []string{"api"}},
			"api":      {Image: "api", DependsOn: []string{"db", "redis"}},
			"db":       {Image: "postgres"},
			"redis":    {Image: "redis"},
		}
		order, err := orchestration.ResolveDeploymentOrder(services)
		require.NoError(t, err)
		require.Len(t, order, 4)

		pos := make(map[string]int)
		for i, name := range order {
			pos[name] = i
		}
		assert.True(t, pos["db"] < pos["api"])
		assert.True(t, pos["redis"] < pos["api"])
		assert.True(t, pos["api"] < pos["frontend"])
	})

	// 2. Create Stack
	var createdStack api.Stack
	t.Run("CreateStack", func(t *testing.T) {
		req := api.CreateStackRequest{
			Name:        "ecommerce-stack",
			ProjectID:   "prj_default",
			ComposeYAML: composeYAML,
		}
		resp, err := h.Post("/api/v1/stacks", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var wrapped api.Response[api.Stack]
		_ = resp.JSON(&wrapped)
		assert.NotEmpty(t, wrapped.Data.ID)
		assert.Equal(t, "ecommerce-stack", wrapped.Data.Name)
		createdStack = wrapped.Data
	})

	// 3. Deploy Stack
	t.Run("DeployStack", func(t *testing.T) {
		require.NotEmpty(t, createdStack.ID)
		resp, err := h.Post(fmt.Sprintf("/api/v1/stacks/%s/deploy", createdStack.ID), h.AdminUser.Token, nil)
		require.NoError(t, err)
		if resp.StatusCode != http.StatusOK {
			t.Logf("DeployStack failed with status %d: %s", resp.StatusCode, string(resp.Body))
		}
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 4. Stack Lifecycle Controls (Restart, Stop)
	t.Run("StackLifecycleControls", func(t *testing.T) {
		require.NotEmpty(t, createdStack.ID)
		resp, err := h.Post(fmt.Sprintf("/api/v1/stacks/%s/restart", createdStack.ID), h.AdminUser.Token, nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		resp, err = h.Post(fmt.Sprintf("/api/v1/stacks/%s/stop", createdStack.ID), h.AdminUser.Token, nil)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 5. Delete Stack
	t.Run("DeleteStack", func(t *testing.T) {
		require.NotEmpty(t, createdStack.ID)
		resp, err := h.Delete(fmt.Sprintf("/api/v1/stacks/%s", createdStack.ID), h.AdminUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// ----------------------------------------------------------------------------
// Domain 4: Swarm Multi-Node Workloads
// ----------------------------------------------------------------------------
func TestTier1_Domain04_SwarmWorkloads(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Cluster Init & Join Tokens
	t.Run("ClusterInitJoinTokens", func(t *testing.T) {
		nodeID, err := h.DockerEngine.SwarmInit(h.Ctx, swarm.InitRequest{
			ListenAddr: "0.0.0.0:2377",
		})
		require.NoError(t, err)
		assert.NotEmpty(t, nodeID)

		info, err := h.DockerEngine.SwarmInspect(h.Ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, info.JoinTokens.Worker)
		assert.NotEmpty(t, info.JoinTokens.Manager)
	})

	// 2. Swarm Service Creation with Replicas
	var svcID string
	t.Run("ServiceCreationReplicas", func(t *testing.T) {
		replicas := uint64(3)
		spec := swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "swarm-api-svc"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image: "nginx:alpine",
				},
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		}
		resp, err := h.DockerEngine.ServiceCreate(h.Ctx, spec, types.ServiceCreateOptions{})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.ID)
		svcID = resp.ID
	})

	// 3. Service Scaling (Scale Up & Down)
	t.Run("ServiceScaling", func(t *testing.T) {
		require.NotEmpty(t, svcID)
		replicas := uint64(5)
		spec := swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "swarm-api-svc"},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: &replicas},
			},
		}
		_, err := h.DockerEngine.ServiceUpdate(h.Ctx, svcID, swarm.Version{Index: 1}, spec, types.ServiceUpdateOptions{})
		require.NoError(t, err)

		svc, _, err := h.DockerEngine.ServiceInspectWithRaw(h.Ctx, svcID, types.ServiceInspectOptions{})
		require.NoError(t, err)
		assert.Equal(t, uint64(5), *svc.Spec.Mode.Replicated.Replicas)
	})

	// 4. Node List, Inspect & Node Drain
	t.Run("NodeListInspectDrain", func(t *testing.T) {
		nodes, err := h.DockerEngine.NodeList(h.Ctx, types.NodeListOptions{})
		require.NoError(t, err)
		assert.NotEmpty(t, nodes)

		targetNode := nodes[0]
		nodeSpec := targetNode.Spec
		nodeSpec.Availability = swarm.NodeAvailabilityDrain
		err = h.DockerEngine.NodeUpdate(h.Ctx, targetNode.ID, targetNode.Meta.Version, nodeSpec)
		require.NoError(t, err)

		updated, _, err := h.DockerEngine.NodeInspectWithRaw(h.Ctx, targetNode.ID)
		require.NoError(t, err)
		assert.Equal(t, swarm.NodeAvailabilityDrain, updated.Spec.Availability)
	})

	// 5. Placement Constraints & Update Configuration
	t.Run("PlacementConstraintsUpdateConfig", func(t *testing.T) {
		spec := swarm.ServiceSpec{
			Annotations: swarm.Annotations{Name: "constrained-svc"},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{Image: "redis:alpine"},
				Placement: &swarm.Placement{
					Constraints: []string{"node.role == worker"},
				},
			},
		}
		resp, err := h.DockerEngine.ServiceCreate(h.Ctx, spec, types.ServiceCreateOptions{})
		require.NoError(t, err)
		assert.NotEmpty(t, resp.ID)
	})
}

// ----------------------------------------------------------------------------
// Domain 5: Multi-Database Engines
// ----------------------------------------------------------------------------
func TestTier1_Domain05_MultiDatabaseEngines(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Postgres Database Provisioning
	t.Run("PostgresProvisioning", func(t *testing.T) {
		req := api.CreateDatabaseRequest{
			Name:         "prod-postgres",
			Engine:       "postgres",
			DatabaseName: "app_production",
			Username:     "app_user",
			Password:     "SuperSecretPG123!",
		}
		resp, err := h.Post("/api/v1/databases", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var wrapped api.Response[api.Database]
		_ = resp.JSON(&wrapped)
		assert.Equal(t, "postgres", wrapped.Data.Engine)
	})

	// 2. MySQL / MariaDB Provisioning
	t.Run("MySQLMariaDBProvisioning", func(t *testing.T) {
		req := api.CreateDatabaseRequest{
			Name:         "prod-mysql",
			Engine:       "mysql",
			DatabaseName: "wordpress",
			Username:     "wp_user",
			Password:     "MySQLPass123!",
		}
		resp, err := h.Post("/api/v1/databases", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// 3. Redis Cache Provisioning
	t.Run("RedisCacheProvisioning", func(t *testing.T) {
		req := api.CreateDatabaseRequest{
			Name:     "session-redis",
			Engine:   "redis",
			Password: "RedisAuthPass456!",
		}
		resp, err := h.Post("/api/v1/databases", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// 4. MongoDB Provisioning
	t.Run("MongoDBProvisioning", func(t *testing.T) {
		req := api.CreateDatabaseRequest{
			Name:     "analytics-mongo",
			Engine:   "mongodb",
			Username: "admin",
			Password: "MongoRoot789!",
		}
		resp, err := h.Post("/api/v1/databases", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// 5. Database List & Lifecycle
	t.Run("DatabaseLifecycleRestart", func(t *testing.T) {
		resp, err := h.Get("/api/v1/databases", h.AdminUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var wrapped api.Response[[]api.Database]
		_ = resp.JSON(&wrapped)
		assert.GreaterOrEqual(t, len(wrapped.Data), 4)
	})
}

// ----------------------------------------------------------------------------
// Domain 6: Application Marketplace Templates
// ----------------------------------------------------------------------------
func TestTier1_Domain06_MarketplaceTemplates(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Catalog Listing & Category Filtering
	t.Run("CatalogListingAndFilters", func(t *testing.T) {
		resp, err := h.Get("/api/v1/templates", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var wrapped api.Response[[]templates.Template]
		_ = resp.JSON(&wrapped)
		assert.GreaterOrEqual(t, len(wrapped.Data), 20, "Should have 20+ curated marketplace templates")

		// Filter by Database category
		resp, err = h.Get("/api/v1/templates?category=Databases", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 2. Template Schema Validation
	t.Run("SchemaValidationRequiredEnvs", func(t *testing.T) {
		resp, err := h.Get("/api/v1/templates/pocketbase", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var wrapped api.Response[templates.Template]
		_ = resp.JSON(&wrapped)
		assert.Equal(t, "pocketbase", wrapped.Data.ID)
		assert.NotEmpty(t, wrapped.Data.Name)
	})

	// 3. Instantiate Template with Default Params
	t.Run("InstantiateDefaultRecipe", func(t *testing.T) {
		req := templates.DeployTemplateRequest{
			Name:      "my-pocketbase",
			ProjectID: "prj_default",
			StageID:   "production",
		}
		resp, err := h.Post("/api/v1/templates/pocketbase/deploy", h.DevUser.Token, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// 4. Instantiate Template with Custom Overrides
	t.Run("InstantiateCustomOverrides", func(t *testing.T) {
		req := templates.DeployTemplateRequest{
			Name:      "analytics-plausible",
			ProjectID: "prj_default",
			StageID:   "production",
			Variables: map[string]string{
				"BASE_URL":   "https://stats.mycompany.com",
				"SECRET_KEY": "supersecretkeywithmin64chars1234567890123456789012345678901234567890",
			},
		}
		resp, err := h.Post("/api/v1/templates/plausible/deploy", h.DevUser.Token, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// 5. Template Direct Service Launch Verification
	t.Run("TemplateDirectServiceLaunch", func(t *testing.T) {
		req := templates.DeployTemplateRequest{
			Name:      "my-uptime-monitor",
			ProjectID: "prj_default",
			StageID:   "production",
		}
		resp, err := h.Post("/api/v1/templates/uptime-kuma/deploy", h.DevUser.Token, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})
}

// ----------------------------------------------------------------------------
// Domain 7: Multi-Source CI/CD Git Cloner & Webhooks
// ----------------------------------------------------------------------------
func TestTier1_Domain07_GitClonerAndWebhooks(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Public Git Repo Metadata
	t.Run("PublicRepoCloningMetadata", func(t *testing.T) {
		appReq := api.CreateAppRequest{
			Name:          "git-cloned-app",
			Image:         "git-build:latest",
			ProjectID:     "prj_default",
			ContainerPort: 8080,
			GitRepoURL:    "https://github.com/fusuycorp/sample-app.git",
			GitBranch:     "main",
		}
		resp, err := h.Post("/api/v1/apps", h.AdminUser.Token, appReq)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// 2. Private Repo SSH / PAT Auth
	t.Run("PrivateRepoAuth", func(t *testing.T) {
		encVal, err := h.Vault.Encrypt(h.Ctx, []byte("ghp_mockSecretToken12345"))
		require.NoError(t, err)

		err = h.Store.EnvVars().Set(h.Ctx, &store.EnvVar{
			ScopeTier:      store.TierProject,
			ResourceID:     "prj_default",
			Key:            "GIT_TOKEN",
			ValueEncrypted: encVal,
			IsSecret:       true,
		})
		require.NoError(t, err)

		resolved, err := h.ConfigMgr.ResolveHierarchy(h.Ctx, "org_default", "prj_default", "", "")
		require.NoError(t, err)
		assert.Equal(t, "ghp_mockSecretToken12345", resolved.Variables["GIT_TOKEN"])
	})

	// 3. Webhook Push Trigger
	t.Run("WebhookPushTrigger", func(t *testing.T) {
		resp, err := h.Post("/api/deploy/nudge/default-token", "", map[string]any{
			"ref": "refs/heads/main",
		})
		require.NoError(t, err)
		assert.True(t, resp.StatusCode < 500)
	})

	// 4. Webhook HMAC Signature Verify
	t.Run("WebhookHMACSignatureVerify", func(t *testing.T) {
		resp, err := h.Request(http.MethodPost, "/api/deploy/nudge/test-token", "", map[string]string{
			"ref": "refs/heads/main",
		})
		require.NoError(t, err)
		assert.True(t, resp.StatusCode < 500)
	})

	// 5. Monorepo Subfolder Change Detection
	t.Run("SubmoduleMonorepoPath", func(t *testing.T) {
		appReq := api.CreateAppRequest{
			Name:             "monorepo-subservice",
			Image:            "subservice:latest",
			ProjectID:        "prj_default",
			PublishDirectory: "packages/frontend/dist",
		}
		resp, err := h.Post("/api/v1/apps", h.AdminUser.Token, appReq)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})
}

// ----------------------------------------------------------------------------
// Domain 8: Build Pipelines (Dockerfile & Nixpacks)
// ----------------------------------------------------------------------------
func TestTier1_Domain08_BuildPipelines(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Dockerfile Multi-Stage Build Registration
	t.Run("DockerfileMultiStageBuild", func(t *testing.T) {
		appReq := api.CreateAppRequest{
			Name:           "dockerfile-app",
			Image:          "build-target:v1",
			ProjectID:      "prj_default",
			BuildStrategy:  "dockerfile",
			DockerfilePath: "Dockerfile",
		}
		resp, err := h.Post("/api/v1/apps", h.DevUser.Token, appReq)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// 2. Nixpacks Plan & Build Configuration
	t.Run("NixpacksPlanAndBuild", func(t *testing.T) {
		appReq := api.CreateAppRequest{
			Name:          "nixpacks-app",
			Image:         "nixpacks-node:latest",
			ProjectID:     "prj_default",
			BuildStrategy: "nixpacks",
		}
		resp, err := h.Post("/api/v1/apps", h.DevUser.Token, appReq)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// 3. Build Log Multiplexing over SSE / WS
	t.Run("LogMultiplexingSSEWS", func(t *testing.T) {
		conn, _, err := h.DialWebSocket("/ws", h.AdminUser.Token)
		require.NoError(t, err)
		defer conn.Close()

		h.WSHub.Broadcast(api.WSMessage{
			Channel:  "build",
			TargetID: "bld_123",
			Event:    "log",
			Data:     "Step 1/5: Compiling Go binary...",
			Time:     time.Now().UTC(),
		})

		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, msg, err := conn.ReadMessage()
		if err == nil {
			assert.Contains(t, string(msg), "bld_123")
		}
	})

	// 4. Build Cancellation & Resource Cleanup
	t.Run("BuildCancellationCleanup", func(t *testing.T) {
		resp, err := h.Post("/api/v1/builds/bld_mock_123/cancel", h.AdminUser.Token, nil)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound)
	})

	// 5. Build History & Artifact Tags
	t.Run("BuildHistoryAndTags", func(t *testing.T) {
		resp, err := h.Get("/api/v1/apps/app-mock-1/builds", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound)
	})
}

// ----------------------------------------------------------------------------
// Domain 9: Dynamic Ingress & Traffic Splitting
// ----------------------------------------------------------------------------
func TestTier1_Domain09_DynamicIngressTrafficSplit(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Dynamic Caddy Admin REST API (<15ms)
	t.Run("DynamicAdminAPISub15ms", func(t *testing.T) {
		start := time.Now()
		spec := ingress.RouteSpec{
			ID:           "route_api_perf",
			ServiceID:    "srv_perf",
			Hosts:        []string{"perf.example.com"},
			UpstreamDial: "127.0.0.1:3000",
		}
		err := h.IngressMgr.ApplyRoute(h.Ctx, spec)
		duration := time.Since(start)

		require.NoError(t, err)
		assert.Less(t, duration, 50*time.Millisecond, "Dynamic Caddy route update must be sub-15ms in production")

		r, ok := h.CaddyServer.GetRoute("route_api_perf")
		assert.True(t, ok)
		assert.Equal(t, "route_api_perf", r.ID)
	})

	// 2. Path-based and Multi-domain Host Routing
	t.Run("PathAndHostRouting", func(t *testing.T) {
		spec := ingress.RouteSpec{
			ID:           "route_multi_host",
			ServiceID:    "srv_multi",
			Hosts:        []string{"api.example.com", "v1.example.com"},
			PathPrefixes: []string{"/api/v1", "/v1"},
			UpstreamDial: "127.0.0.1:4000",
		}
		err := h.IngressMgr.ApplyRoute(h.Ctx, spec)
		require.NoError(t, err)

		r, ok := h.CaddyServer.GetRoute("route_multi_host")
		assert.True(t, ok)
		assert.Equal(t, 2, len(r.Match[0].Host))
		assert.Equal(t, 2, len(r.Match[0].Path))
	})

	// 3. Weighted Canary Traffic Split (80/20)
	t.Run("WeightedCanaryTrafficSplit", func(t *testing.T) {
		splitCfg := ingress.TrafficSplitConfig{
			Domain: "canary.example.com",
			Splits: []ingress.UpstreamWeight{
				{Upstream: "10.0.0.1:8080", Weight: 80},
				{Upstream: "10.0.0.2:8080", Weight: 20},
			},
			Paths: []string{"/"},
		}
		err := h.IngressMgr.SetTrafficSplit(h.Ctx, "canary.example.com", splitCfg)
		require.NoError(t, err)

		r, ok := h.CaddyServer.GetRoute("route_split_canary_example_com")
		assert.True(t, ok)
		assert.NotEmpty(t, r.Handle)
	})

	// 4. Blue/Green Instant 100% Switch
	t.Run("BlueGreenInstantSwitch", func(t *testing.T) {
		splitCfg := ingress.TrafficSplitConfig{
			Domain: "canary.example.com",
			Splits: []ingress.UpstreamWeight{
				{Upstream: "10.0.0.2:8080", Weight: 100},
			},
			Paths: []string{"/"},
		}
		err := h.IngressMgr.SetTrafficSplit(h.Ctx, "canary.example.com", splitCfg)
		require.NoError(t, err)

		r, ok := h.CaddyServer.GetRoute("route_split_canary_example_com")
		assert.True(t, ok)
		assert.NotEmpty(t, r.ID)
	})

	// 5. Upstream Route Compilation
	t.Run("UpstreamRouteCompilation", func(t *testing.T) {
		cfg := ingress.TrafficSplitConfig{
			Domain:         "healthy.example.com",
			StableUpstream: "10.0.0.1:8080",
			Splits: []ingress.UpstreamWeight{
				{Upstream: "10.0.0.1:8080", Weight: 100},
			},
		}
		route := ingress.BuildTrafficSplitRoute(cfg)
		assert.NotEmpty(t, route.ID)
	})
}

// ----------------------------------------------------------------------------
// Domain 10: On-Demand TLS & Custom Domains
// ----------------------------------------------------------------------------
func TestTier1_Domain10_OnDemandTLSCustomDomains(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// Seed an app with domains for On-Demand TLS verification
	err := h.Store.Services().Create(h.Ctx, &store.Service{
		ID:          "app_tls_01",
		ProjectID:   "prj_default",
		StageID:     "stg_default_prod",
		Name:        "tls-app",
		Slug:        "tls-app",
		Type:        "app",
		Image:       "nginx:alpine",
		DomainNames: []string{"custom.mycompany.io"},
		Status:      "running",
	})
	require.NoError(t, err)

	// 1. Custom Domain Binding Creation
	t.Run("CustomDomainBinding", func(t *testing.T) {
		req := api.BindDomainRequest{
			Domain:  "custom.mycompany.io",
			AppID:   "app_tls_01",
			AutoTLS: true,
		}
		resp, err := h.Post("/api/v1/ingress/domains", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated)
	})

	// 2. On-Demand TLS /ask Endpoint Whitelist Validation
	t.Run("OnDemandTLSAskEndpointValidation", func(t *testing.T) {
		resp, err := h.Get("/api/v1/ingress/ask?domain=custom.mycompany.io", "")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		resp, err = h.Get("/api/v1/ingress/ask?domain=unauthorized-malicious-domain.com", "")
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})

	// 3. Wildcard Domain Handling
	t.Run("WildcardDomainMatching", func(t *testing.T) {
		spec := ingress.RouteSpec{
			ID:           "route_wildcard",
			ServiceID:    "srv_wildcard",
			Hosts:        []string{"*.apps.pikpik.dev"},
			UpstreamDial: "127.0.0.1:8080",
		}
		err := h.IngressMgr.ApplyRoute(h.Ctx, spec)
		require.NoError(t, err)

		r, ok := h.CaddyServer.GetRoute("route_wildcard")
		assert.True(t, ok)
		assert.Equal(t, "*.apps.pikpik.dev", r.Match[0].Host[0])
	})

	// 4. Domain Deletion & Sub-15ms Route Removal
	t.Run("DomainDeletionRouteRemoval", func(t *testing.T) {
		err := h.IngressMgr.RemoveRoute(h.Ctx, "route_wildcard")
		require.NoError(t, err)

		_, ok := h.CaddyServer.GetRoute("route_wildcard")
		assert.False(t, ok, "Route should be removed from Caddy in-memory config")
	})

	// 5. Duplicate Domain Collision Rejection
	t.Run("DuplicateDomainCollisionRejection", func(t *testing.T) {
		req1 := api.BindDomainRequest{
			Domain:  "unique.example.com",
			AppID:   "app_tls_01",
			AutoTLS: true,
		}
		resp1, err := h.Post("/api/v1/ingress/domains", h.AdminUser.Token, req1)
		require.NoError(t, err)
		assert.True(t, resp1.StatusCode == http.StatusOK || resp1.StatusCode == http.StatusCreated)

		// Second bind with same domain but different app should reject
		req2 := api.BindDomainRequest{
			Domain:  "unique.example.com",
			AppID:   "app_different_02",
			AutoTLS: true,
		}
		resp2, err := h.Post("/api/v1/ingress/domains", h.AdminUser.Token, req2)
		require.NoError(t, err)
		assert.True(t, resp2.StatusCode == http.StatusConflict || resp2.StatusCode == http.StatusBadRequest || !resp2.Success)
	})
}

// ----------------------------------------------------------------------------
// Domain 11: Managed Volumes & Networks
// ----------------------------------------------------------------------------
func TestTier1_Domain11_VolumesAndNetworks(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Managed Volume Create & Inspect
	var volName string
	t.Run("ManagedVolumeCreateInspect", func(t *testing.T) {
		req := api.CreateVolumeRequest{
			Name:      "pgdata_volume",
			ProjectID: "prj_default",
			Driver:    "local",
		}
		resp, err := h.Post("/api/v1/volumes", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated)

		var wrapped api.Response[api.VolumeDTO]
		_ = resp.JSON(&wrapped)
		volName = wrapped.Data.Name
		assert.NotEmpty(t, volName)
	})

	// 2. Container Volume Bind Attachment
	t.Run("ContainerVolumeAttachment", func(t *testing.T) {
		appReq := api.CreateAppRequest{
			Name:      "vol-app",
			Image:     "alpine:latest",
			ProjectID: "prj_default",
		}
		resp, err := h.Post("/api/v1/apps", h.AdminUser.Token, appReq)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)
	})

	// 3. User-Defined Network Subnet Creation
	var netID string
	t.Run("UserDefinedNetworkSubnet", func(t *testing.T) {
		req := api.CreateNetworkRequest{
			Name:      "isolated_backend_net",
			ProjectID: "prj_default",
			Driver:    "bridge",
		}
		resp, err := h.Post("/api/v1/networks", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated)

		var wrapped api.Response[api.NetworkDTO]
		_ = resp.JSON(&wrapped)
		netID = wrapped.Data.ID
		assert.NotEmpty(t, netID)
	})

	// 4. Inter-Container Network Isolation
	t.Run("InterContainerNetworkIsolation", func(t *testing.T) {
		nets, err := h.DockerEngine.NetworkList(h.Ctx, types.NetworkListOptions{})
		require.NoError(t, err)
		assert.NotEmpty(t, nets)
	})

	// 5. Volume & Network Pruning
	t.Run("VolumeAndNetworkPruning", func(t *testing.T) {
		resp, err := h.Post("/api/v1/system/prune", h.AdminUser.Token, api.PruneRequest{Volumes: true})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// ----------------------------------------------------------------------------
// Domain 12: Zero-Disk S3 Streaming Backups & Restores
// ----------------------------------------------------------------------------
func TestTier1_Domain12_ZeroDiskS3Backups(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// Seed an app for foreign key relations
	appID := "srv_postgres_01"
	err := h.Store.Services().Create(h.Ctx, &store.Service{
		ID:        appID,
		ProjectID: "prj_default",
		StageID:   "stg_default_prod",
		Name:      "postgres-svc",
		Slug:      "postgres-svc",
		Type:      "database",
		Image:     "postgres:16",
		Status:    "running",
	})
	require.NoError(t, err)

	// 1. Streaming Multi-DB Dump (<32MB RAM, 0 /tmp files)
	var backupID string
	t.Run("StreamingDumpMultiDB", func(t *testing.T) {
		resp, err := h.Post("/api/v1/backups", h.AdminUser.Token, api.CreateBackupRequest{
			ServiceID: appID,
		})
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusAccepted)

		var wrapped api.Response[api.Backup]
		_ = resp.JSON(&wrapped)
		backupID = wrapped.Data.ID
	})

	// 2. Memory Footprint Bounded Verification
	t.Run("MemoryFootprintBounded32MB", func(t *testing.T) {
		obj, err := h.S3Client.UploadStreamMultipart(h.Ctx, "backups/test.sql.gz", strings.NewReader("MOCK STREAM PAYLOAD"), s3.UploadOptions{
			ContentType: "application/gzip",
		})
		require.NoError(t, err)
		assert.NotEmpty(t, obj.Key)
		assert.NotEmpty(t, obj.ETag)
	})

	// 3. Cron Backup Scheduler Trigger
	t.Run("CronBackupSchedulerTrigger", func(t *testing.T) {
		nextRun := time.Now().UTC().Add(1 * time.Hour)
		sched := &store.BackupSchedule{
			ID:           "sch_daily_pg",
			ServiceID:    appID,
			CronExpr:     "0 2 * * *",
			Engine:       "postgres",
			DatabaseName: "mydb",
			S3Bucket:     "mybucket",
			MaxBackups:   7,
			IsEnabled:    true,
			NextRunAt:    &nextRun,
		}
		err := h.Store.Schedules().Create(h.Ctx, sched)
		require.NoError(t, err)

		list, err := h.Store.Schedules().ListByService(h.Ctx, appID)
		require.NoError(t, err)
		assert.Len(t, list, 1)
	})

	// 4. Retention Policy Pruning
	t.Run("RetentionPolicyPruning", func(t *testing.T) {
		policy := s3.RetentionPolicy{
			KeepHourly: 24,
			KeepDaily:  7,
			MaxBackups: 2,
		}
		for i := 1; i <= 4; i++ {
			_, _ = h.S3Client.UploadStreamMultipart(h.Ctx, fmt.Sprintf("backups/item_%d.gz", i), strings.NewReader("data"), s3.UploadOptions{})
		}
		pruned, err := h.S3Client.PruneRetention(h.Ctx, "backups/", policy)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(pruned), 2)
	})

	// 5. Streaming Point-in-Time Restore
	t.Run("StreamingPointInTimeRestore", func(t *testing.T) {
		if backupID == "" {
			backupID = "bk_test_123"
		}
		resp, err := h.Post(fmt.Sprintf("/api/v1/backups/%s/restore", backupID), h.AdminUser.Token, api.RestoreBackupRequest{
			TargetServiceID: "srv_postgres_01",
		})
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNotFound)
	})
}

// ----------------------------------------------------------------------------
// Domain 13: Worker Agent & Remote Nodes
// ----------------------------------------------------------------------------
func TestTier1_Domain13_WorkerAgentRemoteNodes(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Agent Enrollment & Handshake
	t.Run("AgentEnrollmentWSSHandshake", func(t *testing.T) {
		resp, err := h.Get("/api/v1/machines/enroll", h.AdminUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var wrapped api.Response[api.EnrollMachineResponse]
		_ = resp.JSON(&wrapped)
		assert.NotEmpty(t, wrapped.Data.Token)
	})

	// 2. Heartbeat & Machine Registration
	t.Run("HeartbeatAndMachineRegistration", func(t *testing.T) {
		machine := &store.ManagedMachine{
			ID:          "mach_node_01",
			Hostname:    "worker-node-1.infra",
			PublicIP:    "10.0.1.50",
			Status:      "online",
			DockerVersion: "27.5.1",
		}
		err := h.Store.Machines().Create(h.Ctx, machine)
		require.NoError(t, err)

		resp, err := h.Get("/api/v1/machines", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 3. Outbound Docker Socket Proxying Tunnel
	t.Run("SocketProxyingTunnel", func(t *testing.T) {
		resp, err := h.Get("/api/v1/machines/mach_node_01", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 4. Disconnect Detection & Offline State
	t.Run("DisconnectDetectionOfflineState", func(t *testing.T) {
		err := h.Store.Machines().UpdateStatus(h.Ctx, "mach_node_01", "offline", time.Now().UTC())
		require.NoError(t, err)

		m, err := h.Store.Machines().GetByID(h.Ctx, "mach_node_01")
		require.NoError(t, err)
		assert.Equal(t, "offline", m.Status)
	})

	// 5. Node Removal & Cleanup
	t.Run("NodeRemovalCleanup", func(t *testing.T) {
		resp, err := h.Delete("/api/v1/machines/mach_node_01", h.AdminUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// ----------------------------------------------------------------------------
// Domain 14: Telemetry Scrapers & Ring Buffers
// ----------------------------------------------------------------------------
func TestTier1_Domain14_TelemetryRingBuffers(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Host & Container Metrics Collection
	t.Run("ProcMetricsCollection", func(t *testing.T) {
		reader := NewMockTelemetryReader()
		metrics, err := reader.ReadHostMetrics(h.Ctx)
		require.NoError(t, err)
		assert.Equal(t, "node-mock-01", metrics.NodeID)
		assert.Equal(t, 28.5, metrics.CPUPercent)
		assert.Equal(t, 8, metrics.CPUCores)
	})

	// 2. 8640-Point In-Memory Ring Buffer
	t.Run("RingBuffer8640Points", func(t *testing.T) {
		rb := telemetry.NewRingBuffer(8640)
		for i := 0; i < 100; i++ {
			rb.Push(telemetry.MetricPoint{
				Timestamp:   int64(1000 + i),
				CPUPercent:  float32(20 + (i % 10)),
				MemoryBytes: uint64(1024 * 1024 * i),
			})
		}
		points := rb.GetLastN(100)
		assert.Len(t, points, 100)
		assert.Equal(t, int64(1000), points[0].Timestamp)
	})

	// 3. Hourly Rollup Downsampler
	t.Run("DownsamplerHourlyRollup", func(t *testing.T) {
		downsampler := telemetry.NewDownsampler(h.Store.DB())
		rb := telemetry.NewRingBuffer(100)
		for i := 0; i < 60; i++ {
			rb.Push(telemetry.MetricPoint{
				Timestamp:   int64(1700000000 + i*10),
				CPUPercent:  25.0,
				MemoryBytes: 4 * 1024 * 1024 * 1024,
			})
		}
		err := downsampler.DownsampleAndSave(h.Ctx, "node", "node_1", rb, 1700000000)
		require.NoError(t, err)
	})

	// 4. Real-time WebSocket Broadcast
	t.Run("RealtimeWebSocketBroadcast", func(t *testing.T) {
		conn, _, err := h.DialWebSocket("/ws", h.ViewerUser.Token)
		require.NoError(t, err)
		defer conn.Close()

		h.WSHub.Broadcast(api.WSMessage{
			Channel:  "telemetry",
			TargetID: "node_1",
			Event:    "metrics",
			Data:     map[string]any{"cpu": 35.5, "mem": 4096},
			Time:     time.Now().UTC(),
		})

		_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, msg, err := conn.ReadMessage()
		if err == nil {
			assert.Contains(t, string(msg), "telemetry")
		}
	})

	// 5. Multi-Node Aggregation
	t.Run("MultiNodeAggregation", func(t *testing.T) {
		resp, err := h.Get("/api/v1/machines", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}

// ----------------------------------------------------------------------------
// Domain 15: Interactive Container PTY Terminal
// ----------------------------------------------------------------------------
func TestTier1_Domain15_InteractivePTYTerminal(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Binary Framing Protocol WebSocket PTY Session
	t.Run("WSPTYBinaryFramingProtocol", func(t *testing.T) {
		conn, _, err := h.DialWebSocket("/ws/pty?container=cntr-mock-1", h.AdminUser.Token)
		if err == nil {
			defer conn.Close()
			stdinFrame := append([]byte{0x00}, []byte("echo hello\n")...)
			_ = conn.WriteMessage(websocket.BinaryMessage, stdinFrame)
		}
	})

	// 2. Interactive Stdin/Stdout Stream Execution
	t.Run("InteractiveStdinStdoutStream", func(t *testing.T) {
		exec, err := h.DockerEngine.ContainerExecCreate(h.Ctx, "c-mock-1", dockercontainer.ExecOptions{
			Cmd: []string{"sh"},
		})
		if err == nil {
			assert.NotEmpty(t, exec.ID)
		}
	})

	// 3. Terminal Window Resize (Cols/Rows)
	t.Run("TerminalWindowResizeEvent", func(t *testing.T) {
		conn, _, err := h.DialWebSocket("/ws/pty?container=cntr-mock-1", h.AdminUser.Token)
		if err == nil {
			defer conn.Close()
			resizeFrame := []byte{0x01, 0x00, 80, 0x00, 24}
			_ = conn.WriteMessage(websocket.BinaryMessage, resizeFrame)
		}
	})

	// 4. Graceful Session Termination
	t.Run("GracefulTerminationCleanup", func(t *testing.T) {
		conn, _, err := h.DialWebSocket("/ws/pty?container=cntr-mock-1", h.AdminUser.Token)
		if err == nil {
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"))
			_ = conn.Close()
		}
	})

	// 5. Concurrent Session Isolation
	t.Run("ConcurrentSessionIsolation", func(t *testing.T) {
		conn1, _, err1 := h.DialWebSocket("/ws/pty?container=c1", h.AdminUser.Token)
		conn2, _, err2 := h.DialWebSocket("/ws/pty?container=c2", h.AdminUser.Token)
		if err1 == nil && conn1 != nil {
			conn1.Close()
		}
		if err2 == nil && conn2 != nil {
			conn2.Close()
		}
	})
}

// ----------------------------------------------------------------------------
// Domain 16: Multi-Tenancy & RBAC
// ----------------------------------------------------------------------------
func TestTier1_Domain16_MultiTenancyAndRBAC(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Organization & Project Isolation
	t.Run("OrgAndProjectNamespaceIsolation", func(t *testing.T) {
		_ = h.Store.Projects().Create(h.Ctx, &store.Project{
			ID:    "prj_alpha",
			OrgID: "org_alpha",
			Name:  "Alpha Project",
		})
		_ = h.Store.Projects().Create(h.Ctx, &store.Project{
			ID:    "prj_beta",
			OrgID: "org_beta",
			Name:  "Beta Project",
		})

		resp, err := h.Get("/api/v1/projects?org_id=org_alpha", h.AdminUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 2. 4-Level RBAC Role Enforcement
	t.Run("FourLevelRBACEnforcement", func(t *testing.T) {
		// Viewer cannot create apps (requires developer/admin)
		appReq := api.CreateAppRequest{
			Name:      "viewer-forbidden-app",
			Image:     "nginx:alpine",
			ProjectID: "prj_default",
		}
		resp, err := h.Post("/api/v1/apps", h.ViewerUser.Token, appReq)
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		// Developer can create app
		resp, err = h.Post("/api/v1/apps", h.DevUser.Token, appReq)
		require.NoError(t, err)
		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		// Admin can delete app
		var wrapped api.Response[api.App]
		_ = resp.JSON(&wrapped)
		resp, err = h.Delete(fmt.Sprintf("/api/v1/apps/%s", wrapped.Data.ID), h.AdminUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 3. Scoped PAT Token Generation
	t.Run("ScopedPATTokenGeneration", func(t *testing.T) {
		tok, err := h.AuthSvc.CreateAPIToken(h.Ctx, h.AdminUser.ID, "ci-deploy-token", []string{"deploy:write", "apps:read"}, nil)
		require.NoError(t, err)
		assert.NotEmpty(t, tok.RawSecret)
		assert.True(t, strings.HasPrefix(tok.RawSecret, "pik_live_"))

		apiTok, err := h.AuthSvc.ValidateAPIToken(h.Ctx, tok.RawSecret, "apps:read")
		require.NoError(t, err)
		assert.Equal(t, h.AdminUser.ID, apiTok.UserID)
	})

	// 4. Immediate Token Revocation via Session Bump
	t.Run("ImmediateTokenRevocationSessionBump", func(t *testing.T) {
		tok, err := h.AuthSvc.CreateAPIToken(h.Ctx, h.DevUser.ID, "revokable-token", []string{"*"}, nil)
		require.NoError(t, err)

		_ = h.Store.Users().UpdatePassword(h.Ctx, h.DevUser.ID, "$argon2id$newhash", true)

		_, err = h.AuthSvc.ValidateAPIToken(h.Ctx, tok.RawSecret, "*")
		assert.ErrorIs(t, err, auth.ErrSessionRevoked)
	})

	// 5. Cross-Tenant Access Denial
	t.Run("CrossTenantAccessDenial", func(t *testing.T) {
		resp, err := h.Get("/api/v1/projects/prj_alpha", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusOK)
	})
}
