package deploy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/deploy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeployWebhookHandler_RateLimitAndAuth verifies constant-time auth and rate-limiting triggers HTTP 429.
func TestDeployWebhookHandler_RateLimitAndAuth(t *testing.T) {
	handler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{
		RateLimitPerMin: 5,
		BurstLimit:      2,
	})

	const validToken = "pik_ndg_9e8f7a6b5c4d3e2f1a0b9c8d7e6f5a4b"
	tokenHash := sha256.Sum256([]byte(validToken))
	handler.RegisterTokenForTest(hex.EncodeToString(tokenHash[:]), &deploy.NudgeTokenInfo{
		ID:                "tok_1",
		ProjectID:         "proj_alpha",
		ServiceID:         "svc_web",
		AllowedRegistries: []string{"registry.yourdomain.com"},
		IsActive:          true,
	})

	validBody := []byte(`{"image":"registry.yourdomain.com/apps/web:v1.2.0"}`)

	// Send requests up to burst limit
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/deploy/nudge/"+validToken, bytes.NewReader(validBody))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusAccepted {
			t.Fatalf("expected HTTP 202 on burst request %d, got %d", i, rr.Code)
		}
	}

	// 3rd rapid request should hit rate limit (burst = 2)
	req := httptest.NewRequest(http.MethodPost, "/api/deploy/nudge/"+validToken, bytes.NewReader(validBody))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected HTTP 429 on rate limit exceed, got %d", rr.Code)
	}

	// Invalid token request should return HTTP 401
	invalidReq := httptest.NewRequest(http.MethodPost, "/api/deploy/nudge/pik_ndg_invalid_token_12345", bytes.NewReader(validBody))
	invalidReq.Header.Set("Content-Type", "application/json")
	rrInvalid := httptest.NewRecorder()

	handler.ServeHTTP(rrInvalid, invalidReq)
	if rrInvalid.Code != http.StatusUnauthorized {
		t.Fatalf("expected HTTP 401 on invalid token, got %d", rrInvalid.Code)
	}
}

func TestDeployWebhookHandler_TokenGenerationAndRevocation(t *testing.T) {
	ctx := context.Background()
	handler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{
		RateLimitPerMin: 10,
		BurstLimit:      3,
	})

	rawToken, info, err := handler.GenerateToken(ctx, "svc_api", "proj_main")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(rawToken, "pik_ndg_"))
	assert.Equal(t, "svc_api", info.ServiceID)
	assert.Equal(t, "proj_main", info.ProjectID)
	assert.True(t, info.IsActive)

	// Valid request using generated token
	body := []byte(`{"image":"myregistry.io/org/api:v1.0.0"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/deploy/nudge/"+rawToken, bytes.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusAccepted, rr.Code)

	// Revoke token
	err = handler.RevokeToken(ctx, info.ID)
	require.NoError(t, err)

	// Request should now be 401 Unauthorized
	req2 := httptest.NewRequest(http.MethodPost, "/api/deploy/nudge/"+rawToken, bytes.NewReader(body))
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	assert.Equal(t, http.StatusUnauthorized, rr2.Code)
}

func TestDeployWebhookHandler_PayloadValidation(t *testing.T) {
	handler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{})

	// Valid payload
	err := handler.ValidatePayload(&deploy.DeployNudgePayload{
		Image: "ghcr.io/org/repo:sha-1234567",
	}, []string{"ghcr.io/org/"})
	assert.NoError(t, err)

	// Empty image
	err = handler.ValidatePayload(&deploy.DeployNudgePayload{
		Image: "",
	}, nil)
	assert.ErrorIs(t, err, deploy.ErrInvalidImageReference)

	// Invalid image format
	err = handler.ValidatePayload(&deploy.DeployNudgePayload{
		Image: "invalid image with spaces",
	}, nil)
	assert.ErrorIs(t, err, deploy.ErrInvalidImageReference)

	// Unauthorized registry
	err = handler.ValidatePayload(&deploy.DeployNudgePayload{
		Image: "docker.io/untrusted/repo:tag",
	}, []string{"registry.yourdomain.com", "ghcr.io/fusuycorp/"})
	assert.ErrorIs(t, err, deploy.ErrUnauthorizedRegistry)
}

func TestDeployWebhookHandler_PayloadSizeCeiling(t *testing.T) {
	handler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{})
	rawToken, _, err := handler.GenerateToken(context.Background(), "svc_test", "proj_test")
	require.NoError(t, err)

	// Create payload exceeding 64KB (e.g. 70KB)
	largeMessage := strings.Repeat("A", 70*1024)
	largeBody := `{"image":"registry.example.com/app:v1","message":"` + largeMessage + `"}`

	req := httptest.NewRequest(http.MethodPost, "/api/deploy/nudge/"+rawToken, strings.NewReader(largeBody))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rr.Code)
}

func TestDeployWebhookHandler_DispatcherIntegration(t *testing.T) {
	handler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{})
	rawToken, _, err := handler.GenerateToken(context.Background(), "svc_web", "proj_web")
	require.NoError(t, err)

	dispatched := false
	handler.SetDispatcher(deploy.DeploymentDispatcherFunc(func(ctx context.Context, serviceID string, payload deploy.DeployNudgePayload) (string, error) {
		assert.Equal(t, "svc_web", serviceID)
		assert.Equal(t, "registry.example.com/app:v2.0", payload.Image)
		dispatched = true
		return "dep_custom_12345", nil
	}))

	body := `{"image":"registry.example.com/app:v2.0","commitSha":"1234567890abcdef1234567890abcdef12345678"}`
	req := httptest.NewRequest(http.MethodPost, "/api/deploy/nudge/"+rawToken, strings.NewReader(body))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusAccepted, rr.Code)
	assert.True(t, dispatched)
	assert.Contains(t, rr.Body.String(), "dep_custom_12345")
}
