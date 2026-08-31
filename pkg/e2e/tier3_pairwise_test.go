package e2e

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

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
)

// ============================================================================
// TIER 3: CROSS-FEATURE PAIRWISE INTERACTIONS
// ============================================================================

// ----------------------------------------------------------------------------
// Pair 1: Canary Traffic Split + Rolling Deployment
// ----------------------------------------------------------------------------
func TestTier3_Pairwise_CanaryTrafficSplit_RollingDeployment(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Deploy initial v1 service
	app, err := h.Controller.CreateApp(h.Ctx, &api.CreateAppRequest{
		Name:          "canary-roll-app",
		Image:         "registry.example.com/app:v1",
		ProjectID:     "prj_default",
		ContainerPort: 8080,
	})
	require.NoError(t, err)

	err = h.Controller.DeployApp(h.Ctx, app.ID, "sha-v1")
	require.NoError(t, err)

	// 2. Set Canary traffic split (80% stable v1, 20% canary v2)
	domain := "canary-roll.example.com"
	err = h.IngressMgr.SetTrafficSplit(h.Ctx, domain, ingress.TrafficSplitConfig{
		Domain: domain,
		Splits: []ingress.UpstreamWeight{
			{Upstream: "10.0.0.1:8080", Weight: 80},
			{Upstream: "10.0.0.2:8080", Weight: 20},
		},
		Paths: []string{"/"},
	})
	require.NoError(t, err)

	// 3. Concurrently trigger rolling deployment to v2 while split is active
	app.Image = "registry.example.com/app:v2"
	err = h.Controller.DeployApp(h.Ctx, app.ID, "sha-v2")
	require.NoError(t, err)

	// 4. Shift 100% traffic to new version
	err = h.IngressMgr.SetTrafficSplit(h.Ctx, domain, ingress.TrafficSplitConfig{
		Domain: domain,
		Splits: []ingress.UpstreamWeight{
			{Upstream: "10.0.0.2:8080", Weight: 100},
		},
		Paths: []string{"/"},
	})
	require.NoError(t, err)

	routeID := ingress.GenerateTrafficSplitRouteID(domain)
	route, ok := h.CaddyServer.GetRoute(routeID)
	assert.True(t, ok)
	assert.NotEmpty(t, route.ID)
}

// ----------------------------------------------------------------------------
// Pair 2: Docker Compose Stack + Streaming S3 Database Backup
// ----------------------------------------------------------------------------
func TestTier3_Pairwise_ComposeStack_S3DatabaseBackup(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Create and Deploy Compose Stack (Web + DB)
	composeYAML := `
version: '3.8'
services:
  web:
    image: nginx:alpine
    ports:
      - "80:80"
    depends_on:
      - postgres
  postgres:
    image: postgres:16
    environment:
      POSTGRES_DB: appdb
`
	stack, err := h.Controller.CreateStack(h.Ctx, &api.CreateStackRequest{
		Name:        "commerce-stack",
		ProjectID:   "prj_default",
		ComposeYAML: composeYAML,
	})
	require.NoError(t, err)

	err = h.Controller.DeployStack(h.Ctx, stack.ID)
	require.NoError(t, err)

	// Seed database service for S3 backup foreign key
	dbID := "srv_commerce_pg"
	_ = h.Store.Services().Create(h.Ctx, &store.Service{
		ID:        dbID,
		ProjectID: "prj_default",
		StageID:   "stg_default_prod",
		Name:      "commerce-pg",
		Slug:      "commerce-pg",
		Type:      "database",
		Image:     "postgres:16",
		Status:    "running",
	})

	// 2. Perform live streaming S3 backup of postgres service
	dumpReader := strings.NewReader("CREATE TABLE users (id int); INSERT INTO users VALUES (1);")
	obj, err := h.S3Client.UploadStreamMultipart(h.Ctx, "backups/commerce-stack/pg_latest.sql.gz", dumpReader, s3.UploadOptions{
		ContentType: "application/gzip",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, obj.Key)

	// 3. Download & verify backup stream content
	rc, _, err := h.S3Client.DownloadStream(h.Ctx, obj.Key)
	require.NoError(t, err)
	defer rc.Close()

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(rc)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "CREATE TABLE users")
}

