package deploy_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/deploy"
)

func TestDeployWebhookHandler_BranchMatching(t *testing.T) {
	handler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{})
	rawToken := "pik_ndg_test_branch_match"
	tokenHash := sha256.Sum256([]byte(rawToken))

	var dispatchedMu sync.Mutex
	var dispatchedPayload *deploy.DeployNudgePayload
	var dispatchedService string

	handler.SetDispatcher(deploy.DeploymentDispatcherFunc(func(ctx context.Context, serviceID string, payload deploy.DeployNudgePayload) (string, error) {
		dispatchedMu.Lock()
		defer dispatchedMu.Unlock()
		dispatchedService = serviceID
		dispatchedPayload = &payload
		return "dep_match_123", nil
	}))

	handler.RegisterTokenForTest(hex.EncodeToString(tokenHash[:]), &deploy.NudgeTokenInfo{
		ID:        "tok_branch_match",
		ProjectID: "prj_test",
		ServiceID: "svc_app_01",
		Branch:    "main",
		IsActive:  true,
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// 1. Send push with matching ref "refs/heads/main"
	body, _ := json.Marshal(deploy.DeployNudgePayload{
		Image:     "ghcr.io/fusuycorp/web:sha-abc1234",
		Ref:       "refs/heads/main",
		CommitSha: "abc1234def567890abc1234def567890abc12345",
		Message:   "feat: branch gating support",
		Author:    "DevOps Lead",
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/deploy/nudge/"+rawToken, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status 202 Accepted, got %d", resp.StatusCode)
	}

	var res deploy.NudgeResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res.DeploymentID != "dep_match_123" {
		t.Errorf("expected deployment ID 'dep_match_123', got %s", res.DeploymentID)
	}

	dispatchedMu.Lock()
	defer dispatchedMu.Unlock()
	if dispatchedService != "svc_app_01" {
		t.Errorf("expected serviceID 'svc_app_01', got %s", dispatchedService)
	}
	if dispatchedPayload == nil {
		t.Fatalf("expected dispatcher to be invoked")
	}
	if dispatchedPayload.Branch != "main" {
		t.Errorf("expected normalized branch 'main', got %s", dispatchedPayload.Branch)
	}
	if dispatchedPayload.CommitSha != "abc1234def567890abc1234def567890abc12345" {
		t.Errorf("expected commit SHA, got %s", dispatchedPayload.CommitSha)
	}
	if dispatchedPayload.Message != "feat: branch gating support" {
		t.Errorf("expected commit message, got %s", dispatchedPayload.Message)
	}
	if dispatchedPayload.Author != "DevOps Lead" {
		t.Errorf("expected author, got %s", dispatchedPayload.Author)
	}
}

func TestDeployWebhookHandler_BranchMismatch_Ignored(t *testing.T) {
	handler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{})
	rawToken := "pik_ndg_test_branch_mismatch"
	tokenHash := sha256.Sum256([]byte(rawToken))

	dispatcherCalled := false
	handler.SetDispatcher(deploy.DeploymentDispatcherFunc(func(ctx context.Context, serviceID string, payload deploy.DeployNudgePayload) (string, error) {
		dispatcherCalled = true
		return "dep_mismatch", nil
	}))

	handler.RegisterTokenForTest(hex.EncodeToString(tokenHash[:]), &deploy.NudgeTokenInfo{
		ID:        "tok_branch_gated",
		ProjectID: "prj_test",
		ServiceID: "svc_prod_app",
		Branch:    "production",
		IsActive:  true,
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Push from non-matching branch "refs/heads/feature-login"
	body, _ := json.Marshal(deploy.DeployNudgePayload{
		Image:     "ghcr.io/fusuycorp/web:sha-feat123",
		Ref:       "refs/heads/feature-login",
		CommitSha: "deadbeef00112233445566778899aabbccddeeff",
		Message:   "feat: user login flow",
		Author:    "Feature Dev",
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/deploy/nudge/"+rawToken, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for ignored branch mismatch, got %d", resp.StatusCode)
	}

	var res map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if res["status"] != "ignored" || res["reason"] != "branch mismatch" {
		t.Errorf("expected {\"status\":\"ignored\",\"reason\":\"branch mismatch\"}, got %+v", res)
	}
	if dispatcherCalled {
		t.Errorf("dispatcher should NOT be called on branch mismatch")
	}
}

func TestDeployWebhookHandler_CommitMetadataExtraction(t *testing.T) {
	handler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{})
	rawToken := "pik_ndg_test_metadata"
	tokenHash := sha256.Sum256([]byte(rawToken))

	var receivedPayload deploy.DeployNudgePayload
	handler.SetDispatcher(deploy.DeploymentDispatcherFunc(func(ctx context.Context, serviceID string, payload deploy.DeployNudgePayload) (string, error) {
		receivedPayload = payload
		return "dep_meta_123", nil
	}))

	handler.RegisterTokenForTest(hex.EncodeToString(tokenHash[:]), &deploy.NudgeTokenInfo{
		ID:        "tok_meta",
		ProjectID: "prj_test",
		ServiceID: "svc_meta",
		IsActive:  true,
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	body, _ := json.Marshal(deploy.DeployNudgePayload{
		Image:     "docker.io/library/alpine:latest",
		Branch:    "develop",
		CommitSha: "11223344556677889900aabbccddeeff11223344",
		Message:   "fix: critical memory leak",
		Author:    "Backend Architect <architect@pikpik.dev>",
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/deploy/nudge/"+rawToken, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", resp.StatusCode)
	}

	if receivedPayload.CommitSha != "11223344556677889900aabbccddeeff11223344" {
		t.Errorf("commit SHA mismatch: %s", receivedPayload.CommitSha)
	}
	if receivedPayload.Branch != "develop" {
		t.Errorf("branch mismatch: %s", receivedPayload.Branch)
	}
	if receivedPayload.Message != "fix: critical memory leak" {
		t.Errorf("message mismatch: %s", receivedPayload.Message)
	}
	if receivedPayload.Author != "Backend Architect <architect@pikpik.dev>" {
		t.Errorf("author mismatch: %s", receivedPayload.Author)
	}
}

func TestDeployWebhookHandler_UnconfiguredBranchAllowsAll(t *testing.T) {
	handler := deploy.NewDeployWebhookHandler(deploy.HandlerOptions{})
	rawToken := "pik_ndg_test_unconfigured"
	tokenHash := sha256.Sum256([]byte(rawToken))

	callCount := 0
	handler.SetDispatcher(deploy.DeploymentDispatcherFunc(func(ctx context.Context, serviceID string, payload deploy.DeployNudgePayload) (string, error) {
		callCount++
		return "dep_any", nil
	}))

	// Token with empty Branch (no gating configured)
	handler.RegisterTokenForTest(hex.EncodeToString(tokenHash[:]), &deploy.NudgeTokenInfo{
		ID:        "tok_unconf",
		ProjectID: "prj_test",
		ServiceID: "svc_unconf",
		Branch:    "",
		IsActive:  true,
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	body, _ := json.Marshal(deploy.DeployNudgePayload{
		Image:     "docker.io/library/nginx:alpine",
		Ref:       "refs/heads/any-feature-branch",
		CommitSha: "abcdef1234567890abcdef1234567890abcdef12",
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/deploy/nudge/"+rawToken, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", resp.StatusCode)
	}
	if callCount != 1 {
		t.Errorf("expected dispatcher to be called once, got %d", callCount)
	}
}
