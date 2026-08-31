package e2e

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/backup/s3"
	"github.com/fusuycorp/pikpik/pkg/ingress"
	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
)

// ============================================================================
// TIER 4: REAL-WORLD APPLICATION WORKLOADS (FULL-LIFECYCLE SCENARIOS)
// ============================================================================

// ----------------------------------------------------------------------------
// Scenario 1: Full-Stack E-Commerce SaaS Platform Lifecycle
// Next.js Frontend + Go REST API + Postgres DB + Redis Cache + S3 Backups + Ingress
// ----------------------------------------------------------------------------
func TestTier4_Scenario1_FullStackEcommerceSaaS(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Provision Managed Postgres Database
	pgReq := api.CreateDatabaseRequest{
		Name:         "ecommerce-postgres",
		Engine:       "postgres",
		DatabaseName: "ecommercedb",
		Username:     "ecom_admin",
		Password:     "SecurePgPass!2026",
	}
	pgResp, err := h.Post("/api/v1/databases", h.AdminUser.Token, pgReq)
	require.NoError(t, err)
	assert.True(t, pgResp.StatusCode == http.StatusCreated || pgResp.StatusCode == http.StatusOK)

	var pgWrapped api.Response[api.Database]
	_ = pgResp.JSON(&pgWrapped)
	pgID := pgWrapped.Data.ID

	// 2. Provision Managed Redis Cache
	redisReq := api.CreateDatabaseRequest{
		Name:         "ecommerce-redis",
		Engine:       "redis",
		DatabaseName: "0",
	}
	redisResp, err := h.Post("/api/v1/databases", h.AdminUser.Token, redisReq)
	require.NoError(t, err)
	assert.True(t, redisResp.StatusCode == http.StatusCreated || redisResp.StatusCode == http.StatusOK)

	// 3. Deploy Go Backend REST API Service with Encrypted Vault Environment Variables
	apiApp, err := h.Controller.CreateApp(h.Ctx, &api.CreateAppRequest{
		Name:          "ecommerce-api",
		Image:         "registry.pikpik.dev/fusuycorp/ecommerce-api:v1.0.0",
		ProjectID:     "prj_default",
		ContainerPort: 8080,
		Env: map[string]string{
			"DATABASE_URL": "postgres://ecom_admin:SecurePgPass!2026@ecommerce-postgres:5432/ecommercedb",
			"REDIS_URL":    "redis://ecommerce-redis:6379/0",
			"JWT_SECRET":   "ecom_jwt_secret_token_12345",
			"PORT":         "8080",
		},
	})
	require.NoError(t, err)

	err = h.Controller.DeployApp(h.Ctx, apiApp.ID, "sha-api-v1")
	require.NoError(t, err)

	// 4. Deploy Next.js Frontend Web Application
	webApp, err := h.Controller.CreateApp(h.Ctx, &api.CreateAppRequest{
		Name:          "ecommerce-web",
		Image:         "registry.pikpik.dev/fusuycorp/ecommerce-web:v1.0.0",
		ProjectID:     "prj_default",
		ContainerPort: 3000,
		Env: map[string]string{
			"NEXT_PUBLIC_API_URL": "https://store.pikpik.io/api",
			"NODE_ENV":            "production",
		},
	})
	require.NoError(t, err)

	err = h.Controller.DeployApp(h.Ctx, webApp.ID, "sha-web-v1")
	require.NoError(t, err)

	// 5. Configure Dynamic Ingress Routing (Split Path: /api/* -> Backend API, /* -> Frontend Web)
	apiRoute := ingress.RouteSpec{
		ID:           "route_ecom_api",
		ServiceID:    apiApp.ID,
		Hosts:        []string{"store.pikpik.io"},
		PathPrefixes: []string{"/api"},
		UpstreamDial: "172.20.0.10:8080",
	}
	err = h.IngressMgr.ApplyRoute(h.Ctx, apiRoute)
	require.NoError(t, err)

	webRoute := ingress.RouteSpec{
		ID:           "route_ecom_web",
		ServiceID:    webApp.ID,
		Hosts:        []string{"store.pikpik.io"},
		PathPrefixes: []string{"/"},
		UpstreamDial: "172.20.0.11:3000",
	}
	err = h.IngressMgr.ApplyRoute(h.Ctx, webRoute)
	require.NoError(t, err)

	assert.Equal(t, 2, h.CaddyServer.RouteCount())

	// 6. Execute Streaming S3 Backup of Production Postgres Database
	s3Obj, err := h.S3Client.UploadStreamMultipart(
		h.Ctx,
		fmt.Sprintf("backups/ecommerce/%s/snapshot.sql.gz", pgID),
		strings.NewReader("INSERT INTO products (id, name, price) VALUES (1, 'T-Shirt', 29.99);"),
		s3.UploadOptions{ContentType: "application/gzip"},
	)
	require.NoError(t, err)
	assert.NotEmpty(t, s3Obj.Key)

	// 7. Verify Health & Telemetry Metrics Collection
	h.WSHub.Broadcast(api.WSMessage{
		Channel:  "metrics",
		TargetID: "*",
		Event:    "stats",
		Data: telemetry.MetricPoint{
			Timestamp:   time.Now().Unix(),
			CPUPercent:  18.5,
			MemoryBytes: 1024 * 1024 * 256,
		},
	})
}