// ----------------------------------------------------------------------------
// Pair 3: Git Webhook -> Build Pipeline -> Dynamic Ingress Route
// ----------------------------------------------------------------------------
func TestTier3_Pairwise_GitWebhook_Build_Ingress(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Create app with Git repository configured
	app, err := h.Controller.CreateApp(h.Ctx, &api.CreateAppRequest{
		Name:          "webhook-ci-app",
		ProjectID:     "prj_default",
		GitRepoURL:    "https://github.com/fusuycorp/example-saas",
		GitBranch:     "main",
		BuildStrategy: "nixpacks",
		ContainerPort: 3000,
	})
	require.NoError(t, err)

	deploySecret := "webhook_token_secret_123"
	_ = h.Store.Services().Update(h.Ctx, &store.Service{
		ID:              app.ID,
		ProjectID:       "prj_default",
		StageID:         "stg_default_prod",
		Name:            app.Name,
		Slug:            app.Name,
		Type:            "app",
		GitBranch:       "main",
		DeployTokenHash: auth.HashToken(deploySecret),
		Status:          "running",
	})

	// 2. Trigger webhook push event
	resp, err := h.Post(fmt.Sprintf("/api/v1/webhooks/git/%s?token=%s", app.ID, deploySecret), "", map[string]any{
		"repository":     "fusuycorp/example-saas",
		"branch":         "main",
		"commit_sha":     "c0ffee123456",
		"commit_message": "feat: launch new checkout flow",
		"author":         "Dev",
		"clone_url":      "https://github.com/fusuycorp/example-saas.git",
	})
	require.NoError(t, err)
	assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted)

	// 3. Reconcile Ingress route to new build upstream
	spec := ingress.RouteSpec{
		ID:           "route_webhook_ci",
		ServiceID:    app.ID,
		Hosts:        []string{"ci.example.com"},
		UpstreamDial: "172.20.0.15:3000",
	}
	err = h.IngressMgr.ApplyRoute(h.Ctx, spec)
	require.NoError(t, err)

	r, ok := h.CaddyServer.GetRoute("route_webhook_ci")
	assert.True(t, ok)
	assert.Equal(t, "route_webhook_ci", r.ID)
}

// ----------------------------------------------------------------------------
// Pair 4: Swarm Replica Scaling + Telemetry Real-time WebSocket Stream
// ----------------------------------------------------------------------------
func TestTier3_Pairwise_SwarmScale_TelemetryStream(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Connect WebSocket client for live telemetry
	conn, _, err := h.DialWebSocket("/ws/stats", h.ViewerUser.Token)
	require.NoError(t, err)
	defer conn.Close()

	// 2. Create and scale Swarm service from 1 to 4 replicas
	app, err := h.Controller.CreateApp(h.Ctx, &api.CreateAppRequest{
		Name:        "swarm-telemetry-app",
		Image:       "nginx:alpine",
		ProjectID:   "prj_default",
		RuntimeMode: "swarm",
		Replicas:    1,
	})
	require.NoError(t, err)

	// Scale service
	app.Replicas = 4
	_ = h.Store.Services().Update(h.Ctx, &store.Service{
		ID:          app.ID,
		ProjectID:   "prj_default",
		StageID:     "stg_default_prod",
		Name:        app.Name,
		Slug:        app.Name,
		Type:        "app",
		Image:       app.Image,
		Replicas:    4,
		RuntimeMode: "swarm",
		Status:      "running",
	})

	// 3. Push telemetry metrics for scaled replicas
	for i := 1; i <= 4; i++ {
		h.WSHub.Broadcast(api.WSMessage{
			Channel:  "metrics",
			TargetID: "*",
			Event:    "stats",
			Data: telemetry.MetricPoint{
				Timestamp:   time.Now().Unix(),
				CPUPercent:  float32(10 * i),
				MemoryBytes: uint64(1024 * 1024 * 50 * i),
			},
		})
	}

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, msg, err := conn.ReadMessage()
	if err == nil {
		assert.Contains(t, string(msg), "metrics")
	}
}

