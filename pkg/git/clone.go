package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
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
	allowedGitURLSchemes = map[string]bool{
		"https": true,
		"http":  true,
		"ssh":   true,
	}

	// scpLikeSSHPattern matches the scp-like SSH shorthand git supports, e.g. git@github.com:org/repo.git.
	scpLikeSSHPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+@[A-Za-z0-9_.-]+:.+$`)

	commitSHARegex = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)
)

func validateCommitSHA(sha string) error {
	if sha == "" {
		return nil
	}
	if strings.HasPrefix(sha, "-") || !commitSHARegex.MatchString(sha) {
		return fmt.Errorf("git: invalid commit SHA %q", sha)
	}
	return nil
}

func isPrivateOrLoopbackIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 127 || ip4[0] == 10 || ip4[0] == 0 {
			return true
		}
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		if ip4[0] == 100 && (ip4[1]&0xC0) == 64 {
			return true
		}
	}
	return false
}

func validateHostSSRF(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return errors.New("git: empty host in repo_url")
	}
	h := strings.ToLower(host)
	if h == "localhost" || strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".local") || strings.HasSuffix(h, ".internal") {
		return fmt.Errorf("git: repo_url host %q is a blocked internal host", host)
	}

	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrLoopbackIP(ip) {
			return fmt.Errorf("git: repo_url host %q is a private, loopback, or link-local IP", host)
		}
		return nil
	}

	// For remote domains, resolve and ensure none resolve to internal/private IPs
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err == nil && len(ips) > 0 {
		for _, ip := range ips {
			if isPrivateOrLoopbackIP(ip) {
				return fmt.Errorf("git: repo_url host %q resolves to private/loopback IP %s", host, ip.String())
			}
		}
	}
	return nil
}

// validateRepoURL ensures rawURL uses an explicitly allowed git transport before it is ever
// passed to exec.CommandContext, and guards against SSRF to private/internal networks.
func validateRepoURL(rawURL string, allowLocal bool) error {
	if strings.Contains(rawURL, "://") {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return fmt.Errorf("git: invalid repo_url: %w", err)
		}
		scheme := strings.ToLower(parsed.Scheme)
		if !allowedGitURLSchemes[scheme] {
			return fmt.Errorf("git: repo_url scheme %q is not allowed (allowed: https, http, ssh, or scp-like git@host:path)", parsed.Scheme)
		}
		if !allowLocal && os.Getenv("PIKPIK_ALLOW_LOCAL_GIT") != "1" {
			if err := validateHostSSRF(parsed.Hostname()); err != nil {
				return err
			}
		}
		return nil
	}

	if scpLikeSSHPattern.MatchString(rawURL) {
		if !allowLocal && os.Getenv("PIKPIK_ALLOW_LOCAL_GIT") != "1" {
			parts := strings.SplitN(rawURL, ":", 2)
			if len(parts) > 0 {
				userHost := parts[0]
				if atIdx := strings.Index(userHost, "@"); atIdx != -1 {
					host := userHost[atIdx+1:]
					if err := validateHostSSRF(host); err != nil {
						return err
					}
				}
			}
		}
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
	if err := validateRepoURL(opts.RepoURL, opts.AllowLocal); err != nil {
		return nil, err
	}
	if opts.CommitSHA != "" {
		if err := validateCommitSHA(opts.CommitSHA); err != nil {
			return nil, err
		}
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
		checkoutCmd := exec.CommandContext(ctx, "git", "-C", workDir, "checkout", "--", opts.CommitSHA)
		checkoutCmd.Env = cmdEnv
		var checkoutErr bytes.Buffer
		checkoutCmd.Stderr = &checkoutErr

		if err := checkoutCmd.Run(); err != nil {
			// If shallow clone didn't contain the commit, fetch the specific SHA
			fetchCmd := exec.CommandContext(ctx, "git", "-C", workDir, "fetch", fmt.Sprintf("--depth=%d", depth), "origin", "--", opts.CommitSHA)
			fetchCmd.Env = cmdEnv
			if fetchErr := fetchCmd.Run(); fetchErr == nil {
				_ = exec.CommandContext(ctx, "git", "-C", workDir, "checkout", "--", opts.CommitSHA).Run()
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
	res := strings.ReplaceAll(input, secret, "[REDACTED]")
	escaped := url.QueryEscape(secret)
	if escaped != secret {
		res = strings.ReplaceAll(res, escaped, "[REDACTED]")
	}
	return res
}
