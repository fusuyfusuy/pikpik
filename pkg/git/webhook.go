package git

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	// ErrInvalidSignature is returned when a webhook signature fails verification.
	ErrInvalidSignature = errors.New("git: invalid webhook signature")

	// ErrEmptyPayload is returned when a webhook payload is empty.
	ErrEmptyPayload = errors.New("git: empty webhook payload")

	// ErrInvalidPayload is returned when a webhook payload cannot be parsed.
	ErrInvalidPayload = errors.New("git: invalid webhook payload")
)

// VerifyGitHubSignature validates an X-Hub-Signature-256 HMAC-SHA256 signature
// using constant-time comparison to prevent timing side-channel attacks.
func VerifyGitHubSignature(secret string, payload []byte, signatureHeader string) bool {
	if secret == "" || signatureHeader == "" || len(payload) == 0 {
		return false
	}

	// GitHub signature header is prefixed with "sha256="
	var sigHex string
	if strings.HasPrefix(signatureHeader, "sha256=") {
		sigHex = strings.TrimPrefix(signatureHeader, "sha256=")
	} else if strings.HasPrefix(signatureHeader, "sha256:") {
		sigHex = strings.TrimPrefix(signatureHeader, "sha256:")
	} else {
		sigHex = signatureHeader
	}

	expectedMAC, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	actualMAC := mac.Sum(nil)

	if len(actualMAC) != len(expectedMAC) {
		return false
	}

	return subtle.ConstantTimeCompare(actualMAC, expectedMAC) == 1
}

type githubPushPayload struct {
	Ref        string `json:"ref"`
	Before     string `json:"before"`
	After      string `json:"after"`
	Deleted    bool   `json:"deleted"`
	HeadCommit *struct {
		ID        string `json:"id"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
		Author    struct {
			Name     string `json:"name"`
			Email    string `json:"email"`
			Username string `json:"username"`
		} `json:"author"`
	} `json:"head_commit"`
	Repository struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		CloneURL      string `json:"clone_url"`
		SSHURL        string `json:"ssh_url"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		} `json:"owner"`
	} `json:"repository"`
	Pusher struct {
		Name  string `json:"name"`
		Email string `json:"email"`
	} `json:"pusher"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
	Installation *struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// ParseGitHubPushEvent extracts normalized PushEvent metadata from a GitHub webhook push payload.
func ParseGitHubPushEvent(payload []byte) (*PushEvent, error) {
	if len(payload) == 0 {
		return nil, ErrEmptyPayload
	}

	var gh githubPushPayload
	if err := json.Unmarshal(payload, &gh); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
	}

	// Extract branch from ref (e.g. "refs/heads/main" -> "main")
	branch := gh.Ref
	if strings.HasPrefix(branch, "refs/heads/") {
		branch = strings.TrimPrefix(branch, "refs/heads/")
	} else if strings.HasPrefix(branch, "refs/tags/") {
		branch = strings.TrimPrefix(branch, "refs/tags/")
	}

	commitSHA := gh.After
	if (commitSHA == "" || commitSHA == "0000000000000000000000000000000000000000") && gh.HeadCommit != nil {
		commitSHA = gh.HeadCommit.ID
	}

	var commitMsg, author, authorEmail string
	var pushedTime time.Time

	if gh.HeadCommit != nil {
		commitMsg = gh.HeadCommit.Message
		author = gh.HeadCommit.Author.Name
		if author == "" {
			author = gh.HeadCommit.Author.Username
		}
		authorEmail = gh.HeadCommit.Author.Email
		if gh.HeadCommit.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, gh.HeadCommit.Timestamp); err == nil {
				pushedTime = t
			}
		}
	}

	if author == "" {
		author = gh.Pusher.Name
	}
	if authorEmail == "" {
		authorEmail = gh.Pusher.Email
	}

	var installationID int64
	if gh.Installation != nil {
		installationID = gh.Installation.ID
	}

	repoName := gh.Repository.FullName
	if repoName == "" {
		repoName = gh.Repository.Name
	}

	event := &PushEvent{
		Repository:     repoName,
		Branch:         branch,
		Ref:            gh.Ref,
		CommitSHA:      commitSHA,
		CommitMessage:  commitMsg,
		Author:         author,
		AuthorEmail:    authorEmail,
		Sender:         gh.Sender.Login,
		CloneURL:       gh.Repository.CloneURL,
		SSHURL:         gh.Repository.SSHURL,
		InstallationID: installationID,
		PushedAt:       pushedTime,
	}

	return event, nil
}

type genericJSONPayload struct {
	Repository    string `json:"repository"`
	RepoURL       string `json:"repo_url"`
	URL           string `json:"url"`
	Branch        string `json:"branch"`
	Ref           string `json:"ref"`
	CommitSHA     string `json:"commit_sha"`
	SHA           string `json:"sha"`
	After         string `json:"after"`
	CheckoutSHA   string `json:"checkout_sha"`
	CommitMessage string `json:"commit_message"`
	Message       string `json:"message"`
	Author        string `json:"author"`
	AuthorEmail   string `json:"author_email"`
	UserName      string `json:"user_name"`
	UserEmail     string `json:"user_email"`
	Sender        string `json:"sender"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	Project       *struct {
		GitHTTPURL string `json:"git_http_url"`
		GitSSHURL  string `json:"git_ssh_url"`
		Name       string `json:"name"`
		Path       string `json:"path_with_namespace"`
	} `json:"project"`
}