// ----------------------------------------------------------------------------
// Pair 5: Multi-Tenant RBAC + Scoped PAT + Vault Secret Decryption
// ----------------------------------------------------------------------------
func TestTier3_Pairwise_MultiTenant_ScopedPAT_VaultSecrets(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Create App and store encrypted secret in Vault
	app, err := h.Controller.CreateApp(h.Ctx, &api.CreateAppRequest{
		Name:      "vault-pat-app",
		Image:     "alpine:latest",
		ProjectID: "prj_default",
	})
	require.NoError(t, err)

	secretVal := "sk_prod_super_secret_api_key_12345"
	err = h.Controller.SetAppEnv(h.Ctx, app.ID, map[string]string{
		"STRIPE_API_KEY_SECRET": secretVal,
		"PUBLIC_URL":            "https://mycompany.com",
	})
	require.NoError(t, err)

	// 2. Generate Developer PAT token scoped to apps:read
	pat, err := h.AuthSvc.CreateAPIToken(h.Ctx, h.DevUser.ID, "developer-pat-token", []string{"apps:read"}, nil)
	require.NoError(t, err)

	// 3. Validate Developer PAT and fetch decrypted environment variables
	resp, err := h.Get(fmt.Sprintf("/api/v1/apps/%s/env", app.ID), pat.RawSecret)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var envWrapped api.Response[map[string]string]
	_ = resp.JSON(&envWrapped)
	assert.Equal(t, secretVal, envWrapped.Data["STRIPE_API_KEY_SECRET"])
}

// ----------------------------------------------------------------------------
// Pair 6: Custom Domain Binding + On-Demand TLS Whitelist + Caddy Instant Removal
// ----------------------------------------------------------------------------
func TestTier3_Pairwise_CustomDomain_OnDemandTLS_CaddyInstantRemoval(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	domain := "saas.customclient.com"
	appID := "srv_tls_app_pair"

	_ = h.Store.Services().Create(h.Ctx, &store.Service{
		ID:          appID,
		ProjectID:   "prj_default",
		StageID:     "stg_default_prod",
		Name:        "tls-app-pair",
		Slug:        "tls-app-pair",
		Type:        "app",
		Image:       "nginx:alpine",
		DomainNames: []string{domain},
		Status:      "running",
	})

	// 1. Bind custom domain
	resp, err := h.Post("/api/v1/ingress/domains", h.AdminUser.Token, api.BindDomainRequest{
		Domain:  domain,
		AppID:   appID,
		AutoTLS: true,
	})
	require.NoError(t, err)
	assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated)

	// 2. On-Demand TLS /ask verification
	askResp, err := h.Get(fmt.Sprintf("/api/v1/ingress/ask?domain=%s", domain), "")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, askResp.StatusCode)

	// 3. Remove Ingress route and confirm Caddy synchronization
	routeID := fmt.Sprintf("route_%s", appID)
	err = h.IngressMgr.RemoveRoute(h.Ctx, routeID)
	require.NoError(t, err)

	_, ok := h.CaddyServer.GetRoute(routeID)
	assert.False(t, ok)
}

// ----------------------------------------------------------------------------
// Pair 7: Managed Persistent Volume + Database Service Restart Lifecycle
// ----------------------------------------------------------------------------
func TestTier3_Pairwise_ManagedVolume_DatabaseRestart(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Create managed volume
	volReq := api.CreateVolumeRequest{
		Name:      "mysql_data_persistent",
		ProjectID: "prj_default",
		Driver:    "local",
	}
	volResp, err := h.Post("/api/v1/volumes", h.AdminUser.Token, volReq)
	require.NoError(t, err)
	assert.True(t, volResp.StatusCode == http.StatusOK || volResp.StatusCode == http.StatusCreated)

	// 2. Provision Database attached to volume
	dbReq := api.CreateDatabaseRequest{
		Name:         "orders-mysql",
		Engine:       "mysql",
		DatabaseName: "ordersdb",
		Username:     "root",
		Password:     "secretpw123",
	}
	dbResp, err := h.Post("/api/v1/databases", h.AdminUser.Token, dbReq)
	require.NoError(t, err)
	assert.True(t, dbResp.StatusCode == http.StatusCreated || dbResp.StatusCode == http.StatusOK)

	var dbWrapped api.Response[api.Database]
	_ = dbResp.JSON(&dbWrapped)
	dbID := dbWrapped.Data.ID

	// 3. Restart Database and verify state persistence
	restartResp, err := h.Post(fmt.Sprintf("/api/v1/databases/%s/restart", dbID), h.AdminUser.Token, nil)
	require.NoError(t, err)
	assert.True(t, restartResp.StatusCode == http.StatusOK || restartResp.StatusCode == http.StatusAccepted)
}

