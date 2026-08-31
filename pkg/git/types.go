package git

import (
	"os"
	"time"
)

// PushEvent represents normalized information extracted from a git push webhook event.
type PushEvent struct {
	Repository     string    `json:"repository"`
	Branch         string    `json:"branch"`
	Ref            string    `json:"ref"`
	CommitSHA      string    `json:"commit_sha"`
	CommitMessage  string    `json:"commit_message"`
	Author         string    `json:"author"`
	AuthorEmail    string    `json:"author_email"`
	Sender         string    `json:"sender"`
	CloneURL       string    `json:"clone_url"`
	SSHURL         string    `json:"ssh_url"`
	InstallationID int64     `json:"installation_id,omitempty"`
	PushedAt       time.Time `json:"pushed_at,omitempty"`
}

// GitHubAppConfig encapsulates credentials and configuration needed to authenticate as a GitHub App.
type GitHubAppConfig struct {
	AppID          int64  `json:"app_id"`
	PrivateKey     []byte `json:"private_key"`
	WebhookSecret  string `json:"webhook_secret"`
	InstallationID int64  `json:"installation_id,omitempty"`
	BaseURL        string `json:"base_url,omitempty"`
}

// CommitStatus defines the status state and metadata posted back to a Git provider.
type CommitStatus struct {
	State       string `json:"state"` // "pending", "success", "error", "failure"
	TargetURL   string `json:"target_url,omitempty"`
	Description string `json:"description,omitempty"`
	Context     string `json:"context,omitempty"`
}

// CloneOptions specifies arguments and authentication credentials for repository cloning.
type CloneOptions struct {
	RepoURL       string `json:"repo_url"`
	Branch        string `json:"branch"`
	CommitSHA     string `json:"commit_sha,omitempty"`
	Depth         int    `json:"depth,omitempty"`
	Token         string `json:"token,omitempty"`
	SSHPrivateKey string `json:"ssh_private_key,omitempty"`
	WorkDir       string `json:"work_dir,omitempty"`
	AppID         string `json:"app_id,omitempty"`
	BuildID       string `json:"build_id,omitempty"`
	AllowLocal    bool   `json:"allow_local,omitempty"`
}

// Workspace represents an isolated build workspace directory on the host filesystem.
type Workspace struct {
	Path    string `json:"path"`
	AppID   string `json:"app_id,omitempty"`
	BuildID string `json:"build_id,omitempty"`
}

// Cleanup deletes the workspace directory and all contained files from disk.
// It is idempotent and safe to call multiple times or when the directory is already deleted.
func (ws *Workspace) Cleanup() error {
	if ws == nil || ws.Path == "" {
		return nil
	}
	return os.RemoveAll(ws.Path)
}