// ParseGenericGitPush parses a push webhook event from a generic Git provider or webhook payload.
// It supports application/json payloads, application/x-www-form-urlencoded, and query parameters.
func ParseGenericGitPush(r *http.Request) (*PushEvent, error) {
	if r == nil {
		return nil, errors.New("git: nil request")
	}

	ct := r.Header.Get("Content-Type")

	var bodyBytes []byte
	if r.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(io.LimitReader(r.Body, 5*1024*1024))
		if err != nil {
			return nil, fmt.Errorf("git: failed to read request body: %w", err)
		}
	}

	event := &PushEvent{
		PushedAt: time.Now().UTC(),
	}

	if len(bodyBytes) > 0 && (strings.Contains(ct, "application/json") || bodyBytes[0] == '{') {
		var gen genericJSONPayload
		if err := json.Unmarshal(bodyBytes, &gen); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidPayload, err)
		}

		event.Branch = gen.Branch
		if event.Branch == "" && gen.Ref != "" {
			event.Branch = gen.Ref
		}
		if event.Branch != "" {
			event.Branch = strings.TrimPrefix(event.Branch, "refs/heads/")
			event.Branch = strings.TrimPrefix(event.Branch, "refs/tags/")
		}
		event.Ref = gen.Ref
		if event.Ref == "" && event.Branch != "" {
			event.Ref = "refs/heads/" + event.Branch
		}

		event.CommitSHA = gen.CommitSHA
		if event.CommitSHA == "" {
			event.CommitSHA = gen.SHA
		}
		if event.CommitSHA == "" {
			event.CommitSHA = gen.After
		}
		if event.CommitSHA == "" {
			event.CommitSHA = gen.CheckoutSHA
		}

		event.CommitMessage = gen.CommitMessage
		if event.CommitMessage == "" {
			event.CommitMessage = gen.Message
		}

		event.Author = gen.Author
		if event.Author == "" {
			event.Author = gen.UserName
		}

		event.AuthorEmail = gen.AuthorEmail
		if event.AuthorEmail == "" {
			event.AuthorEmail = gen.UserEmail
		}

		event.Sender = gen.Sender

		event.CloneURL = gen.CloneURL
		if event.CloneURL == "" {
			event.CloneURL = gen.RepoURL
		}
		if event.CloneURL == "" {
			event.CloneURL = gen.URL
		}
		if event.CloneURL == "" && gen.Project != nil {
			event.CloneURL = gen.Project.GitHTTPURL
		}

		event.SSHURL = gen.SSHURL
		if event.SSHURL == "" && gen.Project != nil {
			event.SSHURL = gen.Project.GitSSHURL
		}

		event.Repository = gen.Repository
		if event.Repository == "" && gen.Project != nil {
			if gen.Project.Path != "" {
				event.Repository = gen.Project.Path
			} else {
				event.Repository = gen.Project.Name
			}
		}
	}

	// Fallback or override from URL query parameters / form values
	if event.Branch == "" {
		event.Branch = r.URL.Query().Get("branch")
		if event.Branch == "" {
			event.Branch = r.URL.Query().Get("ref")
		}
	}
	if event.Branch != "" {
		event.Branch = strings.TrimPrefix(event.Branch, "refs/heads/")
		event.Branch = strings.TrimPrefix(event.Branch, "refs/tags/")
	}
	if event.Ref == "" {
		event.Ref = r.URL.Query().Get("ref")
	}
	if event.CommitSHA == "" {
		event.CommitSHA = r.URL.Query().Get("commit_sha")
		if event.CommitSHA == "" {
			event.CommitSHA = r.URL.Query().Get("sha")
		}
	}
	if event.Repository == "" {
		event.Repository = r.URL.Query().Get("repository")
		if event.Repository == "" {
			event.Repository = r.URL.Query().Get("repo")
		}
	}
	if event.CloneURL == "" {
		event.CloneURL = r.URL.Query().Get("clone_url")
		if event.CloneURL == "" {
			event.CloneURL = r.URL.Query().Get("repo_url")
		}
	}
	if event.Author == "" {
		event.Author = r.URL.Query().Get("author")
	}
	if event.CommitMessage == "" {
		event.CommitMessage = r.URL.Query().Get("message")
	}

	if event.Branch == "" && event.CommitSHA == "" && event.Repository == "" && event.CloneURL == "" {
		return nil, ErrEmptyPayload
	}

	if event.Branch == "" {
		event.Branch = "main"
	}
	if event.Ref == "" {
		event.Ref = "refs/heads/" + event.Branch
	}

	return event, nil
}
