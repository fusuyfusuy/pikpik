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

func TestBuildEndpoints_GenericGitWebhook(t *testing.T) {
	st, err := store.Open("file:" + store.NewID("db") + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer st.Close()

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
	svc := &store.Service{ID: "app_api_01", ProjectID: proj.ID, StageID: stage.ID, Name: "api", Slug: "api", Type: "app"}
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
