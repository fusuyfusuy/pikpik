package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrEmptyRepoURL is returned when no repository URL is provided.
	ErrEmptyRepoURL = errors.New("git: repo_url is required")

	// ErrCloneFailed is returned when git clone fails.
	ErrCloneFailed = errors.New("git: clone failed")

	// allowedGitURLSchemes is the set of transports CloneRepository is permitted to invoke.
	// Deliberately excludes git's alternate transport helpers (ext::, fd::) and file:// / bare
	// local paths, which can be abused for arbitrary command execution or local file disclosure
	// when opts.RepoURL originates from an untrusted source (e.g. a webhook payload).
	allowedGitURLSchemes = map[string]bool{
		"https": true,
		"http":  true,
		"ssh":   true,
	}

	// scpLikeSSHPattern matches the scp-like SSH shorthand git supports, e.g. git@github.com:org/repo.git.
	scpLikeSSHPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+@[A-Za-z0-9_.-]+:.+$`)
)

// validateRepoURL ensures rawURL uses an explicitly allowed git transport before it is ever
// passed to exec.CommandContext. See allowedGitURLSchemes for the rationale.
func validateRepoURL(rawURL string) error {
	if strings.Contains(rawURL, "://") {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return fmt.Errorf("git: invalid repo_url: %w", err)
		}
		if !allowedGitURLSchemes[strings.ToLower(parsed.Scheme)] {
			return fmt.Errorf("git: repo_url scheme %q is not allowed (allowed: https, http, ssh, or scp-like git@host:path)", parsed.Scheme)
		}
		return nil
	}

	if scpLikeSSHPattern.MatchString(rawURL) {
		return nil
	}

	return fmt.Errorf("git: repo_url %q must use https://, http://, ssh://, or scp-like git@host:path", rawURL)
}

// CloneRepository executes a typed git clone command using exec.CommandContext.
// Zero shell interpolation is performed. Supports HTTPS tokens and SSH deploy keys.
func CloneRepository(ctx context.Context, opts CloneOptions) (*Workspace, error) {
	if strings.TrimSpace(opts.RepoURL) == "" {
		return nil, ErrEmptyRepoURL
	}
	if err := validateRepoURL(opts.RepoURL); err != nil {
		return nil, err
	}

	workDir := opts.WorkDir
	if workDir == "" {
		if opts.AppID != "" && opts.BuildID != "" {
			workDir = filepath.Join("/var/lib/pikpik/builds", opts.AppID, opts.BuildID)
		} else if opts.BuildID != "" {
			workDir = filepath.Join("/var/lib/pikpik/builds", "default", opts.BuildID)
		} else {
			workDir = filepath.Join(os.TempDir(), "pikpik-builds", fmt.Sprintf("bld_%d", time.Now().UnixNano()))
		}
	}

	// Prepare parent directory and ensure clean target path
	if err := os.MkdirAll(filepath.Dir(workDir), 0755); err != nil {
		return nil, fmt.Errorf("git: failed to create parent build directory: %w", err)
	}
	_ = os.RemoveAll(workDir)

	cloneURL := opts.RepoURL
	if opts.Token != "" && (strings.HasPrefix(cloneURL, "https://") || strings.HasPrefix(cloneURL, "http://")) {
		if parsedURL, err := url.Parse(cloneURL); err == nil {
			parsedURL.User = url.UserPassword("x-access-token", opts.Token)
			cloneURL = parsedURL.String()
		}
	}

	// Validate branch to prevent flag injection
	branch := strings.TrimSpace(opts.Branch)
	if strings.HasPrefix(branch, "-") {
		return nil, fmt.Errorf("git: invalid branch name %q", branch)
	}

	args := []string{"clone"}
	depth := opts.Depth
	if depth <= 0 {
		depth = 1
	}
	args = append(args, fmt.Sprintf("--depth=%d", depth), "--single-branch")

	if branch != "" {
		args = append(args, "--branch", branch)
	}

	// Positional arguments separated with --
	args = append(args, "--", cloneURL, workDir)

	cleanedUp := false
	defer func() {
		if !cleanedUp {
			_ = os.RemoveAll(workDir)
		}
	}()

	cmd := exec.CommandContext(ctx, "git", args...)
	cmdEnv := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=http:https:ssh")

	// Handle SSH Deploy Key
	if strings.TrimSpace(opts.SSHPrivateKey) != "" {
		keyFile, err := os.CreateTemp("", "pikpik-ssh-key-*")
		if err != nil {
			return nil, fmt.Errorf("git: failed to create temp ssh key file: %w", err)
		}
		keyPath := keyFile.Name()
		defer os.Remove(keyPath)

		if err := os.WriteFile(keyPath, []byte(opts.SSHPrivateKey), 0600); err != nil {
			_ = keyFile.Close()
			return nil, fmt.Errorf("git: failed to write temp ssh key: %w", err)
		}
		_ = keyFile.Close()

		sshCmd := fmt.Sprintf("ssh -i %s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null", keyPath)
		cmdEnv = append(cmdEnv, "GIT_SSH_COMMAND="+sshCmd)
	}

	cmd.Env = cmdEnv

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errOut := maskSensitive(stderr.String(), opts.Token)
		return nil, fmt.Errorf("%w: %v: %s", ErrCloneFailed, err, strings.TrimSpace(errOut))
	}

	// Checkout specific commit SHA if requested and different from branch HEAD
	if opts.CommitSHA != "" {
		checkoutCmd := exec.CommandContext(ctx, "git", "-C", workDir, "checkout", opts.CommitSHA)
		checkoutCmd.Env = cmdEnv
		var checkoutErr bytes.Buffer
		checkoutCmd.Stderr = &checkoutErr

		if err := checkoutCmd.Run(); err != nil {
			// If shallow clone didn't contain the commit, fetch the specific SHA
			fetchCmd := exec.CommandContext(ctx, "git", "-C", workDir, "fetch", fmt.Sprintf("--depth=%d", depth), "origin", opts.CommitSHA)
			fetchCmd.Env = cmdEnv
			if fetchErr := fetchCmd.Run(); fetchErr == nil {
				_ = exec.CommandContext(ctx, "git", "-C", workDir, "checkout", opts.CommitSHA).Run()
			}
		}
	}

	cleanedUp = true
	return &Workspace{
		Path:    workDir,
		AppID:   opts.AppID,
		BuildID: opts.BuildID,
	}, nil
}

func maskSensitive(input, secret string) string {
	if secret == "" {
		return input
	}
	return strings.ReplaceAll(input, secret, "[REDACTED]")
}
