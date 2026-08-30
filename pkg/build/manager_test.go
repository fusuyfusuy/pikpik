package build_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/build"
	"github.com/fusuycorp/pikpik/pkg/git"
	"github.com/fusuycorp/pikpik/pkg/store"
)

// mockBroadcaster records broadcast messages for test assertions.
type mockBroadcaster struct {
	mu     sync.Mutex
	events []string
}

func (m *mockBroadcaster) Broadcast(channel, targetID, event string, data any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, fmt.Sprintf("%s:%s:%s", channel, targetID, event))
}

// mockDeployer captures app deployments.
type mockDeployer struct {
	mu            sync.Mutex
	deployedApp   string
	deployedImage string
	deployErr     error
}

func (m *mockDeployer) DeployApp(ctx context.Context, appID string, image string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deployedApp = appID
	m.deployedImage = image
	return m.deployErr
}

func (m *mockDeployer) getDeployed() (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deployedApp, m.deployedImage
}

// mockPusher is a mock implementation of build.ImagePusher.
type mockPusher struct {
	mu          sync.Mutex
	pushedImage string
	pushedAuth  string
	pushErr     error
}

func (m *mockPusher) Push(ctx context.Context, imageTag, auth string, logCb build.LogCallback) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pushedImage = imageTag
	m.pushedAuth = auth
	if logCb != nil {
		logCb("[mock-pusher] Pushed " + imageTag)
	}
	return m.pushErr
}

func (m *mockPusher) getPushed() (string, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pushedImage, m.pushedAuth
}

// mockBuilder is a mock implementation of build.Builder.
type mockBuilder struct {
	buildFunc func(ctx context.Context, srcDir string, opts build.BuildOptions, logCb build.LogCallback) (*build.BuildResult, error)
}

func (m *mockBuilder) Build(ctx context.Context, srcDir string, opts build.BuildOptions, logCb build.LogCallback) (*build.BuildResult, error) {
	if m.buildFunc != nil {
		return m.buildFunc(ctx, srcDir, opts, logCb)
	}
	if logCb != nil {
		logCb("[mock] Compiling step 1/2...")
		logCb("[mock] Successfully compiled image.")
	}
	return &build.BuildResult{
		ImageTag: opts.ImageTag,
		Strategy: opts.Strategy,
		Duration: 150 * time.Millisecond,
		ImageID:  "sha256:mockimage123",
	}, nil
}

func setupTestStore(t *testing.T) (store.Store, string) {
	t.Helper()
	st, err := store.Open("file:" + store.NewID("db") + "?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("failed to open memory sqlite store: %v", err)
	}

	ctx := context.Background()
	org := &store.Organization{Name: "Acme", Slug: "acme-" + store.NewID("tst")}
	_ = st.Organizations().Create(ctx, org)

	proj := &store.Project{OrgID: org.ID, Name: "Proj", Slug: "proj-" + store.NewID("tst")}
	_ = st.Projects().Create(ctx, proj)

	stage := &store.Stage{ProjectID: proj.ID, Name: "Prod", Slug: "prod"}
	_ = st.Stages().Create(ctx, stage)

	svc := &store.Service{
		ID:        "app_web_01",
		ProjectID: proj.ID,
		StageID:   stage.ID,
		Name:      "web",
		Slug:      "web",
		Type:      "app",
		Image:     "nginx:old",
	}
	_ = st.Services().Create(ctx, svc)

	return st, svc.ID
}

