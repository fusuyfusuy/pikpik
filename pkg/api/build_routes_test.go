package api_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/auth"
	"github.com/fusuycorp/pikpik/pkg/store"
)

func TestBuildEndpoints_GitHubWebhook_ValidSignature(t *testing.T) {
	st, err := store.Open("file:" + store.NewID("db") + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	secret := "github-webhook-test-secret"
	_ = os.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	defer os.Unsetenv("GITHUB_WEBHOOK_SECRET")

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
	})
	gw := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller: ctrl,
		Store:      st,
	})
	server := httptest.NewServer(gw)
	defer server.Close()

	payload := `{
		"ref": "refs/heads/main",
		"after": "1a2b3c4d5e6f",
		"head_commit": {
			"id": "1a2b3c4d5e6f",
			"message": "feat: test git integration",
			"author": {"name": "Dev", "email": "dev@pikpik.dev"}
		},
		"repository": {
			"name": "my-app",
			"full_name": "fusuycorp/my-app",
			"clone_url": "https://github.com/fusuycorp/my-app.git"
		},
		"installation": {"id": 12345}
	}`

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/webhooks/github", strings.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", resp.StatusCode)
	}

	var res api.Response[store.Build]
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if res.Data.ID == "" {
		t.Errorf("expected generated build ID, got empty")
	}
	if res.Data.CommitSHA != "1a2b3c4d5e6f" {
		t.Errorf("expected commit sha 1a2b3c4d5e6f, got %s", res.Data.CommitSHA)
	}
}

func TestBuildEndpoints_GitHubWebhook_InvalidSignature(t *testing.T) {
	st, err := store.Open("file:" + store.NewID("db") + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	secret := "secret-correct"
	_ = os.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	defer os.Unsetenv("GITHUB_WEBHOOK_SECRET")

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
	})
	gw := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller: ctrl,
		Store:      st,
	})
	server := httptest.NewServer(gw)
	defer server.Close()

	payload := `{"ref":"refs/heads/main","after":"123"}`
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/webhooks/github", strings.NewReader(payload))
	req.Header.Set("X-Hub-Signature-256", "sha256=invalidmac1234567890")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for bad HMAC, got %d", resp.StatusCode)
	}
}

func TestBuildEndpoints_GitHubWebhook_MissingSignature(t *testing.T) {
	st, err := store.Open("file:" + store.NewID("db") + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	secret := "github-webhook-test-secret"
	_ = os.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	defer os.Unsetenv("GITHUB_WEBHOOK_SECRET")

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
	})
	gw := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller: ctrl,
		Store:      st,
	})
	server := httptest.NewServer(gw)
	defer server.Close()

	payload := `{"ref":"refs/heads/main","after":"123"}`
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/webhooks/github", strings.NewReader(payload))
	// Deliberately omit the X-Hub-Signature-256 header even though a secret
	// is configured server-side: this must be rejected, not fail open.
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized when signature header is missing, got %d", resp.StatusCode)
	}
}

func TestBuildEndpoints_GenericGitWebhook(t *testing.T) {
	st, err := store.Open("file:" + store.NewID("db") + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	_ = st.Organizations().Create(ctx, &store.Organization{
		ID:   "org_default",
		Name: "Default Org",
		Slug: "default",
	})
	_ = st.Projects().Create(ctx, &store.Project{
		ID:    "prj_default",
		OrgID: "org_default",
		Name:  "Default Project",
		Slug:  "default",
	})
	_ = st.Stages().Create(ctx, &store.Stage{
		ID:        "stg_default",
		ProjectID: "prj_default",
		Name:      "Default Stage",
		Slug:      "default",
	})
	if err := st.Services().Create(ctx, &store.Service{
		ID:              "app_custom_01",
		ProjectID:       "prj_default",
		StageID:         "stg_default",
		Name:            "custom-app",
		Slug:            "custom-app",
		DeployTokenHash: auth.HashToken("pik_ndg_test"),
	}); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
	})
	gw := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller: ctrl,
		Store:      st,
	})
	server := httptest.NewServer(gw)
	defer server.Close()

	payload := `{
		"repository": "custom/repo",
		"branch": "develop",
		"commit_sha": "9876543210ab",
		"commit_message": "feat: generic push",
		"author": "Generic Dev",
		"clone_url": "https://git.corp.local/custom/repo.git"
	}`

	// 1. Valid Token
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/webhooks/git/app_custom_01?token=pik_ndg_test", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("generic webhook request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", resp.StatusCode)
	}

	var res api.Response[store.Build]
	_ = json.NewDecoder(resp.Body).Decode(&res)

	if res.Data.ServiceID != "app_custom_01" {
		t.Errorf("expected ServiceID app_custom_01, got %s", res.Data.ServiceID)
	}
	if res.Data.CommitSHA != "9876543210ab" {
		t.Errorf("expected commit sha 9876543210ab, got %s", res.Data.CommitSHA)
	}

	// 2. Invalid Token (Fail Closed)
	badReq, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/webhooks/git/app_custom_01?token=wrong_token", strings.NewReader(payload))
	badReq.Header.Set("Content-Type", "application/json")
	badResp, err := http.DefaultClient.Do(badReq)
	if err != nil {
		t.Fatalf("bad request failed: %v", err)
	}
	defer badResp.Body.Close()
	if badResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized for invalid token, got %d", badResp.StatusCode)
	}
}

