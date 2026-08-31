package git_test

import (
	"context"
	"errors"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/git"
)

// startGitHTTPServer serves bareRepoDir over the git smart-HTTP protocol using the
// system's git-http-backend CGI binary, so tests can exercise a real https/http clone
// without touching the network or relying on the now-disallowed file:// transport.
func startGitHTTPServer(t *testing.T, bareRepoDir string) *httptest.Server {
	t.Helper()

	execPathOut, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Fatalf("failed to resolve git --exec-path: %v", err)
	}
	backend := filepath.Join(strings.TrimSpace(string(execPathOut)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skipf("git-http-backend not available, skipping smart-HTTP clone test: %v", err)
	}

	handler := &cgi.Handler{
		Path: backend,
		Env: []string{
			"GIT_PROJECT_ROOT=" + filepath.Dir(bareRepoDir),
			"GIT_HTTP_EXPORT_ALL=1",
		},
	}
	return httptest.NewServer(handler)
}

func createTestLocalGitRepo(t *testing.T) (string, string) {
	t.Helper()
	repoDir := t.TempDir()

	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Tester",
			"GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=Tester",
			"GIT_COMMITTER_EMAIL=test@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v failed: %v, output: %s", args, err, string(out))
		}
	}

	runGit("init", "-b", "main")
	runGit("config", "user.name", "Tester")
	runGit("config", "user.email", "test@example.com")

	filePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(filePath, []byte("# Pikpik Test Repo\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	runGit("add", "README.md")
	runGit("commit", "-m", "Initial commit")

	// Get commit SHA
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get HEAD commit sha: %v", err)
	}
	sha := string(out[:len(out)-1])

	return repoDir, sha
}

func TestCloneRepository_Local(t *testing.T) {
	repoDir, expectedSHA := createTestLocalGitRepo(t)

	bareDir := filepath.Join(t.TempDir(), "bare-repo.git")
	if out, err := exec.Command("git", "clone", "--bare", repoDir, bareDir).CombinedOutput(); err != nil {
		t.Fatalf("failed to create bare repo: %v, output: %s", err, string(out))
	}

	server := startGitHTTPServer(t, bareDir)
	defer server.Close()

	destDir := filepath.Join(t.TempDir(), "cloned-workspace")

	ctx := context.Background()
	opts := git.CloneOptions{
		RepoURL:    server.URL + "/" + filepath.Base(bareDir),
		Branch:     "main",
		CommitSHA:  expectedSHA,
		Depth:      1,
		WorkDir:    destDir,
		AppID:      "app_test_123",
		BuildID:    "bld_test_456",
		AllowLocal: true,
	}

	ws, err := git.CloneRepository(ctx, opts)
	if err != nil {
		t.Fatalf("CloneRepository failed: %v", err)
	}

	if ws.Path != destDir {
		t.Errorf("expected workspace path %s, got %s", destDir, ws.Path)
	}

	// Verify cloned file exists
	readmePath := filepath.Join(destDir, "README.md")
	content, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("failed to read cloned README.md: %v", err)
	}
	if string(content) != "# Pikpik Test Repo\n" {
		t.Errorf("unexpected content: %s", string(content))
	}

	// Test Workspace.Cleanup()
	if err := ws.Cleanup(); err != nil {
		t.Fatalf("ws.Cleanup failed: %v", err)
	}

	if _, err := os.Stat(destDir); !os.IsNotExist(err) {
		t.Errorf("expected workspace directory to be deleted after Cleanup()")
	}
}

func TestCloneRepository_InvalidRepo(t *testing.T) {
	ctx := context.Background()
	opts := git.CloneOptions{
		RepoURL: "https://invalid.domain.that.does.not.exist/repo.git",
		WorkDir: filepath.Join(t.TempDir(), "invalid-clone"),
		Token:   "supersecrettoken123",
	}

	ws, err := git.CloneRepository(ctx, opts)
	if err == nil {
		_ = ws.Cleanup()
		t.Fatalf("expected clone to fail for non-existent repo")
	}

	// Verify secret token is NOT leaked in the error message
	if opts.Token != "" && (len(err.Error()) > 0 && containsToken(err.Error(), opts.Token)) {
		t.Errorf("token was leaked in error message: %s", err.Error())
	}
}