func TestBuildManager_EndToEndSuccess(t *testing.T) {
	st, serviceID := setupTestStore(t)
	defer st.Close()

	broadcaster := &mockBroadcaster{}
	deployer := &mockDeployer{}

	var createdWorkspaceDir string

	mockCloner := func(ctx context.Context, opts git.CloneOptions) (*git.Workspace, error) {
		tempWS, err := os.MkdirTemp("", "pikpik-cloner-ws-*")
		if err != nil {
			return nil, err
		}
		createdWorkspaceDir = tempWS

		// Write mock Dockerfile
		_ = os.WriteFile(filepath.Join(tempWS, "Dockerfile"), []byte("FROM alpine\n"), 0644)
		_ = os.WriteFile(filepath.Join(tempWS, "app.js"), []byte("console.log(1)"), 0644)

		return &git.Workspace{
			Path:    tempWS,
			AppID:   opts.AppID,
			BuildID: opts.BuildID,
		}, nil
	}

	bm := build.NewBuildManager(build.BuildManagerOptions{
		Store:             st,
		DockerfileBuilder: &mockBuilder{},
		Deployer:          deployer,
		Broadcaster:       broadcaster,
		Cloner:            mockCloner,
		Workers:           1,
	})
	defer bm.Close()

	var logMu sync.Mutex
	var streamedLogs []string
	job := &build.BuildJob{
		ID:            "bld_test_success_01",
		AppID:         serviceID,
		RepoURL:       "https://github.com/fusuycorp/example.git",
		Branch:        "main",
		CommitSHA:     "abcdef123456",
		CommitMessage: "feat: rollout test",
		Author:        "Developer",
		LogCallback: func(line string) {
			logMu.Lock()
			streamedLogs = append(streamedLogs, line)
			logMu.Unlock()
		},
	}

	ctx := context.Background()
	if err := bm.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Wait for build completion
	var finishedBuild *store.Build
	for i := 0; i < 50; i++ {
		time.Sleep(50 * time.Millisecond)
		bld, err := st.Builds().GetByID(ctx, job.ID)
		if err == nil && (bld.Status == "success" || bld.Status == "failed") {
			finishedBuild = bld
			break
		}
	}

	if finishedBuild == nil {
		t.Fatalf("build did not finish within timeout")
	}

	if finishedBuild.Status != "success" {
		t.Fatalf("expected build status 'success', got %q (error: %s)", finishedBuild.Status, finishedBuild.ErrorMessage)
	}

	expectedTag := "127.0.0.1:5000/pikpik/" + serviceID + ":abcdef1"
	if finishedBuild.ImageTag != expectedTag {
		t.Errorf("expected tag %s, got %s", expectedTag, finishedBuild.ImageTag)
	}

	depApp, depImg := deployer.getDeployed()
	if depApp != serviceID || depImg != finishedBuild.ImageTag {
		t.Errorf("expected deployer to receive app %s and image %s, got app %s image %s",
			serviceID, finishedBuild.ImageTag, depApp, depImg)
	}

	// Verify service image in store updated
	svc, err := st.Services().GetByID(ctx, serviceID)
	if err != nil || svc.Image != finishedBuild.ImageTag {
		t.Errorf("expected store service image updated to %s, got %v (err: %v)", finishedBuild.ImageTag, svc.Image, err)
	}

	// Verify workspace cleanup
	if createdWorkspaceDir != "" {
		if _, err := os.Stat(createdWorkspaceDir); !os.IsNotExist(err) {
			t.Errorf("expected workspace directory %s to be cleaned up, but it still exists", createdWorkspaceDir)
		}
	}

	logMu.Lock()
	logsCount := len(streamedLogs)
	logMu.Unlock()

	if logsCount == 0 {
		t.Errorf("expected streamed log lines, got 0")
	}
}

