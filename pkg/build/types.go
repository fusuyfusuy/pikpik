package build

import (
	"context"
	"time"

	"github.com/fusuycorp/pikpik/pkg/git"
)

// BuildStrategy represents the archetype/mechanism used to compile and package source code into an OCI container.
type BuildStrategy string

const (
	StrategyDockerfile BuildStrategy = "dockerfile"
	StrategyCompose    BuildStrategy = "compose"
	StrategyNixpacks   BuildStrategy = "nixpacks"
	StrategyAuto       BuildStrategy = "auto"
)

// BuildStatus represents the current lifecycle execution state of a build.
type BuildStatus string

const (
	StatusQueued    BuildStatus = "queued"
	StatusCloning   BuildStatus = "cloning"
	StatusBuilding  BuildStatus = "building"
	StatusDeploying BuildStatus = "deploying"
	StatusSuccess   BuildStatus = "success"
	StatusFailed    BuildStatus = "failed"
	StatusCancelled BuildStatus = "cancelled"
)

// LogCallback is a streaming hook invoked for every line or chunk of build output.
type LogCallback func(line string)

// BuildOptions holds fine-grained build arguments, paths, and environment settings.
type BuildOptions struct {
	ImageTag       string            `json:"image_tag"`
	DockerfilePath string            `json:"dockerfile_path,omitempty"` // e.g. "Dockerfile" or "backend/Dockerfile"
	ContextDir     string            `json:"context_dir,omitempty"`     // root build context relative to repo root
	BuildArgs      map[string]string `json:"build_args,omitempty"`
	Target         string            `json:"target,omitempty"`
	NoCache        bool              `json:"no_cache,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Strategy       BuildStrategy     `json:"strategy,omitempty"`
}

// BuildJob represents a unit of build work to be queued, executed, and tracked.
type BuildJob struct {
	ID                   string            `json:"id"`
	AppID                string            `json:"app_id"` // service_id in store
	DeploymentID         string            `json:"deployment_id,omitempty"`
	RepoURL              string            `json:"repo_url"`
	Branch               string            `json:"branch"`
	CommitSHA            string            `json:"commit_sha,omitempty"`
	CommitMessage        string            `json:"commit_message,omitempty"`
	Author               string            `json:"author,omitempty"`
	AuthorEmail          string            `json:"author_email,omitempty"`
	Strategy             BuildStrategy     `json:"strategy,omitempty"`
	Status               BuildStatus       `json:"status"`
	ImageTag             string            `json:"image_tag,omitempty"`
	Options              BuildOptions      `json:"options"`
	GitToken             string            `json:"git_token,omitempty"`
	SSHPrivateKey        string            `json:"ssh_private_key,omitempty"`
	GitHubInstallationID int64             `json:"github_installation_id,omitempty"`
	GitHubOwner          string            `json:"github_owner,omitempty"`
	GitHubRepo           string            `json:"github_repo,omitempty"`
	TargetURL            string            `json:"target_url,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
	StartedAt            time.Time         `json:"started_at,omitempty"`
	FinishedAt           *time.Time        `json:"finished_at,omitempty"`
	DurationMS           int64             `json:"duration_ms,omitempty"`
	ErrorMessage         string            `json:"error_message,omitempty"`
	LogsPath             string            `json:"logs_path,omitempty"`
	LogCallback          LogCallback       `json:"-"`
}

// BuildResult encapsulates the outcome of a successful image build.
type BuildResult struct {
	ImageTag string        `json:"image_tag"`
	Strategy BuildStrategy `json:"strategy"`
	Duration time.Duration `json:"duration"`
	ImageID  string        `json:"image_id,omitempty"`
	LogsPath string        `json:"logs_path,omitempty"`
}

// Builder is the common interface implemented by individual packaging strategies.
type Builder interface {
	Build(ctx context.Context, srcDir string, opts BuildOptions, logCb LogCallback) (*BuildResult, error)
}

// ImagePusher defines the contract for pushing built images to an OCI registry.
type ImagePusher interface {
	Push(ctx context.Context, imageTag, auth string, logCb LogCallback) error
}

// GitCloner defines the contract for checking out source repositories.
type GitCloner func(ctx context.Context, opts git.CloneOptions) (*git.Workspace, error)

// DockerProgress represents Docker progress frames.
type DockerProgress struct {
	Current int64 `json:"current,omitempty"`
	Total   int64 `json:"total,omitempty"`
	Start   int64 `json:"start,omitempty"`
}

// DockerErrorDetail contains detailed error information from Docker build frames.
type DockerErrorDetail struct {
	Message string `json:"message"`
	Code    int    `json:"code,omitempty"`
}

// DockerJSONMessage represents a single JSON line stream frame from the Docker Engine / BuildKit API.
type DockerJSONMessage struct {
	Stream      string             `json:"stream,omitempty"`
	Status      string             `json:"status,omitempty"`
	Progress    *DockerProgress    `json:"progressDetail,omitempty"`
	ID          string             `json:"id,omitempty"`
	From        string             `json:"from,omitempty"`
	Time        int64              `json:"time,omitempty"`
	TimeNano    int64              `json:"timeNano,omitempty"`
	Error       string             `json:"error,omitempty"`
	ErrorDetail *DockerErrorDetail `json:"errorDetail,omitempty"`
	Aux         map[string]any     `json:"aux,omitempty"`
}
