package build

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	imageTypes "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/fusuycorp/pikpik/pkg/git"
	"github.com/fusuycorp/pikpik/pkg/store"
)

var (
	// ErrQueueFull is returned when the build job queue is at maximum capacity.
	ErrQueueFull = errors.New("build: job queue full")

	// ErrManagerClosed is returned when enqueueing to a terminated BuildManager.
	ErrManagerClosed = errors.New("build: build manager is closed")

	// ErrBuildNotFound is returned when attempting to cancel a non-existent build.
	ErrBuildNotFound = errors.New("build: build job not found")
)

// BuildBroadcaster provides real-time event broadcasting to SSE and WebSocket clients.
type BuildBroadcaster interface {
	Broadcast(channel, targetID, event string, data any)
}

// AppDeployer handles deploying or rolling out a service container after a successful build.
type AppDeployer interface {
	DeployApp(ctx context.Context, appID string, image string) error
}

// BuildManagerOptions specifies configuration for initializing a BuildManager instance.
type BuildManagerOptions struct {
	Store             store.Store
	DockerClient      client.CommonAPIClient
	DockerfileBuilder Builder
	NixpacksBuilder   Builder
	GitHubClient      *git.GitHubClient
	Deployer          AppDeployer
	Broadcaster       BuildBroadcaster
	Cloner            GitCloner
	Workers           int
	QueueCapacity     int
	WorkDirRoot       string
	LogsDir           string
	RegistryHost      string
	RegistryAuth      string
	ImagePusher       ImagePusher
}

// BuildManager manages asynchronous build job queues, worker pools, strategy dispatch, and rollout triggers.
type BuildManager struct {
	mu                sync.RWMutex
	st                store.Store
	dockerCli         client.CommonAPIClient
	dockerfileBuilder Builder
	nixpacksBuilder   Builder
	githubClient      *git.GitHubClient
	deployer          AppDeployer
	broadcaster       BuildBroadcaster
	cloner            GitCloner
	registryHost      string
	registryAuth      string
	imagePusher       ImagePusher
	workDirRoot       string
	logsDir           string
	queue             chan *BuildJob
	cancels           map[string]context.CancelFunc
	workers           int
	wg                sync.WaitGroup
	closeOnce         sync.Once
	closed            bool
	closeChan         chan struct{}
}

// NewBuildManager constructs and starts a new worker-pool-backed BuildManager.
func NewBuildManager(opts BuildManagerOptions) *BuildManager {
	workers := opts.Workers
	if workers <= 0 {
		workers = 2
	}

	queueCap := opts.QueueCapacity
	if queueCap <= 0 {
		queueCap = 100
	}

	cloner := opts.Cloner
	if cloner == nil {
		cloner = git.CloneRepository
	}

	dfBuilder := opts.DockerfileBuilder
	if dfBuilder == nil && opts.DockerClient != nil {
		dfBuilder = NewDockerfileBuilder(opts.DockerClient)
	}

	npBuilder := opts.NixpacksBuilder
	if npBuilder == nil && opts.DockerClient != nil {
		if dfb, ok := dfBuilder.(*DockerfileBuilder); ok {
			npBuilder = NewNixpacksBuilder(opts.DockerClient, dfb)
		} else {
			npBuilder = NewNixpacksBuilder(opts.DockerClient, nil)
		}
	}

	workDir := opts.WorkDirRoot
	if workDir == "" {
		workDir = filepath.Join(os.TempDir(), "pikpik-builds")
	}

	logsDir := opts.LogsDir
	if logsDir == "" {
		logsDir = filepath.Join(workDir, "logs")
	}
	_ = os.MkdirAll(logsDir, 0755)

	regHost := opts.RegistryHost
	if regHost == "" {
		regHost = "127.0.0.1:5000"
	}
	regHost = strings.TrimSuffix(regHost, "/")

	bm := &BuildManager{
		st:                opts.Store,
		dockerCli:         opts.DockerClient,
		dockerfileBuilder: dfBuilder,
		nixpacksBuilder:   npBuilder,
		githubClient:      opts.GitHubClient,
		deployer:          opts.Deployer,
		broadcaster:       opts.Broadcaster,
		cloner:            cloner,
		registryHost:      regHost,
		registryAuth:      opts.RegistryAuth,
		imagePusher:       opts.ImagePusher,
		workDirRoot:       workDir,
		logsDir:           logsDir,
		queue:             make(chan *BuildJob, queueCap),
		cancels:           make(map[string]context.CancelFunc),
		workers:           workers,
		closeChan:         make(chan struct{}),
	}

	// Start worker pool
	for i := 0; i < workers; i++ {
		bm.wg.Add(1)
		go bm.workerLoop(i)
	}

	return bm
}