func TestBuildManager_BuildFailure(t *testing.T) {
	st, serviceID := setupTestStore(t)
	defer st.Close()

	var createdWorkspaceDir string

	mockCloner := func(ctx context.Context, opts git.CloneOptions) (*git.Workspace, error) {
		tempWS, _ := os.MkdirTemp("", "pikpik-fail-ws-*")
		createdWorkspaceDir = tempWS
		_ = os.WriteFile(filepath.Join(tempWS, "Dockerfile"), []byte("FROM alpine\n"), 0644)
		return &git.Workspace{Path: tempWS}, nil
	}

	failingBuilder := &mockBuilder{
		buildFunc: func(ctx context.Context, srcDir string, opts build.BuildOptions, logCb build.LogCallback) (*build.BuildResult, error) {
			return nil, errors.New("compilation failed at step 4")
		},
	}

	bm := build.NewBuildManager(build.BuildManagerOptions{
		Store:             st,
		DockerfileBuilder: failingBuilder,
		Cloner:            mockCloner,
		Workers:           1,
	})
	defer bm.Close()

	job := &build.BuildJob{
		ID:        "bld_test_fail_01",
		AppID:     serviceID,
		RepoURL:   "https://github.com/fusuycorp/failing.git",
		Branch:    "main",
		CommitSHA: "deadbeef0000",
	}

	ctx := context.Background()
	if err := bm.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	var finishedBuild *store.Build
	for i := 0; i < 50; i++ {
		time.Sleep(50 * time.Millisecond)
		bld, err := st.Builds().GetByID(ctx, job.ID)
		if err == nil && (bld.Status == "success" || bld.Status == "failed") {
			finishedBuild = bld
			break
		}
	}

	if finishedBuild == nil {
		t.Fatalf("build did not complete")
	}

	if finishedBuild.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", finishedBuild.Status)
	}

	if finishedBuild.ErrorMessage != "compilation failed at step 4" {
		t.Errorf("expected error message 'compilation failed at step 4', got %q", finishedBuild.ErrorMessage)
	}

	// Verify workspace cleanup on failure as well
	if createdWorkspaceDir != "" {
		if _, err := os.Stat(createdWorkspaceDir); !os.IsNotExist(err) {
			t.Errorf("workspace directory was not cleaned up on failure")
		}
	}
}

func TestBuildManager_Cancel(t *testing.T) {
	st, serviceID := setupTestStore(t)
	defer st.Close()

	bm := build.NewBuildManager(build.BuildManagerOptions{
		Store: st,
		Cloner: func(ctx context.Context, opts git.CloneOptions) (*git.Workspace, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
				return &git.Workspace{Path: os.TempDir()}, nil
			}
		},
		Workers: 1,
	})
	defer bm.Close()

	job := &build.BuildJob{
		ID:      "bld_test_cancel_01",
		AppID:   serviceID,
		RepoURL: "https://github.com/fusuycorp/slow.git",
		Branch:  "main",
	}

	ctx := context.Background()
	_ = bm.Enqueue(ctx, job)

	time.Sleep(50 * time.Millisecond)

	if err := bm.Cancel(ctx, job.ID); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	bld, err := st.Builds().GetByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("failed to fetch cancelled build: %v", err)
	}

	if bld.Status != "cancelled" && bld.Status != "failed" {
		t.Errorf("expected status cancelled or failed, got %q", bld.Status)
	}
}

func TestBuildManager_RegistryPush_Success(t *testing.T) {
	st, serviceID := setupTestStore(t)
	defer st.Close()

	pusher := &mockPusher{}
	deployer := &mockDeployer{}

	mockCloner := func(ctx context.Context, opts git.CloneOptions) (*git.Workspace, error) {
		tempWS, _ := os.MkdirTemp("", "pikpik-push-ws-*")
		_ = os.WriteFile(filepath.Join(tempWS, "Dockerfile"), []byte("FROM alpine\n"), 0644)
		return &git.Workspace{Path: tempWS}, nil
	}

	bm := build.NewBuildManager(build.BuildManagerOptions{
		Store:             st,
		DockerfileBuilder: &mockBuilder{},
		Deployer:          deployer,
		ImagePusher:       pusher,
		Cloner:            mockCloner,
		RegistryAuth:      "base64authtoken",
		Workers:           1,
	})
	defer bm.Close()

	job := &build.BuildJob{
		ID:        "bld_test_push_01",
		AppID:     serviceID,
		RepoURL:   "https://github.com/fusuycorp/push-success.git",
		Branch:    "main",
		CommitSHA: "1234567890ab",
	}

	ctx := context.Background()
	if err := bm.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	var finishedBuild *store.Build
	for i := 0; i < 50; i++ {
		time.Sleep(50 * time.Millisecond)
		bld, err := st.Builds().GetByID(ctx, job.ID)
		if err == nil && (bld.Status == "success" || bld.Status == "failed") {
			finishedBuild = bld
			break
		}
	}

	if finishedBuild == nil || finishedBuild.Status != "success" {
		t.Fatalf("expected build success, got %v", finishedBuild)
	}

	expectedTag := "127.0.0.1:5000/pikpik/" + serviceID + ":1234567"
	pushedImg, pushedAuth := pusher.getPushed()
	if pushedImg != expectedTag {
		t.Errorf("expected pushed image %s, got %s", expectedTag, pushedImg)
	}
	if pushedAuth != "base64authtoken" {
		t.Errorf("expected auth token 'base64authtoken', got '%s'", pushedAuth)
	}
}