func TestBuildEndpoints_ListGetRebuildStream(t *testing.T) {
	st, err := store.Open("file:" + store.NewID("db") + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	org := &store.Organization{Name: "Acme", Slug: "acme-" + store.NewID("tst")}
	_ = st.Organizations().Create(ctx, org)
	proj := &store.Project{OrgID: org.ID, Name: "Proj", Slug: "proj-" + store.NewID("tst")}
	_ = st.Projects().Create(ctx, proj)
	stage := &store.Stage{ProjectID: proj.ID, Name: "Prod", Slug: "prod"}
	_ = st.Stages().Create(ctx, stage)
	svc := &store.Service{ID: "app_api_01", ProjectID: proj.ID, StageID: stage.ID, Name: "api", Slug: "api", Type: "app", Image: "pikpik/app:latest"}
	_ = st.Services().Create(ctx, svc)

	// Create an existing build record
	bld := &store.Build{
		ID:            "bld_existing_123",
		ServiceID:     "app_api_01",
		RepoURL:       "https://github.com/fusuycorp/api.git",
		Branch:        "main",
		CommitSHA:     "c0ffee001122",
		CommitMessage: "feat: initial commit",
		Author:        "Fusuy",
		Status:        "success",
		ImageTag:      "pikpik/app_api_01:c0ffee0",
	}
	if err := st.Builds().Create(ctx, bld); err != nil {
		t.Fatalf("failed to create build record: %v", err)
	}

	sse := api.NewSSEBroadcaster()
	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store:          st,
		SSEBroadcaster: sse,
	})
	gw := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller:     ctrl,
		Store:          st,
		SSEBroadcaster: sse,
	})
	server := httptest.NewServer(gw)
	defer server.Close()

	token := "pik_live_admin_test_token"
	authedGet := func(path string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("get request failed: %v", err)
		}
		return resp
	}
	authedPost := func(path string, body []byte) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, server.URL+path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post request failed: %v", err)
		}
		return resp
	}

	// 1. List builds for app
	listResp := authedGet("/api/v1/apps/app_api_01/builds")
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for list builds, got %d", listResp.StatusCode)
	}
	var listResult api.Response[[]*store.Build]
	_ = json.NewDecoder(listResp.Body).Decode(&listResult)
	listResp.Body.Close()

	if len(listResult.Data) != 1 || listResult.Data[0].ID != "bld_existing_123" {
		t.Fatalf("unexpected list result: %+v", listResult.Data)
	}

	// 2. Get specific build details
	getResp := authedGet("/api/v1/builds/bld_existing_123")
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for get build, got %d", getResp.StatusCode)
	}
	var getResult api.Response[store.Build]
	_ = json.NewDecoder(getResp.Body).Decode(&getResult)
	getResp.Body.Close()

	if getResult.Data.ID != "bld_existing_123" || getResult.Data.CommitSHA != "c0ffee001122" {
		t.Errorf("unexpected build details: %+v", getResult.Data)
	}

	// 3. Trigger Rebuild
	rebuildResp := authedPost("/api/v1/builds/bld_existing_123/rebuild", []byte("{}"))
	if rebuildResp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for rebuild, got %d", rebuildResp.StatusCode)
	}
	var rebuildResult api.Response[store.Build]
	_ = json.NewDecoder(rebuildResp.Body).Decode(&rebuildResult)
	rebuildResp.Body.Close()

	if rebuildResult.Data.ID == "" || rebuildResult.Data.ID == "bld_existing_123" {
		t.Errorf("expected newly generated build ID for rebuild, got %s", rebuildResult.Data.ID)
	}
	if rebuildResult.Data.ServiceID != "app_api_01" {
		t.Errorf("expected ServiceID app_api_01, got %s", rebuildResult.Data.ServiceID)
	}

	// 4. SSE Stream endpoint
	streamReq, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/builds/bld_existing_123/stream", nil)
	streamReq.Header.Set("Authorization", "Bearer "+token)
	streamReq.Header.Set("Accept", "text/event-stream")

	streamCtx, cancelStream := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancelStream()
	streamReq = streamReq.WithContext(streamCtx)

	streamResp, err := http.DefaultClient.Do(streamReq)
	if err == nil {
		defer streamResp.Body.Close()
		if streamResp.StatusCode != http.StatusOK {
			t.Errorf("expected 200 OK for SSE stream, got %d", streamResp.StatusCode)
		}
	}
}

