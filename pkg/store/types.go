package store

import (
	"time"
)

// Build represents a git repository build execution record.
type Build struct {
	ID            string     `json:"id"`
	ServiceID     string     `json:"service_id"`
	DeploymentID  string     `json:"deployment_id,omitempty"`
	RepoURL       string     `json:"repo_url"`
	Branch        string     `json:"branch"`
	CommitSHA     string     `json:"commit_sha"`
	CommitMessage string     `json:"commit_message,omitempty"`
	Author        string     `json:"author,omitempty"`
	Status        string     `json:"status"` // "queued", "cloning", "building", "pushing", "success", "failed", "cancelled"
	LogsPath      string     `json:"logs_path,omitempty"`
	ImageTag      string     `json:"image_tag,omitempty"`
	ErrorMessage  string     `json:"error_message,omitempty"`
	DurationMS    int64      `json:"duration_ms"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

// GitHubInstallation represents an installed GitHub App instance mapped to an organization.
type GitHubInstallation struct {
	ID                  string    `json:"id"`
	OrgID               string    `json:"org_id"`
	InstallationID      int64     `json:"installation_id"`
	AccountName         string    `json:"account_name"`
	AccountType         string    `json:"account_type"`          // "User", "Organization"
	RepositorySelection string    `json:"repository_selection"` // "all", "selected"
	Permissions         string    `json:"permissions"`          // JSON string
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}