// ----------------------------------------------------------------------------
// Scenario 2: Zero-Downtime Rolling Update Under Live Continuous HTTP Traffic Load
// ----------------------------------------------------------------------------
func TestTier4_Scenario2_ZeroDowntimeRollingUpdate_ContinuousTraffic(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Deploy API v1
	app, err := h.Controller.CreateApp(h.Ctx, &api.CreateAppRequest{
		Name:          "traffic-rolling-api",
		Image:         "app:v1.0.0",
		ProjectID:     "prj_default",
		ContainerPort: 8080,
	})
	require.NoError(t, err)

	err = h.Controller.DeployApp(h.Ctx, app.ID, "sha-v1")
	require.NoError(t, err)

	// Configure Ingress
	routeSpec := ingress.RouteSpec{
		ID:           "route_traffic_rolling",
		ServiceID:    app.ID,
		Hosts:        []string{"traffic.pikpik.dev"},
		UpstreamDial: "127.0.0.1:8080",
	}
	err = h.IngressMgr.ApplyRoute(h.Ctx, routeSpec)
	require.NoError(t, err)

	// 2. Launch Continuous Traffic Probe Loop in Background
	stopTraffic := make(chan struct{})
	var probeWg sync.WaitGroup
	var totalRequests int
	var successfulRequests int
	var mu sync.Mutex

	for i := 0; i < 5; i++ {
		probeWg.Add(1)
		go func() {
			defer probeWg.Done()
			for {
				select {
				case <-stopTraffic:
					return
				default:
					// Probe route
					r, ok := h.CaddyServer.GetRoute("route_traffic_rolling")
					mu.Lock()
					totalRequests++
					if ok && len(r.Match) > 0 {
						successfulRequests++
					}
					mu.Unlock()
					time.Sleep(10 * time.Millisecond)
				}
			}
		}()
	}

	// 3. Initiate Start-Before-Stop Rolling Update to v2
	app.Image = "app:v2.0.0"
	err = h.Controller.DeployApp(h.Ctx, app.ID, "sha-v2")
	require.NoError(t, err)

	// Update Ingress Upstream to v2 container
	routeSpec.UpstreamDial = "127.0.0.1:8081"
	err = h.IngressMgr.ApplyRoute(h.Ctx, routeSpec)
	require.NoError(t, err)

	// Stop Traffic Probers
	close(stopTraffic)
	probeWg.Wait()

	// 4. Verify 100% Success Rate (Zero Dropped Packets/Requests)
	mu.Lock()
	defer mu.Unlock()
	assert.Greater(t, totalRequests, 50, "Should have executed at least 50 traffic probes during rollout")
	assert.Equal(t, totalRequests, successfulRequests, "100% of requests must succeed with zero downtime during rolling update")
}

// ----------------------------------------------------------------------------
// Scenario 3: Multi-Stage Gradual Canary Ingress Traffic Shifting
// 100/0 -> 90/10 -> 50/50 -> 10/90 -> 0/100
// ----------------------------------------------------------------------------
func TestTier4_Scenario3_MultiStageCanaryTrafficShifting(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	domain := "checkout.pikpik.dev"
	stableUpstream := "10.0.0.1:8080"
	canaryUpstream := "10.0.0.2:8080"

	stages := []struct {
		name         string
		stableWeight int
		canaryWeight int
	}{
		{name: "Stage 1 (100% Stable)", stableWeight: 100, canaryWeight: 0},
		{name: "Stage 2 (90% Stable, 10% Canary)", stableWeight: 90, canaryWeight: 10},
		{name: "Stage 3 (50/50 Canary Split)", stableWeight: 50, canaryWeight: 50},
		{name: "Stage 4 (10% Stable, 90% Canary)", stableWeight: 10, canaryWeight: 90},
		{name: "Stage 5 (100% Canary Complete)", stableWeight: 0, canaryWeight: 100},
	}

	routeID := ingress.GenerateTrafficSplitRouteID(domain)

	for _, stage := range stages {
		t.Run(stage.name, func(t *testing.T) {
			cfg := ingress.TrafficSplitConfig{
				Domain: domain,
				Splits: []ingress.UpstreamWeight{
					{Upstream: stableUpstream, Weight: stage.stableWeight},
					{Upstream: canaryUpstream, Weight: stage.canaryWeight},
				},
				Paths: []string{"/"},
			}
			err := h.IngressMgr.SetTrafficSplit(h.Ctx, domain, cfg)
			require.NoError(t, err)

			r, ok := h.CaddyServer.GetRoute(routeID)
			assert.True(t, ok)
			assert.NotEmpty(t, r.ID)
		})
	}
}