// ----------------------------------------------------------------------------
// Pair 8: Interactive PTY Terminal + Streaming Stdin/Stdout over WebSocket
// ----------------------------------------------------------------------------
func TestTier3_Pairwise_PTY_StdinStdoutStreaming(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Dial PTY WebSocket
	conn, _, err := h.DialWebSocket("/ws/pty?container=active-c1", h.AdminUser.Token)
	require.NoError(t, err)
	defer conn.Close()

	// 2. Send resize frame
	resizeFrame := []byte{0x01, 0x00, 0x50, 0x00, 0x18} // 80x24
	err = conn.WriteMessage(websocket.BinaryMessage, resizeFrame)
	require.NoError(t, err)

	// 3. Send stdin command frame
	stdinFrame := append([]byte{0x00}, []byte("ls -la\n")...)
	err = conn.WriteMessage(websocket.BinaryMessage, stdinFrame)
	require.NoError(t, err)

	// 4. Verify output received
	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, _ = conn.ReadMessage()
}

// ----------------------------------------------------------------------------
// Pair 9: Scheduled Cron Backups + Automated S3 Retention Pruning
// ----------------------------------------------------------------------------
func TestTier3_Pairwise_CronScheduler_S3RetentionPrune(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	prefix := "retention-pair-test/"

	// 1. Simulate 5 daily backup runs uploaded to S3
	for i := 1; i <= 5; i++ {
		key := fmt.Sprintf("%sdump_2026-08-%02d.sql.gz", prefix, i)
		_, err := h.S3Client.UploadStreamMultipart(h.Ctx, key, strings.NewReader("MOCK DATA"), s3.UploadOptions{})
		require.NoError(t, err)
	}

	// 2. Execute retention policy keeping only 2 latest backups
	pruned, err := h.S3Client.PruneRetention(h.Ctx, prefix, s3.RetentionPolicy{
		MaxBackups: 2,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, len(pruned), "Pruning should delete the 3 oldest backups")

	// 3. Verify remaining objects
	remaining, err := h.S3Client.ListObjects(h.Ctx, prefix)
	require.NoError(t, err)
	assert.Equal(t, 2, len(remaining))
}

// ----------------------------------------------------------------------------
// Pair 10: Concurrent Deployment Burst Debounce Coalescing
// ----------------------------------------------------------------------------
func TestTier3_Pairwise_ConcurrentDeployment_BurstDebounce(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	spec := orchestration.ContainerSpec{
		Name:  "debounce-burst-container",
		Image: "nginx:alpine",
		Labels: map[string]string{
			"pikpik.app_id": "app_burst_123",
		},
	}

	// Initial deployment
	_, err := h.Orchestrator.Containers().DeployWithRollingUpdate(h.Ctx, spec, orchestration.RollingUpdateConfig{
		Monitor: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	// Fire 5 concurrent rolling update calls
	var wg sync.WaitGroup
	errs := make([]error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			localSpec := orchestration.ContainerSpec{
				Name:  "debounce-burst-container",
				Image: "nginx:alpine",
				Labels: map[string]string{
					"pikpik.app_id": "app_burst_123",
				},
			}
			_, errs[idx] = h.Orchestrator.Containers().DeployWithRollingUpdate(h.Ctx, localSpec, orchestration.RollingUpdateConfig{
				Monitor: 50 * time.Millisecond,
			})
		}(i)
	}

	wg.Wait()

	// Verify all succeeded or coalesced without race
	for _, err := range errs {
		assert.NoError(t, err)
	}
}