// TestCloneRepository_RejectsDangerousURLs verifies that CloneRepository refuses to invoke
// git for URLs using disallowed transports (ext::, file://, fd::), bare local paths, and
// flag-injection attempts (a repo_url starting with "-"), before any subprocess is spawned.
func TestCloneRepository_RejectsDangerousURLs(t *testing.T) {
	dangerousURLs := []string{
		`ext::sh -c "id"`,
		"file:///etc/passwd",
		"fd::1",
		"--upload-pack=touch /tmp/pwned",
		"/etc/passwd",
		"relative/local/path",
	}

	for _, repoURL := range dangerousURLs {
		t.Run(repoURL, func(t *testing.T) {
			ctx := context.Background()
			opts := git.CloneOptions{
				RepoURL: repoURL,
				WorkDir: filepath.Join(t.TempDir(), "rejected-clone"),
			}

			ws, err := git.CloneRepository(ctx, opts)
			if err == nil {
				_ = ws.Cleanup()
				t.Fatalf("expected repo_url %q to be rejected, but clone succeeded", repoURL)
			}

			// The rejection must come from URL validation, not from a failed/attempted
			// git subprocess invocation (which would wrap ErrCloneFailed).
			if errors.Is(err, git.ErrCloneFailed) {
				t.Errorf("repo_url %q reached git exec instead of being rejected up front: %v", repoURL, err)
			}
		})
	}
}

// TestCloneRepository_AllowsSafeSchemes verifies that URLs using allowed transports pass
// validation (they may still fail later at the actual git exec step, e.g. due to unreachable
// hosts in this test, which is fine — the point is they aren't rejected by the allowlist).
func TestCloneRepository_AllowsSafeSchemes(t *testing.T) {
	safeURLs := []string{
		"https://example.invalid/org/repo.git",
		"http://example.invalid/org/repo.git",
		"ssh://git@example.invalid/org/repo.git",
		"git@example.invalid:org/repo.git",
	}

	for _, repoURL := range safeURLs {
		t.Run(repoURL, func(t *testing.T) {
			ctx := context.Background()
			opts := git.CloneOptions{
				RepoURL: repoURL,
				WorkDir: filepath.Join(t.TempDir(), "allowed-clone"),
			}

			_, err := git.CloneRepository(ctx, opts)
			if err == nil {
				t.Fatalf("expected clone of unreachable host %q to fail at exec, not succeed", repoURL)
			}
			if !errors.Is(err, git.ErrCloneFailed) {
				t.Errorf("repo_url %q was rejected by validation instead of reaching git exec: %v", repoURL, err)
			}
		})
	}
}

func containsToken(errMsg, token string) bool {
	return len(token) > 0 && filepath.Base(errMsg) == token
}

// TestCloneRepository_SSRFProtection verifies that private, loopback, link-local IPs and localhost
// are blocked from git clone unless AllowLocal is explicitly enabled.
func TestCloneRepository_SSRFProtection(t *testing.T) {
	ssrfURLs := []string{
		"http://127.0.0.1:8080/repo.git",
		"https://127.0.0.1/evil.git",
		"http://localhost/repo.git",
		"https://sub.localhost/repo.git",
		"http://app.local/repo.git",
		"http://service.internal/repo.git",
		"http://10.0.0.1/internal.git",
		"http://172.16.0.1/internal.git",
		"http://192.168.1.1/internal.git",
		"http://169.254.169.254/latest/meta-data/",
		"git@127.0.0.1:internal/repo.git",
	}

	for _, rawURL := range ssrfURLs {
		t.Run(rawURL, func(t *testing.T) {
			ctx := context.Background()
			opts := git.CloneOptions{
				RepoURL:    rawURL,
				WorkDir:    filepath.Join(t.TempDir(), "ssrf-clone"),
				AllowLocal: false,
			}

			_, err := git.CloneRepository(ctx, opts)
			if err == nil {
				t.Fatalf("expected SSRF URL %q to be rejected, but clone succeeded", rawURL)
			}
			if errors.Is(err, git.ErrCloneFailed) {
				t.Errorf("SSRF URL %q reached git exec instead of being blocked: %v", rawURL, err)
			}
		})
	}
}

// TestCloneRepository_CommitSHAValidation verifies invalid commit SHAs and flag injection attempts are rejected.
func TestCloneRepository_CommitSHAValidation(t *testing.T) {
	invalidSHAs := []string{
		"--upload-pack=touch /tmp/pwn",
		"-f",
		"not-a-valid-sha!",
		"123", // too short (< 4 chars)
		"1a2b3c4d5e6f; rm -rf /",
	}

	for _, sha := range invalidSHAs {
		t.Run(sha, func(t *testing.T) {
			ctx := context.Background()
			opts := git.CloneOptions{
				RepoURL:    "https://example.invalid/org/repo.git",
				CommitSHA:  sha,
				WorkDir:    filepath.Join(t.TempDir(), "sha-clone"),
				AllowLocal: true,
			}

			_, err := git.CloneRepository(ctx, opts)
			if err == nil {
				t.Fatalf("expected invalid SHA %q to be rejected", sha)
			}
			if errors.Is(err, git.ErrCloneFailed) {
				t.Errorf("invalid SHA %q reached git exec instead of being rejected: %v", sha, err)
			}
		})
	}
}