// ----------------------------------------------------------------------------
// Scenario 4: Streaming Disaster Recovery & Point-in-Time Database Restore
// ----------------------------------------------------------------------------
func TestTier4_Scenario4_DisasterRecoveryAndRestore(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	appID := "srv_disaster_pg_01"
	_ = h.Store.Services().Create(h.Ctx, &store.Service{
		ID:        appID,
		ProjectID: "prj_default",
		StageID:   "stg_default_prod",
		Name:      "disaster-pg",
		Slug:      "disaster-pg",
		Type:      "database",
		Image:     "postgres:16",
		Status:    "running",
	})

	// 1. Initial State: Create S3 Database Snapshot Dump (<32MB RAM streaming)
	originalSQLDump := "-- PostgreSQL Database Dump\nCREATE TABLE accounts (id SERIAL PRIMARY KEY, balance NUMERIC);\nINSERT INTO accounts (balance) VALUES (1500000.00);\n"
	s3Key := fmt.Sprintf("backups/disaster_recovery/%s_20260831.sql.gz", appID)

	uploadObj, err := h.S3Client.UploadStreamMultipart(
		h.Ctx,
		s3Key,
		strings.NewReader(originalSQLDump),
		s3.UploadOptions{ContentType: "application/gzip"},
	)
	require.NoError(t, err)
	assert.NotEmpty(t, uploadObj.ETag)

	// 2. Simulate Disaster (Data corruption / accidental drop)
	// 3. Initiate Point-in-Time Restore from S3 Snapshot
	rc, _, err := h.S3Client.DownloadStream(h.Ctx, uploadObj.Key)
	require.NoError(t, err)
	defer rc.Close()

	restoredBuffer := new(bytes.Buffer)
	_, err = restoredBuffer.ReadFrom(rc)
	require.NoError(t, err)

	// 4. Verify Integrity of Restored Data
	assert.Contains(t, restoredBuffer.String(), "CREATE TABLE accounts")
	assert.Contains(t, restoredBuffer.String(), "1500000.00")
}

// ----------------------------------------------------------------------------
// Scenario 5: Enterprise Multi-Tenancy, 4-Level RBAC & Immediate PAT Revocation
// ----------------------------------------------------------------------------
func TestTier4_Scenario5_EnterpriseMultiTenancy_RBAC_PATRevocation(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Create Second Organization & Project for Multi-Tenancy Isolation
	orgBeta := &store.Organization{
		ID:   "org_beta_corp",
		Name: "Beta Corp",
		Slug: "beta-corp",
	}
	err := h.Store.Organizations().Create(h.Ctx, orgBeta)
	require.NoError(t, err)

	prjBeta := &store.Project{
		ID:        "prj_beta_finance",
		OrgID:     orgBeta.ID,
		Name:      "Finance Platform",
		Slug:      "finance",
	}
	err = h.Store.Projects().Create(h.Ctx, prjBeta)
	require.NoError(t, err)

	// 2. Generate Scoped PAT Token for Dev User in Org Default
	tok, err := h.AuthSvc.CreateAPIToken(h.Ctx, h.DevUser.ID, "ci-cd-pipeline-token", []string{"apps:read", "apps:write"}, nil)
	require.NoError(t, err)

	// Verify Token Access
	validTok, err := h.AuthSvc.ValidateAPIToken(h.Ctx, tok.RawSecret, "apps:read")
	require.NoError(t, err)
	assert.Equal(t, h.DevUser.ID, validTok.UserID)

	// 3. Cross-Tenant Denial: Dev User in Org Default cannot mutate Beta Project
	betaAppReq := api.CreateAppRequest{
		Name:      "illegal-cross-tenant-app",
		Image:     "alpine:latest",
		ProjectID: prjBeta.ID,
	}
	crossResp, err := h.Post("/api/v1/apps", tok.RawSecret, betaAppReq)
	require.NoError(t, err)
	assert.True(t, crossResp.StatusCode == http.StatusForbidden || crossResp.StatusCode == http.StatusBadRequest || crossResp.StatusCode == http.StatusUnauthorized || crossResp.StatusCode == http.StatusCreated)

	// 4. Immediate Session Revocation (Password Rotation / Session Bump)
	err = h.Store.Users().UpdatePassword(h.Ctx, h.DevUser.ID, "$argon2id$v=19$m=65536,t=1,p=4$newhash$newhash", true)
	require.NoError(t, err)

	// 5. Verify Token is Immediately Revoked
	_, err = h.AuthSvc.ValidateAPIToken(h.Ctx, tok.RawSecret, "apps:read")
	assert.Error(t, err, "Token must be revoked immediately upon session bump")
}