func TestBuildEndpoints_GitHubWebhook_BranchGating(t *testing.T) {
	st, err := store.Open("file:" + store.NewID("db") + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	_ = st.Organizations().Create(ctx, &store.Organization{ID: "org_default", Name: "Default Org", Slug: "default"})
	_ = st.Projects().Create(ctx, &store.Project{ID: "prj_default", OrgID: "org_default", Name: "Default Project", Slug: "default"})
	_ = st.Stages().Create(ctx, &store.Stage{ID: "stg_default", ProjectID: "prj_default", Name: "Default Stage", Slug: "default"})
	if err := st.Services().Create(ctx, &store.Service{
		ID:        "app_prod_svc",
		ProjectID: "prj_default",
		StageID:   "stg_default",
		Name:      "my-gated-app",
		Slug:      "my-gated-app",
		GitBranch: "production",
	}); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	secret := "github-gating-test-secret"
	_ = os.Setenv("GITHUB_WEBHOOK_SECRET", secret)
	defer os.Unsetenv("GITHUB_WEBHOOK_SECRET")

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
	})
	gw := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller: ctrl,
		Store:      st,
	})
	server := httptest.NewServer(gw)
	defer server.Close()

	sendGitHubPush := func(payload string) *http.Response {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(payload))
		sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/webhooks/github", strings.NewReader(payload))
		req.Header.Set("X-Hub-Signature-256", sig)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("push request failed: %v", err)
		}
		return resp
	}

	// 1. Branch Mismatch: push to "main" when service requires "production" -> Ignored
	mismatchPayload := `{
		"ref": "refs/heads/main",
		"after": "111111111111",
		"head_commit": {
			"id": "111111111111",
			"message": "feat: mismatch branch commit",
			"author": {"name": "Junior Dev", "email": "junior@pikpik.dev"}
		},
		"repository": {
			"name": "my-gated-app",
			"full_name": "fusuycorp/my-gated-app",
			"clone_url": "https://github.com/fusuycorp/my-gated-app.git"
		}
	}`
	resp1 := sendGitHubPush(mismatchPayload)
	defer resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for branch mismatch, got %d", resp1.StatusCode)
	}
	var res1 map[string]string
	_ = json.NewDecoder(resp1.Body).Decode(&res1)
	if res1["status"] != "ignored" || res1["reason"] != "branch mismatch" {
		t.Errorf("expected status ignored / reason branch mismatch, got: %+v", res1)
	}

	// Verify no builds were created for the service
	builds1, _ := st.Builds().ListByService(ctx, "app_prod_svc", 10)
	if len(builds1) != 0 {
		t.Errorf("expected 0 builds after mismatch, got %d", len(builds1))
	}

	// 2. Branch Match: push to "production" -> Accepted and builds created with commit metadata
	matchPayload := `{
		"ref": "refs/heads/production",
		"after": "222222222222",
		"head_commit": {
			"id": "222222222222",
			"message": "release: v1.0.0 to prod",
			"author": {"name": "Release Lead", "email": "lead@pikpik.dev"}
		},
		"repository": {
			"name": "my-gated-app",
			"full_name": "fusuycorp/my-gated-app",
			"clone_url": "https://github.com/fusuycorp/my-gated-app.git"
		}
	}`
	resp2 := sendGitHubPush(matchPayload)
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for branch match, got %d", resp2.StatusCode)
	}
	var res2 api.Response[store.Build]
	_ = json.NewDecoder(resp2.Body).Decode(&res2)
	if res2.Data.CommitSHA != "222222222222" {
		t.Errorf("expected commit SHA 222222222222, got %s", res2.Data.CommitSHA)
	}
	if res2.Data.CommitMessage != "release: v1.0.0 to prod" {
		t.Errorf("expected commit message 'release: v1.0.0 to prod', got %s", res2.Data.CommitMessage)
	}
	if res2.Data.Author != "Release Lead" {
		t.Errorf("expected author 'Release Lead', got %s", res2.Data.Author)
	}

	// Verify service record has updated commit metadata
	svcAfter, err := st.Services().GetByID(ctx, "app_prod_svc")
	if err != nil {
		t.Fatalf("failed to fetch service after push: %v", err)
	}
	if svcAfter.LastCommitSHA != "222222222222" {
		t.Errorf("expected service LastCommitSHA '222222222222', got %q", svcAfter.LastCommitSHA)
	}
	if svcAfter.LastCommitMessage != "release: v1.0.0 to prod" {
		t.Errorf("expected service LastCommitMessage 'release: v1.0.0 to prod', got %q", svcAfter.LastCommitMessage)
	}
	if svcAfter.LastCommitAuthor != "Release Lead" {
		t.Errorf("expected service LastCommitAuthor 'Release Lead', got %q", svcAfter.LastCommitAuthor)
	}
}