// Enqueue registers a build job in the store and pushes it to the worker queue.
func (bm *BuildManager) Enqueue(ctx context.Context, job *BuildJob) error {
	bm.mu.RLock()
	if bm.closed {
		bm.mu.RUnlock()
		return ErrManagerClosed
	}
	bm.mu.RUnlock()

	if job.ID == "" {
		job.ID = store.NewID("bld")
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	if job.Status == "" {
		job.Status = StatusQueued
	}

	regHost := bm.registryHost
	if regHost == "" {
		regHost = "127.0.0.1:5000"
	}
	regHost = strings.TrimSuffix(regHost, "/")

	if job.ImageTag == "" {
		shortSHA := job.CommitSHA
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		if shortSHA == "" {
			shortSHA = job.ID
		}
		appName := job.AppID
		if appName == "" {
			appName = "app"
		}
		job.ImageTag = fmt.Sprintf("%s/pikpik/%s:%s", regHost, appName, shortSHA)
	} else if !strings.Contains(job.ImageTag, "/") || strings.HasPrefix(job.ImageTag, "pikpik/") {
		job.ImageTag = fmt.Sprintf("%s/%s", regHost, strings.TrimPrefix(job.ImageTag, "/"))
	}
	job.Options.ImageTag = job.ImageTag

	// Persist initial build record in database
	if bm.st != nil {
		bldRecord := &store.Build{
			ID:            job.ID,
			ServiceID:     job.AppID,
			DeploymentID:  job.DeploymentID,
			RepoURL:       job.RepoURL,
			Branch:        job.Branch,
			CommitSHA:     job.CommitSHA,
			CommitMessage: job.CommitMessage,
			Author:        job.Author,
			Status:        string(StatusQueued),
			ImageTag:      job.ImageTag,
			LogsPath:      filepath.Join(bm.logsDir, job.ID+".log"),
			StartedAt:     job.CreatedAt,
		}
		if err := bm.st.Builds().Create(ctx, bldRecord); err != nil {
			return fmt.Errorf("build: failed to record build in store: %w", err)
		}
	}

	if bm.broadcaster != nil {
		bm.broadcaster.Broadcast("events", job.ID, "build_status", map[string]string{
			"build_id": job.ID,
			"app_id":   job.AppID,
			"status":   string(StatusQueued),
		})
	}

	select {
	case bm.queue <- job:
		return nil
	default:
		return ErrQueueFull
	}
}

// Cancel terminates a running or queued build job.
func (bm *BuildManager) Cancel(ctx context.Context, buildID string) error {
	bm.mu.Lock()
	cancel, exists := bm.cancels[buildID]
	if exists {
		delete(bm.cancels, buildID)
	}
	bm.mu.Unlock()

	if exists && cancel != nil {
		cancel()
	}

	now := time.Now().UTC()
	if bm.st != nil {
		_ = bm.st.Builds().UpdateStatus(ctx, buildID, string(StatusCancelled), &now, 0, "Build cancelled by user", "")
	}

	if bm.broadcaster != nil {
		bm.broadcaster.Broadcast("events", buildID, "build_status", map[string]string{
			"build_id": buildID,
			"status":   string(StatusCancelled),
		})
	}

	return nil
}

// Close gracefully terminates the BuildManager and waits for active worker goroutines to exit.
func (bm *BuildManager) Close() error {
	bm.closeOnce.Do(func() {
		bm.mu.Lock()
		bm.closed = true
		close(bm.closeChan)
		close(bm.queue)

		for _, cancel := range bm.cancels {
			if cancel != nil {
				cancel()
			}
		}
		bm.cancels = make(map[string]context.CancelFunc)
		bm.mu.Unlock()

		bm.wg.Wait()
	})
	return nil
}

// workerLoop continuously processes incoming build jobs from the queue channel.
func (bm *BuildManager) workerLoop(workerID int) {
	defer bm.wg.Done()

	for {
		select {
		case <-bm.closeChan:
			return
		case job, ok := <-bm.queue:
			if !ok {
				return
			}
			bm.executeJob(job)
		}
	}
}

// executeJob orchestrates the end-to-end build workflow.
func (bm *BuildManager) executeJob(job *BuildJob) {
	jobCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bm.mu.Lock()
	bm.cancels[job.ID] = cancel
	bm.mu.Unlock()

	defer func() {
		bm.mu.Lock()
		delete(bm.cancels, job.ID)
		bm.mu.Unlock()
	}()

	job.StartedAt = time.Now().UTC()

	// Check if already cancelled while waiting in queue
	if bm.st != nil {
		if cur, err := bm.st.Builds().GetByID(jobCtx, job.ID); err == nil && cur != nil && cur.Status == string(StatusCancelled) {
			return
		}
	}

	// 1. Setup Log Writer & File
	logsPath := filepath.Join(bm.logsDir, job.ID+".log")
	job.LogsPath = logsPath
	logFile, _ := os.OpenFile(logsPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if logFile != nil {
		defer logFile.Close()
	}

	logCb := func(line string) {
		lineWithNewline := line
		if !strings.HasSuffix(lineWithNewline, "\n") {
			lineWithNewline += "\n"
		}
		if logFile != nil {
			_, _ = logFile.WriteString(lineWithNewline)
		}
		if bm.broadcaster != nil {
			bm.broadcaster.Broadcast("logs", job.ID, "log_line", line)
			if job.AppID != "" {
				bm.broadcaster.Broadcast("logs", job.AppID, "log_line", line)
			}
		}
		if job.LogCallback != nil {
			job.LogCallback(line)
		}
	}

	// 2. Record build status as 'cloning'
	job.Status = StatusCloning
	if bm.st != nil {
		_ = bm.st.Builds().UpdateStatus(jobCtx, job.ID, string(StatusCloning), nil, 0, "", "")
	}
	if bm.broadcaster != nil {
		bm.broadcaster.Broadcast("events", job.ID, "build_status", map[string]string{"status": string(StatusCloning)})
	}

	// 3. Post GitHub commit status 'pending'
	bm.postCommitStatus(jobCtx, job, "pending", "Building container image from source...")

	// 4. Shallow clone repo via pkg/git.CloneRepository
	logCb(fmt.Sprintf("[git] Cloning repository %s (branch: %s, commit: %s)...", job.RepoURL, job.Branch, job.CommitSHA))

	workDir := filepath.Join(bm.workDirRoot, job.AppID, job.ID)
	cloneOpts := git.CloneOptions{
		RepoURL:       job.RepoURL,
		Branch:        job.Branch,
		CommitSHA:     job.CommitSHA,
		Depth:         1,
		Token:         job.GitToken,
		SSHPrivateKey: job.SSHPrivateKey,
		WorkDir:       workDir,
		AppID:         job.AppID,
		BuildID:       job.ID,
	}

	ws, err := bm.cloner(jobCtx, cloneOpts)
	if err != nil {
		bm.failBuild(jobCtx, job, fmt.Errorf("git clone failed: %w", err), logCb)
		return
	}
	// Always clean up ephemeral workspace on completion
	defer func() {
		_ = ws.Cleanup()
	}()

	// 5. Auto-detect build strategy (Dockerfile vs. Compose vs. Nixpacks)
	strategy := job.Strategy
	if strategy == "" || strategy == StrategyAuto {
		strategy = DetectStrategy(ws.Path, job.Options.DockerfilePath)
	}
	job.Strategy = strategy
	job.Options.Strategy = strategy
	logCb(fmt.Sprintf("[build] Detected build archetype: %s", strategy))

	// 6. Record status as 'building'
	job.Status = StatusBuilding
	if bm.st != nil {
		_ = bm.st.Builds().UpdateStatus(jobCtx, job.ID, string(StatusBuilding), nil, 0, "", "")
	}
	if bm.broadcaster != nil {
		bm.broadcaster.Broadcast("events", job.ID, "build_status", map[string]string{
			"status":   string(StatusBuilding),
			"strategy": string(strategy),
		})
	}

	// 7. Execute Build
	var buildRes *BuildResult
	var buildErr error

	switch strategy {
	case StrategyDockerfile:
		if bm.dockerfileBuilder != nil {
			buildRes, buildErr = bm.dockerfileBuilder.Build(jobCtx, ws.Path, job.Options, logCb)
		} else {
			buildErr = errors.New("dockerfile builder not configured")
		}
	case StrategyNixpacks:
		if bm.nixpacksBuilder != nil {
			buildRes, buildErr = bm.nixpacksBuilder.Build(jobCtx, ws.Path, job.Options, logCb)
		} else {
			buildErr = errors.New("nixpacks builder not configured")
		}
	case StrategyCompose:
		// Compose builder falls back to Dockerfile or custom stack deployment
		if bm.dockerfileBuilder != nil {
			buildRes, buildErr = bm.dockerfileBuilder.Build(jobCtx, ws.Path, job.Options, logCb)
		} else {
			buildErr = errors.New("compose builder not configured")
		}
	default:
		if bm.dockerfileBuilder != nil {
			buildRes, buildErr = bm.dockerfileBuilder.Build(jobCtx, ws.Path, job.Options, logCb)
		} else {
			buildErr = fmt.Errorf("unsupported build strategy: %s", strategy)
		}
	}

	if buildErr != nil {
		bm.failBuild(jobCtx, job, buildErr, logCb)
		return
	}

	imageTag := job.ImageTag
	if buildRes != nil && buildRes.ImageTag != "" {
		imageTag = buildRes.ImageTag
	}

	// Push image to registry so multi-node Swarm workers can pull images across nodes
	if bm.imagePusher != nil {
		logCb(fmt.Sprintf("[registry] Pushing image %s to registry...", imageTag))
		if err := bm.imagePusher.Push(jobCtx, imageTag, bm.registryAuth, logCb); err != nil {
			bm.failBuild(jobCtx, job, fmt.Errorf("registry push failed: %w", err), logCb)
			return
		}
		logCb(fmt.Sprintf("[registry] Successfully pushed %s to registry.", imageTag))
	} else if bm.dockerCli != nil {
		logCb(fmt.Sprintf("[registry] Pushing image %s to registry...", imageTag))
		pushOpts := imageTypes.PushOptions{
			RegistryAuth: bm.registryAuth,
		}
		resp, err := bm.dockerCli.ImagePush(jobCtx, imageTag, pushOpts)
		if err != nil {
			bm.failBuild(jobCtx, job, fmt.Errorf("registry push failed: %w", err), logCb)
			return
		}
		defer resp.Close()
		if _, err := ParseDockerBuildOutput(resp, logCb); err != nil {
			bm.failBuild(jobCtx, job, fmt.Errorf("registry push failed: %w", err), logCb)
			return
		}
		logCb(fmt.Sprintf("[registry] Successfully pushed %s to registry.", imageTag))
	}

	// 8. On success: record status as 'deploying' and trigger zero-downtime rollout
	job.Status = StatusDeploying
	if bm.st != nil {
		_ = bm.st.Builds().UpdateStatus(jobCtx, job.ID, string(StatusDeploying), nil, 0, "", imageTag)
	}
	if bm.broadcaster != nil {
		bm.broadcaster.Broadcast("events", job.ID, "build_status", map[string]string{"status": string(StatusDeploying)})
	}

	if bm.deployer != nil && job.AppID != "" {
		logCb(fmt.Sprintf("[deploy] Deploying new image %s to app %s...", imageTag, job.AppID))
		if err := bm.deployer.DeployApp(jobCtx, job.AppID, imageTag); err != nil {
			logCb(fmt.Sprintf("[deploy] WARNING: Deployment trigger returned error: %v", err))
		} else {
			logCb("[deploy] Zero-downtime deployment successfully initiated.")
		}
	}

	if bm.st != nil && job.AppID != "" {
		if svc, err := bm.st.Services().GetByID(jobCtx, job.AppID); err == nil && svc != nil {
			svc.Image = imageTag
			svc.Status = "running"
			svc.UpdatedAt = time.Now().UTC()
			_ = bm.st.Services().Update(jobCtx, svc)
		}
	}

	// 9. Update status as 'success' and post GitHub commit status 'success'
	now := time.Now().UTC()
	job.FinishedAt = &now
	job.DurationMS = now.Sub(job.StartedAt).Milliseconds()
	job.Status = StatusSuccess

	if bm.st != nil {
		_ = bm.st.Builds().UpdateStatus(jobCtx, job.ID, string(StatusSuccess), &now, job.DurationMS, "", imageTag)
	}

	bm.postCommitStatus(jobCtx, job, "success", fmt.Sprintf("Build & rollout completed in %dms", job.DurationMS))

	if bm.broadcaster != nil {
		bm.broadcaster.Broadcast("events", job.ID, "build_finished", map[string]any{
			"status":      string(StatusSuccess),
			"image_tag":   imageTag,
			"duration_ms": job.DurationMS,
		})
	}
	logCb(fmt.Sprintf("[build] Completed successfully in %dms. Image tag: %s", job.DurationMS, imageTag))
}

// failBuild handles errors during any build phase, recording failures and updating commit status.
func (bm *BuildManager) failBuild(ctx context.Context, job *BuildJob, err error, logCb LogCallback) {
	now := time.Now().UTC()
	job.FinishedAt = &now
	job.DurationMS = now.Sub(job.StartedAt).Milliseconds()
	job.Status = StatusFailed
	job.ErrorMessage = err.Error()

	if logCb != nil {
		logCb(fmt.Sprintf("[build] ERROR: %s", err.Error()))
	}

	if bm.st != nil {
		_ = bm.st.Builds().UpdateStatus(ctx, job.ID, string(StatusFailed), &now, job.DurationMS, job.ErrorMessage, "")
	}

	bm.postCommitStatus(ctx, job, "failure", fmt.Sprintf("Build failed: %s", job.ErrorMessage))

	if bm.broadcaster != nil {
		bm.broadcaster.Broadcast("events", job.ID, "build_finished", map[string]any{
			"status":      string(StatusFailed),
			"error":       job.ErrorMessage,
			"duration_ms": job.DurationMS,
		})
	}
}

// postCommitStatus safely sends a commit status update to GitHub if configured.
func (bm *BuildManager) postCommitStatus(ctx context.Context, job *BuildJob, state, description string) {
	if job.CommitSHA == "" || job.GitHubOwner == "" || job.GitHubRepo == "" {
		return
	}

	token := job.GitToken
	if token == "" && bm.githubClient != nil && job.GitHubInstallationID != 0 {
		tok, _, err := bm.githubClient.GetInstallationToken(ctx, job.GitHubInstallationID)
		if err == nil {
			token = tok
		}
	}

	if token != "" {
		_ = git.SetCommitStatus(ctx, token, job.GitHubOwner, job.GitHubRepo, job.CommitSHA, state, description, job.TargetURL)
	}
}

// DetectStrategy inspects repository file structure to choose the appropriate build strategy.
func DetectStrategy(dir, customDF string) BuildStrategy {
	if customDF != "" {
		if _, err := os.Stat(filepath.Join(dir, customDF)); err == nil {
			return StrategyDockerfile
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); err == nil {
		return StrategyDockerfile
	}
	if _, err := os.Stat(filepath.Join(dir, "dockerfile")); err == nil {
		return StrategyDockerfile
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err == nil {
		return StrategyCompose
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yaml")); err == nil {
		return StrategyCompose
	}
	if _, err := os.Stat(filepath.Join(dir, "compose.yml")); err == nil {
		return StrategyCompose
	}
	if _, err := os.Stat(filepath.Join(dir, "compose.yaml")); err == nil {
		return StrategyCompose
	}
	return StrategyNixpacks
}
