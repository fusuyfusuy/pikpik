package deploy

import (
	"context"
	"net/http"
	"time"
)

// DeployNudgePayload defines the structure sent by CI webhooks to trigger redeployment.
type DeployNudgePayload struct {
	Image     string `json:"image"`               // mandatory, e.g. "registry.domain.com/app:sha-123"
	Tag       string `json:"tag,omitempty"`         // optional image tag
	CommitSha string `json:"commitSha,omitempty"`   // git commit SHA (40 hex chars)
	Branch    string `json:"branch,omitempty"`      // git branch name
	Message   string `json:"message,omitempty"`     // commit message
	Author    string `json:"author,omitempty"`      // author name/email
}

// NudgeTokenInfo contains decoded security metadata for a webhook token.
type NudgeTokenInfo struct {
	ID                string    `json:"id"`
	ProjectID         string    `json:"projectId"`
	ServiceID         string    `json:"serviceId"`
	TokenHash         string    `json:"-"`
	AllowedRegistries []string  `json:"allowedRegistries"`
	RateLimitPerMin   int       `json:"rateLimitPerMin"`
	IsActive          bool      `json:"isActive"`
	CreatedAt         time.Time `json:"createdAt"`
}

// NudgeResponse defines the immediate JSON response returned to CI upon receiving a valid webhook.
type NudgeResponse struct {
	DeploymentID string    `json:"deploymentId"`
	ServiceID    string    `json:"serviceId"`
	Status       string    `json:"status"` // "QUEUED", "IN_PROGRESS"
	QueuedAt     time.Time `json:"queuedAt"`
}

// HandlerOptions configures the DeployWebhookHandler rate limits and defaults.
type HandlerOptions struct {
	RateLimitPerMin int      `json:"rateLimitPerMin"`
	BurstLimit      int      `json:"burstLimit"`
	IPRateLimit     int      `json:"ipRateLimit"`
	IPBurstLimit    int      `json:"ipBurstLimit"`
	AllowedHosts    []string `json:"allowedHosts"`
}

// DeploymentDispatcher defines the callback invoked when a valid deploy nudge is received.
type DeploymentDispatcher interface {
	DispatchDeploy(ctx context.Context, serviceID string, payload DeployNudgePayload) (deploymentID string, err error)
}

// DeploymentDispatcherFunc is an adapter to allow the use of ordinary functions as DeploymentDispatcher.
type DeploymentDispatcherFunc func(ctx context.Context, serviceID string, payload DeployNudgePayload) (deploymentID string, err error)

// DispatchDeploy calls f(ctx, serviceID, payload).
func (f DeploymentDispatcherFunc) DispatchDeploy(ctx context.Context, serviceID string, payload DeployNudgePayload) (string, error) {
	return f(ctx, serviceID, payload)
}

// DeployWebhookHandler manages authenticated deployment nudge endpoints.
type DeployWebhookHandler interface {
	// ServeHTTP implements the http.Handler interface for POST /api/deploy/nudge/{token}.
	ServeHTTP(w http.ResponseWriter, r *http.Request)

	// GenerateToken creates a new webhook token for a target service.
	GenerateToken(ctx context.Context, serviceID, projectID string) (rawToken string, info *NudgeTokenInfo, err error)

	// RevokeToken invalidates an existing nudge webhook token.
	RevokeToken(ctx context.Context, tokenID string) error

	// ValidatePayload verifies payload JSON structure, size, and registry allowlist.
	ValidatePayload(payload *DeployNudgePayload, allowedRegistries []string) error
}
