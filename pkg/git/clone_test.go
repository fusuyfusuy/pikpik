package git_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/git"
)

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
	destDir := filepath.Join(t.TempDir(), "cloned-workspace")

	ctx := context.Background()
	opts := git.CloneOptions{
		RepoURL:   "file://" + repoDir,
		Branch:    "main",
		CommitSHA: expectedSHA,
		Depth:     1,
		WorkDir:   destDir,
		AppID:     "app_test_123",
		BuildID:   "bld_test_456",
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

func containsToken(errMsg, token string) bool {
	return len(token) > 0 && filepath.Base(errMsg) == token
}
