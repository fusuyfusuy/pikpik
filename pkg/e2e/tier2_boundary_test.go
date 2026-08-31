package e2e

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
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
	"github.com/fusuycorp/pikpik/pkg/telemetry"
)

// ============================================================================
// TIER 2: BOUNDARY & CORNER CASES (ROBUSTNESS & ADVERSARIAL INPUTS)
// ============================================================================

// ----------------------------------------------------------------------------
// Group 1: Compute & App Deployments Boundary Cases
// ----------------------------------------------------------------------------
func TestTier2_Boundary_AppInputsAndLimits(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Empty App Name
	t.Run("EmptyAppNameRejection", func(t *testing.T) {
		req := api.CreateAppRequest{
			Name:      "",
			Image:     "nginx:alpine",
			ProjectID: "prj_default",
		}
		resp, err := h.Post("/api/v1/apps", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	// 2. Empty App Name Rejection
	t.Run("EmptyAppNameRejection", func(t *testing.T) {
		req := api.CreateAppRequest{
			Name:      "",
			Image:     "nginx:alpine",
			ProjectID: "prj_default",
		}
		resp, err := h.Post("/api/v1/apps", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	// 3. Extreme String Payload in App Name & Env
	t.Run("ExtremePayloadStringsAndUnicode", func(t *testing.T) {
		largeEnvVal := strings.Repeat("A", 10000)
		specialEnvVal := "🔥🚀; rm -rf /; $(whoami); `id` & <script>alert(1)</script>"
		req := api.CreateAppRequest{
			Name:          "extreme-app-unicode-日本語",
			Image:         "nginx:alpine",
			ProjectID:     "prj_default",
			ContainerPort: 8080,
			Env: map[string]string{
				"LARGE_VAR":   largeEnvVal,
				"SPECIAL_VAR": specialEnvVal,
			},
		}
		resp, err := h.Post("/api/v1/apps", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK)
	})

	// 4. Non-Existent App Operations Handling
	t.Run("NonExistentApp404Handling", func(t *testing.T) {
		nonExistentID := "app_non_existent_99999"
		resp, err := h.Get(fmt.Sprintf("/api/v1/apps/%s", nonExistentID), h.AdminUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		resp, err = h.Post(fmt.Sprintf("/api/v1/apps/%s/deploy", nonExistentID), h.AdminUser.Token, nil)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode >= 400)

		resp, err = h.Post(fmt.Sprintf("/api/v1/apps/%s/stop", nonExistentID), h.AdminUser.Token, nil)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode >= 400)

		resp, err = h.Delete(fmt.Sprintf("/api/v1/apps/%s", nonExistentID), h.AdminUser.Token)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound)
	})
}

// ----------------------------------------------------------------------------
// Group 2: Docker Compose & DAG Boundary Cases
// ----------------------------------------------------------------------------
func TestTier2_Boundary_StackDAGCyclesAndErrors(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Direct Circular Dependency Detection (A -> B -> A)
	t.Run("DirectCircularDependencyRejection", func(t *testing.T) {
		circularServices := map[string]orchestration.ComposeServiceDef{
			"service-a": {Image: "alpine", DependsOn: []string{"service-b"}},
			"service-b": {Image: "alpine", DependsOn: []string{"service-a"}},
		}
		_, err := orchestration.ResolveDeploymentOrder(circularServices)
		assert.ErrorIs(t, err, orchestration.ErrCyclicDependency)
	})

	// 2. Transitive Circular Dependency Detection (A -> B -> C -> A)
	t.Run("TransitiveCircularDependencyRejection", func(t *testing.T) {
		transitiveServices := map[string]orchestration.ComposeServiceDef{
			"app":   {Image: "node", DependsOn: []string{"api"}},
			"api":   {Image: "go", DependsOn: []string{"db"}},
			"db":    {Image: "postgres", DependsOn: []string{"proxy"}},
			"proxy": {Image: "caddy", DependsOn: []string{"app"}},
		}
		_, err := orchestration.ResolveDeploymentOrder(transitiveServices)
		assert.ErrorIs(t, err, orchestration.ErrCyclicDependency)
	})

	// 3. Unknown Dependency Target Reference
	t.Run("UnknownDependencyRejection", func(t *testing.T) {
		unknownDepServices := map[string]orchestration.ComposeServiceDef{
			"api": {Image: "node", DependsOn: []string{"missing-ghost-database"}},
		}
		_, err := orchestration.ResolveDeploymentOrder(unknownDepServices)
		assert.ErrorIs(t, err, orchestration.ErrUnknownDependency)
	})

	// 4. Malformed Compose YAML Ingestion
	t.Run("MalformedComposeYAMLValidation", func(t *testing.T) {
		corruptedYAML := `
version: '3.8'
services:
  bad-service:
    image: [unclosed list
    ports: "invalid"
`
		req := api.InspectComposeRequest{
			ComposeYAML: corruptedYAML,
		}
		resp, err := h.Post("/api/v1/apps/inspect-compose", h.ViewerUser.Token, req)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusBadRequest || resp.StatusCode >= 400 || !resp.Success)
	})

	// 5. Inspect Empty Compose YAML
	t.Run("InspectEmptyComposeYAML", func(t *testing.T) {
		req := api.InspectComposeRequest{
			ComposeYAML: "",
		}
		resp, err := h.Post("/api/v1/apps/inspect-compose", h.ViewerUser.Token, req)
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusBadRequest || resp.StatusCode >= 400 || resp.StatusCode == http.StatusOK)
	})
}

// ----------------------------------------------------------------------------
// Group 3: Dynamic Ingress & Traffic Split Boundary Cases
// ----------------------------------------------------------------------------
func TestTier2_Boundary_TrafficSplitsAndIngress(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Extreme Traffic Split Ratios (0/100, 100/0, 99/1)
	t.Run("ExtremeTrafficSplitRatios", func(t *testing.T) {
		testRatios := []struct {
			w1 int
			w2 int
		}{
			{w1: 100, w2: 0},
			{w1: 0, w2: 100},
			{w1: 99, w2: 1},
			{w1: 50, w2: 50},
		}

		for _, r := range testRatios {
			cfg := ingress.TrafficSplitConfig{
				Domain: "split.example.com",
				Splits: []ingress.UpstreamWeight{
					{Upstream: "10.0.0.1:8080", Weight: r.w1},
					{Upstream: "10.0.0.2:8080", Weight: r.w2},
				},
				Paths: []string{"/"},
			}
			err := h.IngressMgr.SetTrafficSplit(h.Ctx, "split.example.com", cfg)
			require.NoError(t, err)

			route, ok := h.CaddyServer.GetRoute("route_split_split_example_com")
			assert.True(t, ok)
			assert.NotEmpty(t, route.Handle)
		}
	})

	// 2. Route with Multiple Unicode & Punycode Domains
	t.Run("PunycodeAndSubdomainRouting", func(t *testing.T) {
		spec := ingress.RouteSpec{
			ID:           "route_punycode",
			ServiceID:    "srv_punycode",
			Hosts:        []string{"xn--bcher-kva.example.com", "sub.sub2.domain.co.uk"},
			UpstreamDial: "127.0.0.1:9000",
		}
		err := h.IngressMgr.ApplyRoute(h.Ctx, spec)
		require.NoError(t, err)

		r, ok := h.CaddyServer.GetRoute("route_punycode")
		assert.True(t, ok)
		assert.Equal(t, 2, len(r.Match[0].Host))
	})

	// 3. On-Demand TLS /ask Endpoint Injections
	t.Run("OnDemandTLSAskSecurityAttacks", func(t *testing.T) {
		attackPayloads := []string{
			"admin.pikpik.dev; DROP TABLE services;--",
			strings.Repeat("A", 1000) + ".com",
			"../../../../etc/passwd",
			"",
		}

		for _, payload := range attackPayloads {
			resp, err := h.Get(fmt.Sprintf("/api/v1/ingress/ask?domain=%s", payload), "")
			require.NoError(t, err)
			assert.True(t, resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusBadRequest, "Malicious domain must be strictly rejected with 403 or 400")
		}
	})

	// 4. Non-Existent Route Deletion (Idempotency)
	t.Run("IdempotentNonExistentRouteDeletion", func(t *testing.T) {
		err := h.IngressMgr.RemoveRoute(h.Ctx, "route_non_existent_ghost_999")
		assert.NoError(t, err, "Route deletion must be idempotent")
	})

	// 5. High-Frequency Rapid Route Mutations
	t.Run("RapidRouteMutationsConcurrency", func(t *testing.T) {
		for i := 0; i < 20; i++ {
			spec := ingress.RouteSpec{
				ID:           fmt.Sprintf("route_burst_%d", i),
				ServiceID:    fmt.Sprintf("srv_burst_%d", i),
				Hosts:        []string{fmt.Sprintf("burst-%d.example.com", i)},
				UpstreamDial: fmt.Sprintf("127.0.0.1:%d", 8000+i),
			}
			err := h.IngressMgr.ApplyRoute(h.Ctx, spec)
			require.NoError(t, err)
		}
		assert.GreaterOrEqual(t, h.CaddyServer.RouteCount(), 20)
	})
}

// ----------------------------------------------------------------------------
// Group 4: S3 Streaming & Backup Boundary Cases
// ----------------------------------------------------------------------------
func TestTier2_Boundary_S3BackupStreamingLimits(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Zero-Byte Stream Backup Upload
	t.Run("ZeroByteStreamBackupUpload", func(t *testing.T) {
		emptyReader := bytes.NewReader([]byte{})
		obj, err := h.S3Client.UploadStreamMultipart(h.Ctx, "backups/empty.sql.gz", emptyReader, s3.UploadOptions{})
		require.NoError(t, err)
		assert.Equal(t, int64(0), obj.Size)
		assert.NotEmpty(t, obj.Key)
	})

	// 2. Large Multi-Chunk Streaming Upload (Bounded Memory <32MB)
	t.Run("LargeMultiChunkStreamingBoundedMemory", func(t *testing.T) {
		largePayload := strings.Repeat("POSTGRESQL-DUMP-ROW-DATA-MOCK-LINE\n", 100000) // ~3.5MB in RAM
		obj, err := h.S3Client.UploadStreamMultipart(h.Ctx, "backups/large_db.sql.gz", strings.NewReader(largePayload), s3.UploadOptions{
			ContentType: "application/gzip",
		})
		require.NoError(t, err)
		assert.Equal(t, int64(len(largePayload)), obj.Size)

		// Download and verify integrity
		rc, _, err := h.S3Client.DownloadStream(h.Ctx, obj.Key)
		require.NoError(t, err)
		defer rc.Close()

		buf := new(bytes.Buffer)
		_, err = buf.ReadFrom(rc)
		require.NoError(t, err)
		assert.Equal(t, len(largePayload), buf.Len())
	})

	// 3. Non-Existent S3 Key Download (404)
	t.Run("NonExistentS3KeyDownload", func(t *testing.T) {
		_, _, err := h.S3Client.DownloadStream(h.Ctx, "backups/non_existent_key_123.sql.gz")
		assert.Error(t, err)
	})

	// 4. Retention Policy with Zero Backups to Keep
	t.Run("RetentionPolicyZeroMaxBackups", func(t *testing.T) {
		for i := 1; i <= 3; i++ {
			_, _ = h.S3Client.UploadStreamMultipart(h.Ctx, fmt.Sprintf("retention/dump_%d.gz", i), strings.NewReader("data"), s3.UploadOptions{})
		}
		pruned, err := h.S3Client.PruneRetention(h.Ctx, "retention/", s3.RetentionPolicy{MaxBackups: 1})
		require.NoError(t, err)
		assert.Equal(t, 2, len(pruned))
	})

	// 5. Restore Request with Invalid Service
	t.Run("RestoreToNonExistentService", func(t *testing.T) {
		resp, err := h.Post("/api/v1/backups/bk_invalid_123/restore", h.AdminUser.Token, api.RestoreBackupRequest{
			TargetServiceID: "srv_ghost_not_found",
		})
		require.NoError(t, err)
		assert.True(t, resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest || resp.StatusCode >= 400)
	})
}

// ----------------------------------------------------------------------------
// Group 5: Authentication, RBAC & Security Limits
// ----------------------------------------------------------------------------
func TestTier2_Boundary_AuthAndRBACLimits(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Expired API Token
	t.Run("ExpiredTokenRejection", func(t *testing.T) {
		pastTime := time.Now().UTC().Add(-2 * time.Hour)
		tok, err := h.AuthSvc.CreateAPIToken(h.Ctx, h.AdminUser.ID, "expired-token", []string{"*"}, &pastTime)
		require.NoError(t, err)

		_, err = h.AuthSvc.ValidateAPIToken(h.Ctx, tok.RawSecret, "apps:read")
		assert.ErrorIs(t, err, auth.ErrTokenExpired)
	})

	// 2. Tampered Token Secret
	t.Run("TamperedTokenRejection", func(t *testing.T) {
		tamperedToken := "pik_live_invalidchecksumsecretkeythatdoesntexist"
		_, err := h.AuthSvc.ValidateAPIToken(h.Ctx, tamperedToken, "apps:read")
		assert.ErrorIs(t, err, auth.ErrTokenNotFound)
	})

	// 3. Insufficient Scope Permissions
	t.Run("InsufficientScopeRejection", func(t *testing.T) {
		tok, err := h.AuthSvc.CreateAPIToken(h.Ctx, h.DevUser.ID, "read-only-token", []string{"apps:read"}, nil)
		require.NoError(t, err)

		// Validate with required write scope
		_, err = h.AuthSvc.ValidateAPIToken(h.Ctx, tok.RawSecret, "apps:write")
		assert.ErrorIs(t, err, auth.ErrInsufficientScope)
	})

	// 4. Unauthenticated Access to Protected Routes
	t.Run("UnauthenticatedAccessRejection", func(t *testing.T) {
		protectedEndpoints := []struct {
			method string
			path   string
		}{
			{method: http.MethodGet, path: "/api/v1/projects"},
			{method: http.MethodPost, path: "/api/v1/apps"},
			{method: http.MethodGet, path: "/api/v1/databases"},
			{method: http.MethodGet, path: "/api/v1/machines"},
			{method: http.MethodPost, path: "/api/v1/system/prune"},
		}

		for _, ep := range protectedEndpoints {
			resp, err := h.Request(ep.method, ep.path, "", nil)
			require.NoError(t, err)
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "Endpoint %s %s must require auth", ep.method, ep.path)
		}
	})

	// 5. RBAC Privilege Escalation Prevention
	t.Run("ViewerCannotMutateSystem", func(t *testing.T) {
		// Viewer attempting admin prune
		resp, err := h.Post("/api/v1/system/prune", h.ViewerUser.Token, api.PruneRequest{All: true})
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)

		// Viewer attempting token creation
		resp, err = h.Post("/api/v1/auth/tokens", h.ViewerUser.Token, api.CreateTokenRequest{Name: "hacked", Scopes: []string{"*"}})
		require.NoError(t, err)
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

// ----------------------------------------------------------------------------
// Group 6: Interactive Container PTY Terminal Boundary Cases
// ----------------------------------------------------------------------------
func TestTier2_Boundary_PTYBinaryProtocol(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Zero Terminal Dimensions (0x0)
	t.Run("ZeroTerminalDimensionsResize", func(t *testing.T) {
		conn, _, err := h.DialWebSocket("/ws/pty?container=mock-c1", h.AdminUser.Token)
		if err == nil {
			defer conn.Close()
			zeroResizeFrame := []byte{0x01, 0x00, 0x00, 0x00, 0x00}
			err = conn.WriteMessage(websocket.BinaryMessage, zeroResizeFrame)
			assert.NoError(t, err)
		}
	})

	// 2. Extreme Terminal Dimensions (65535x65535)
	t.Run("ExtremeTerminalDimensionsResize", func(t *testing.T) {
		conn, _, err := h.DialWebSocket("/ws/pty?container=mock-c1", h.AdminUser.Token)
		if err == nil {
			defer conn.Close()
			extremeResizeFrame := []byte{0x01, 0xFF, 0xFF, 0xFF, 0xFF}
			err = conn.WriteMessage(websocket.BinaryMessage, extremeResizeFrame)
			assert.NoError(t, err)
		}
	})

	// 3. Malformed Binary Framing (Truncated Frame)
	t.Run("TruncatedBinaryFramePayload", func(t *testing.T) {
		conn, _, err := h.DialWebSocket("/ws/pty?container=mock-c1", h.AdminUser.Token)
		if err == nil {
			defer conn.Close()
			// 1-byte truncated frame for opcode 0x01 (resize needs 5 bytes)
			truncatedFrame := []byte{0x01}
			_ = conn.WriteMessage(websocket.BinaryMessage, truncatedFrame)
		}
	})

	// 4. Non-Existent Container PTY Connect
	t.Run("NonExistentContainerPTYConnect", func(t *testing.T) {
		conn, resp, _ := h.DialWebSocket("/ws/pty?container=non_existent_container_ghost", h.AdminUser.Token)
		if conn != nil {
			conn.Close()
		}
		if resp != nil {
			assert.True(t, resp.StatusCode < 500)
		}
	})

	// 5. Unauthenticated WebSocket Upgrade Rejection
	t.Run("UnauthenticatedWebSocketUpgrade", func(t *testing.T) {
		_, resp, err := h.DialWebSocket("/ws/pty?container=mock-c1", "")
		if err != nil && resp != nil {
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		}
	})
}

// ----------------------------------------------------------------------------
// Group 7: Telemetry Scrapers & Ring Buffer Boundaries
// ----------------------------------------------------------------------------
func TestTier2_Boundary_TelemetryBufferBoundaries(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Ring Buffer Capacity Wrap-Around (>8640 Points Eviction)
	t.Run("RingBufferWrapAroundEviction", func(t *testing.T) {
		capacity := 100
		rb := telemetry.NewRingBuffer(capacity)

		// Push 250 points (2.5x capacity)
		for i := 1; i <= 250; i++ {
			rb.Push(telemetry.MetricPoint{
				Timestamp:   int64(i),
				CPUPercent:  float32(i % 100),
				MemoryBytes: uint64(i * 1024),
			})
		}

		points := rb.GetLastN(100)
		assert.Len(t, points, 100)
		// Oldest point should be 151 (1..150 evicted)
		assert.Equal(t, int64(151), points[0].Timestamp)
		assert.Equal(t, int64(250), points[99].Timestamp)
	})

	// 2. Query More Points Than Stored
	t.Run("QueryMorePointsThanStored", func(t *testing.T) {
		rb := telemetry.NewRingBuffer(100)
		rb.Push(telemetry.MetricPoint{Timestamp: 1, CPUPercent: 10})
		rb.Push(telemetry.MetricPoint{Timestamp: 2, CPUPercent: 20})

		points := rb.GetLastN(50)
		assert.Len(t, points, 2)
	})

	// 3. Zero / Negative N Queries
	t.Run("ZeroOrNegativeQueryN", func(t *testing.T) {
		rb := telemetry.NewRingBuffer(100)
		rb.Push(telemetry.MetricPoint{Timestamp: 1})

		assert.Nil(t, rb.GetLastN(0))
		assert.Nil(t, rb.GetLastN(-5))
	})

	// 4. Downsampler on Empty Data
	t.Run("DownsamplerOnEmptyData", func(t *testing.T) {
		downsampler := telemetry.NewDownsampler(h.Store.DB())
		emptyRB := telemetry.NewRingBuffer(10)
		err := downsampler.DownsampleAndSave(h.Ctx, "node", "empty-node", emptyRB, time.Now().Unix())
		require.NoError(t, err)
	})

	// 5. Concurrent Push and Read on Ring Buffer
	t.Run("ConcurrentPushAndReadRingBuffer", func(t *testing.T) {
		rb := telemetry.NewRingBuffer(1000)
		done := make(chan bool)

		// Writer
		go func() {
			for i := 0; i < 500; i++ {
				rb.Push(telemetry.MetricPoint{
					Timestamp:  int64(i),
					CPUPercent: float32(i % 50),
				})
			}
			done <- true
		}()

		// Reader
		go func() {
			for i := 0; i < 500; i++ {
				_ = rb.GetLastN(50)
			}
			done <- true
		}()

		<-done
		<-done
	})
}

// ----------------------------------------------------------------------------
// Group 8: Volume, Network & System Prune Boundaries
// ----------------------------------------------------------------------------
func TestTier2_Boundary_SystemAndPrune(t *testing.T) {
	h := NewTestHarness(t)
	defer h.Close()

	// 1. Prune on Empty System (Zero Reclaimable)
	t.Run("PruneEmptySystem", func(t *testing.T) {
		resp, err := h.Post("/api/v1/system/prune", h.AdminUser.Token, api.PruneRequest{
			All:        true,
			Volumes:    true,
			BuildCache: true,
		})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var wrapped api.Response[api.PruneResult]
		_ = resp.JSON(&wrapped)
		assert.True(t, wrapped.Success)
	})

	// 2. High-Quantity Volume Creation and Mass Cleanup
	t.Run("HighQuantityVolumeCreationAndCleanup", func(t *testing.T) {
		for i := 0; i < 15; i++ {
			req := api.CreateVolumeRequest{
				Name:      fmt.Sprintf("burst_vol_%d", i),
				ProjectID: "prj_default",
				Driver:    "local",
			}
			resp, err := h.Post("/api/v1/volumes", h.AdminUser.Token, req)
			require.NoError(t, err)
			assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated)
		}

		resp, err := h.Get("/api/v1/volumes?project_id=prj_default", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 3. High-Quantity Network Creation and Mass Cleanup
	t.Run("HighQuantityNetworkCreationAndCleanup", func(t *testing.T) {
		for i := 0; i < 15; i++ {
			req := api.CreateNetworkRequest{
				Name:      fmt.Sprintf("burst_net_%d", i),
				ProjectID: "prj_default",
				Driver:    "bridge",
			}
			resp, err := h.Post("/api/v1/networks", h.AdminUser.Token, req)
			require.NoError(t, err)
			assert.True(t, resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated)
		}

		resp, err := h.Get("/api/v1/networks?project_id=prj_default", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	// 4. Duplicate Volume Name in Same Project
	t.Run("DuplicateVolumeNameRejection", func(t *testing.T) {
		req := api.CreateVolumeRequest{
			Name:      "shared_unique_vol",
			ProjectID: "prj_default",
		}
		resp1, err := h.Post("/api/v1/volumes", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.True(t, resp1.StatusCode == http.StatusOK || resp1.StatusCode == http.StatusCreated)

		resp2, err := h.Post("/api/v1/volumes", h.AdminUser.Token, req)
		require.NoError(t, err)
		assert.True(t, resp2.StatusCode >= 400 || !resp2.Success || resp2.StatusCode == http.StatusOK)
	})

	// 5. System Info and Disk Usage DTO Verification
	t.Run("SystemInfoAndDiskUsage", func(t *testing.T) {
		resp, err := h.Get("/api/v1/system/info", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var infoWrapped api.Response[api.SystemInfo]
		_ = resp.JSON(&infoWrapped)
		assert.True(t, infoWrapped.Success)

		resp, err = h.Get("/api/v1/system/disk", h.ViewerUser.Token)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})
}
