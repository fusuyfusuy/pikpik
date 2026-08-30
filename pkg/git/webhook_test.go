package git_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/git"
)

func TestVerifyGitHubSignature(t *testing.T) {
	secret := "super-secret-token"
	payload := []byte(`{"ref":"refs/heads/main","after":"c0ffee"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	tests := []struct {
		name      string
		secret    string
		payload   []byte
		sigHeader string
		expected  bool
	}{
		{
			name:      "Valid signature",
			secret:    secret,
			payload:   payload,
			sigHeader: validSig,
			expected:  true,
		},
		{
			name:      "Valid signature with colon",
			secret:    secret,
			payload:   payload,
			sigHeader: "sha256:" + hex.EncodeToString(mac.Sum(nil)),
			expected:  true,
		},
		{
			name:      "Wrong secret",
			secret:    "wrong-secret",
			payload:   payload,
			sigHeader: validSig,
			expected:  false,
		},
		{
			name:      "Tampered payload",
			secret:    secret,
			payload:   []byte(`{"ref":"refs/heads/main","after":"tampered"}`),
			sigHeader: validSig,
			expected:  false,
		},
		{
			name:      "Malformed signature format",
			secret:    secret,
			payload:   payload,
			sigHeader: "invalid_sig",
			expected:  false,
		},
		{
			name:      "Empty secret",
			secret:    "",
			payload:   payload,
			sigHeader: validSig,
			expected:  false,
		},
		{
			name:      "Empty payload",
			secret:    secret,
			payload:   nil,
			sigHeader: validSig,
			expected:  false,
		},
		{
			name:      "Empty signature header",
			secret:    secret,
			payload:   payload,
			sigHeader: "",
			expected:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := git.VerifyGitHubSignature(tc.secret, tc.payload, tc.sigHeader)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestParseGitHubPushEvent(t *testing.T) {
	rawJSON := `{
		"ref": "refs/heads/main",
		"after": "1a2b3c4d5e6f",
		"head_commit": {
			"id": "1a2b3c4d5e6f",
			"message": "feat: add git integration",
			"timestamp": "2026-08-30T15:04:05Z",
			"author": {
				"name": "Fusuy Dev",
				"email": "dev@pikpik.io",
				"username": "fusuydev"
			}
		},
		"repository": {
			"id": 12345678,
			"name": "pikpik",
			"full_name": "fusuycorp/pikpik",
			"clone_url": "https://github.com/fusuycorp/pikpik.git",
			"ssh_url": "git@github.com:fusuycorp/pikpik.git",
			"default_branch": "main"
		},
		"pusher": {
			"name": "fusuydev",
			"email": "dev@pikpik.io"
		},
		"sender": {
			"login": "fusuydev"
		},
		"installation": {
			"id": 98765
		}
	}`

	event, err := git.ParseGitHubPushEvent([]byte(rawJSON))
	if err != nil {
		t.Fatalf("unexpected error parsing github push event: %v", err)
	}

	if event.Repository != "fusuycorp/pikpik" {
		t.Errorf("expected repo fusuycorp/pikpik, got %q", event.Repository)
	}
	if event.Branch != "main" {
		t.Errorf("expected branch main, got %q", event.Branch)
	}
	if event.CommitSHA != "1a2b3c4d5e6f" {
		t.Errorf("expected commit SHA 1a2b3c4d5e6f, got %q", event.CommitSHA)
	}
	if event.CommitMessage != "feat: add git integration" {
		t.Errorf("expected commit message 'feat: add git integration', got %q", event.CommitMessage)
	}
	if event.Author != "Fusuy Dev" {
		t.Errorf("expected author 'Fusuy Dev', got %q", event.Author)
	}
	if event.AuthorEmail != "dev@pikpik.io" {
		t.Errorf("expected author email 'dev@pikpik.io', got %q", event.AuthorEmail)
	}
	if event.Sender != "fusuydev" {
		t.Errorf("expected sender 'fusuydev', got %q", event.Sender)
	}
	if event.CloneURL != "https://github.com/fusuycorp/pikpik.git" {
		t.Errorf("expected clone url, got %q", event.CloneURL)
	}
	if event.SSHURL != "git@github.com:fusuycorp/pikpik.git" {
		t.Errorf("expected ssh url, got %q", event.SSHURL)
	}
	if event.InstallationID != 98765 {
		t.Errorf("expected installation ID 98765, got %d", event.InstallationID)
	}
}

func TestParseGitHubPushEvent_DeletedBranch(t *testing.T) {
	rawJSON := `{
		"ref": "refs/heads/feature/old-branch",
		"after": "0000000000000000000000000000000000000000",
		"deleted": true,
		"repository": {
			"name": "pikpik",
			"full_name": "fusuycorp/pikpik",
			"clone_url": "https://github.com/fusuycorp/pikpik.git"
		}
	}`

	event, err := git.ParseGitHubPushEvent([]byte(rawJSON))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.Branch != "feature/old-branch" {
		t.Errorf("expected branch feature/old-branch, got %q", event.Branch)
	}
}

func TestParseGitHubPushEvent_EmptyPayload(t *testing.T) {
	_, err := git.ParseGitHubPushEvent(nil)
	if err == nil {
		t.Fatalf("expected error for nil payload")
	}
}

func TestParseGenericGitPush_JSON(t *testing.T) {
	payload := `{
		"repository": "custom/repo",
		"branch": "develop",
		"commit_sha": "abcdef123456",
		"commit_message": "chore: sync upstream",
		"author": "Generic Developer",
		"author_email": "gen@example.com",
		"clone_url": "https://git.internal.corp/custom/repo.git"
	}`

	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/generic", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")

	event, err := git.ParseGenericGitPush(req)
	if err != nil {
		t.Fatalf("failed to parse generic git push: %v", err)
	}

	if event.Repository != "custom/repo" {
		t.Errorf("expected repo custom/repo, got %q", event.Repository)
	}
	if event.Branch != "develop" {
		t.Errorf("expected branch develop, got %q", event.Branch)
	}
	if event.CommitSHA != "abcdef123456" {
		t.Errorf("expected commit sha, got %q", event.CommitSHA)
	}
	if event.CloneURL != "https://git.internal.corp/custom/repo.git" {
		t.Errorf("expected clone url, got %q", event.CloneURL)
	}
}

func TestParseGenericGitPush_QueryParams(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/generic?branch=staging&sha=fedcba654321&repo=my-app&clone_url=https://git.example.com/my-app.git", nil)

	event, err := git.ParseGenericGitPush(req)
	if err != nil {
		t.Fatalf("failed to parse generic git push query params: %v", err)
	}

	if event.Branch != "staging" {
		t.Errorf("expected branch staging, got %q", event.Branch)
	}
	if event.CommitSHA != "fedcba654321" {
		t.Errorf("expected sha fedcba654321, got %q", event.CommitSHA)
	}
	if event.Repository != "my-app" {
		t.Errorf("expected repo my-app, got %q", event.Repository)
	}
}