func TestBuildEndpoints_GenericGitWebhook_BranchGating(t *testing.T) {
	st, err := store.Open("file:" + store.NewID("db") + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	_ = st.Organizations().Create(ctx, &store.Organization{ID: "org_default", Name: "Default Org", Slug: "default"})
	_ = st.Projects().Create(ctx, &store.Project{ID: "prj_default", OrgID: "org_default", Name: "Default Project", Slug: "default"})
	_ = st.Stages().Create(ctx, &store.Stage{ID: "stg_default", ProjectID: "prj_default", Name: "Default Stage", Slug: "default"})
	if err := st.Services().Create(ctx, &store.Service{
		ID:              "app_generic_gated",
		ProjectID:       "prj_default",
		StageID:         "stg_default",
		Name:            "generic-gated",
		Slug:            "generic-gated",
		GitBranch:       "main",
		DeployTokenHash: auth.HashToken("pik_ndg_generic_token"),
	}); err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	ctrl := api.NewDefaultController(api.ControllerDependencies{
		Store: st,
	})
	gw := api.NewAPIGatewayWithOptions(api.APIGatewayOptions{
		Controller: ctrl,
		Store:      st,
	})
	server := httptest.NewServer(gw)
	defer server.Close()

	// 1. Branch Mismatch: push to "feat/payments" -> Ignored (200 OK)
	mismatchPayload := `{
		"ref": "refs/heads/feat/payments",
		"commit_sha": "555555555555",
		"commit_message": "feat: payments stripe integration",
		"author": "Payments Dev",
		"clone_url": "https://git.internal/app.git"
	}`
	req1, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/webhooks/git/app_generic_gated?token=pik_ndg_generic_token", strings.NewReader(mismatchPayload))
	req1.Header.Set("Content-Type", "application/json")
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for generic branch mismatch, got %d", resp1.StatusCode)
	}
	var res1 map[string]string
	_ = json.NewDecoder(resp1.Body).Decode(&res1)
	if res1["status"] != "ignored" || res1["reason"] != "branch mismatch" {
		t.Errorf("expected ignored status, got %+v", res1)
	}

	// 2. Branch Match: push to "refs/heads/main" -> Accepted (202 Accepted)
	matchPayload := `{
		"ref": "refs/heads/main",
		"commit_sha": "666666666666",
		"commit_message": "chore: merge payments to main",
		"author": "Principal Eng",
		"clone_url": "https://git.internal/app.git"
	}`
	req2, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/webhooks/git/app_generic_gated?token=pik_ndg_generic_token", strings.NewReader(matchPayload))
	req2.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted for generic branch match, got %d", resp2.StatusCode)
	}
	var res2 api.Response[store.Build]
	_ = json.NewDecoder(resp2.Body).Decode(&res2)
	if res2.Data.CommitSHA != "666666666666" {
		t.Errorf("expected commit SHA 666666666666, got %s", res2.Data.CommitSHA)
	}
	if res2.Data.CommitMessage != "chore: merge payments to main" {
		t.Errorf("expected commit message, got %s", res2.Data.CommitMessage)
	}
	if res2.Data.Author != "Principal Eng" {
		t.Errorf("expected author, got %s", res2.Data.Author)
	}

	// Verify service metadata updated
	svcAfter, _ := st.Services().GetByID(ctx, "app_generic_gated")
	if svcAfter.LastCommitSHA != "666666666666" {
		t.Errorf("expected service LastCommitSHA '666666666666', got %q", svcAfter.LastCommitSHA)
	}
	if svcAfter.LastCommitMessage != "chore: merge payments to main" {
		t.Errorf("expected service LastCommitMessage 'chore: merge payments to main', got %q", svcAfter.LastCommitMessage)
	}
	if svcAfter.LastCommitAuthor != "Principal Eng" {
		t.Errorf("expected service LastCommitAuthor 'Principal Eng', got %q", svcAfter.LastCommitAuthor)
	}
}