func TestBuildManager_RegistryPush_Failure(t *testing.T) {
	st, serviceID := setupTestStore(t)
	defer st.Close()

	failingPusher := &mockPusher{
		pushErr: errors.New("connection refused by registry daemon"),
	}

	mockCloner := func(ctx context.Context, opts git.CloneOptions) (*git.Workspace, error) {
		tempWS, _ := os.MkdirTemp("", "pikpik-push-fail-ws-*")
		_ = os.WriteFile(filepath.Join(tempWS, "Dockerfile"), []byte("FROM alpine\n"), 0644)
		return &git.Workspace{Path: tempWS}, nil
	}

	bm := build.NewBuildManager(build.BuildManagerOptions{
		Store:             st,
		DockerfileBuilder: &mockBuilder{},
		ImagePusher:       failingPusher,
		Cloner:            mockCloner,
		Workers:           1,
	})
	defer bm.Close()

	job := &build.BuildJob{
		ID:        "bld_test_push_fail_01",
		AppID:     serviceID,
		RepoURL:   "https://github.com/fusuycorp/push-fail.git",
		Branch:    "main",
		CommitSHA: "abcdef000000",
	}

	ctx := context.Background()
	if err := bm.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	var finishedBuild *store.Build
	for i := 0; i < 50; i++ {
		time.Sleep(50 * time.Millisecond)
		bld, err := st.Builds().GetByID(ctx, job.ID)
		if err == nil && (bld.Status == "success" || bld.Status == "failed") {
			finishedBuild = bld
			break
		}
	}

	if finishedBuild == nil {
		t.Fatalf("build did not complete")
	}

	if finishedBuild.Status != "failed" {
		t.Errorf("expected status 'failed', got %q", finishedBuild.Status)
	}

	if !strings.Contains(finishedBuild.ErrorMessage, "registry push failed") {
		t.Errorf("expected error message to contain 'registry push failed', got %q", finishedBuild.ErrorMessage)
	}
}

func TestBuildManager_CustomRegistryHost(t *testing.T) {
	st, serviceID := setupTestStore(t)
	defer st.Close()

	pusher := &mockPusher{}

	mockCloner := func(ctx context.Context, opts git.CloneOptions) (*git.Workspace, error) {
		tempWS, _ := os.MkdirTemp("", "pikpik-custom-reg-*")
		_ = os.WriteFile(filepath.Join(tempWS, "Dockerfile"), []byte("FROM alpine\n"), 0644)
		return &git.Workspace{Path: tempWS}, nil
	}

	bm := build.NewBuildManager(build.BuildManagerOptions{
		Store:             st,
		DockerfileBuilder: &mockBuilder{},
		ImagePusher:       pusher,
		Cloner:            mockCloner,
		RegistryHost:      "registry.example.com:5000",
		Workers:           1,
	})
	defer bm.Close()

	job := &build.BuildJob{
		ID:        "bld_test_custom_reg_01",
		AppID:     serviceID,
		RepoURL:   "https://github.com/fusuycorp/custom-reg.git",
		Branch:    "main",
		CommitSHA: "fedcba987654",
	}

	ctx := context.Background()
	if err := bm.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	var finishedBuild *store.Build
	for i := 0; i < 50; i++ {
		time.Sleep(50 * time.Millisecond)
		bld, err := st.Builds().GetByID(ctx, job.ID)
		if err == nil && (bld.Status == "success" || bld.Status == "failed") {
			finishedBuild = bld
			break
		}
	}

	if finishedBuild == nil || finishedBuild.Status != "success" {
		t.Fatalf("expected build success, got %v", finishedBuild)
	}

	expectedTag := "registry.example.com:5000/pikpik/" + serviceID + ":fedcba9"
	if finishedBuild.ImageTag != expectedTag {
		t.Errorf("expected custom tag %s, got %s", expectedTag, finishedBuild.ImageTag)
	}
}

