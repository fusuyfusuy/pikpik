package build_test

import (
	"archive/tar"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/fusuycorp/pikpik/pkg/build"
)

// mockDockerClient implements minimal CommonAPIClient methods for image build testing.
type mockDockerClient struct {
	client.CommonAPIClient
	buildFunc func(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error)
}

func (m *mockDockerClient) ImageBuild(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
	if m.buildFunc != nil {
		return m.buildFunc(ctx, buildContext, options)
	}
	return types.ImageBuildResponse{
		Body: io.NopCloser(strings.NewReader(`{"stream":"Successfully built\n"}`)),
	}, nil
}

func (m *mockDockerClient) Close() error {
	return nil
}

func TestCreateTarStream(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pikpik-tar-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create test files
	_ = os.WriteFile(filepath.Join(tempDir, "Dockerfile"), []byte("FROM alpine\nCMD echo hi\n"), 0644)
	_ = os.WriteFile(filepath.Join(tempDir, "index.js"), []byte("console.log('hi')"), 0644)

	// Subdirectory with files
	subDir := filepath.Join(tempDir, "src")
	_ = os.MkdirAll(subDir, 0755)
	_ = os.WriteFile(filepath.Join(subDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644)

	// .git directory that must be excluded
	gitDir := filepath.Join(tempDir, ".git")
	_ = os.MkdirAll(gitDir, 0755)
	_ = os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644)

	stream, err := build.CreateTarStream(tempDir)
	if err != nil {
		t.Fatalf("CreateTarStream failed: %v", err)
	}
	defer stream.Close()

	tr := tar.NewReader(stream)
	filesFound := make(map[string]bool)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read error: %v", err)
		}
		filesFound[hdr.Name] = true

		if strings.HasPrefix(hdr.Name, ".git") {
			t.Errorf(".git path %q was not excluded from tar", hdr.Name)
		}
	}

	if !filesFound["Dockerfile"] {
		t.Errorf("expected Dockerfile in tar archive")
	}
	if !filesFound["index.js"] {
		t.Errorf("expected index.js in tar archive")
	}
	if !filesFound["src/main.go"] {
		t.Errorf("expected src/main.go in tar archive")
	}
}

func TestParseDockerBuildOutput_Success(t *testing.T) {
	jsonStream := `{"stream":"Step 1/3 : FROM alpine:latest\n"}
{"stream":" ---> e66264b98777\n"}
{"stream":"Step 2/3 : WORKDIR /app\n"}
{"status":"Downloading","progressDetail":{"current":50,"total":100},"id":"layer-1"}
{"aux":{"ID":"sha256:1234567890abcdef1234567890abcdef"}}
{"stream":"Successfully built sha256:1234567890abcdef1234567890abcdef\n"}
{"stream":"Successfully tagged pikpik/app:v1\n"}
`

	var capturedLogs []string
	logCb := func(line string) {
		capturedLogs = append(capturedLogs, line)
	}

	imageID, err := build.ParseDockerBuildOutput(strings.NewReader(jsonStream), logCb)
	if err != nil {
		t.Fatalf("unexpected error parsing docker output: %v", err)
	}

	if imageID != "sha256:1234567890abcdef1234567890abcdef" {
		t.Errorf("expected image ID sha256:1234567890abcdef1234567890abcdef, got %q", imageID)
	}

	if len(capturedLogs) < 4 {
		t.Errorf("expected at least 4 log lines, got %d: %v", len(capturedLogs), capturedLogs)
	}
}

func TestParseDockerBuildOutput_Error(t *testing.T) {
	jsonStream := `{"stream":"Step 1/2 : FROM node:alpine\n"}
{"stream":"Step 2/2 : RUN npm install\n"}
{"error":"npm ERR! missing script: start","errorDetail":{"message":"npm ERR! missing script: start","code":1}}
`

	var capturedLogs []string
	logCb := func(line string) {
		capturedLogs = append(capturedLogs, line)
	}

	_, err := build.ParseDockerBuildOutput(strings.NewReader(jsonStream), logCb)
	if err == nil {
		t.Fatalf("expected error from failed build stream, got nil")
	}

	if !strings.Contains(err.Error(), "missing script: start") {
		t.Errorf("expected error message to contain 'missing script: start', got %v", err)
	}
}

func TestDockerfileBuilder_BuildSuccess(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pikpik-df-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	_ = os.WriteFile(filepath.Join(tempDir, "Dockerfile"), []byte("FROM alpine\n"), 0644)

	mockCli := &mockDockerClient{
		buildFunc: func(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
			if options.Dockerfile != "Dockerfile" {
				t.Errorf("expected Dockerfile option 'Dockerfile', got %q", options.Dockerfile)
			}
			if len(options.Tags) == 0 || options.Tags[0] != "pikpik/test-app:latest" {
				t.Errorf("expected tag pikpik/test-app:latest, got %v", options.Tags)
			}

			respBody := `{"stream":"Step 1/1 : FROM alpine\n"}` + "\n" +
				`{"aux":{"ID":"sha256:buildres12345"}}` + "\n" +
				`{"stream":"Successfully built\n"}`

			return types.ImageBuildResponse{
				Body: io.NopCloser(strings.NewReader(respBody)),
			}, nil
		},
	}

	builder := build.NewDockerfileBuilder(mockCli)

	opts := build.BuildOptions{
		ImageTag: "pikpik/test-app:latest",
	}

	var logs []string
	res, err := builder.Build(context.Background(), tempDir, opts, func(l string) {
		logs = append(logs, l)
	})
	if err != nil {
		t.Fatalf("builder.Build failed: %v", err)
	}

	if res.ImageTag != "pikpik/test-app:latest" {
		t.Errorf("expected image tag pikpik/test-app:latest, got %s", res.ImageTag)
	}
	if res.Strategy != build.StrategyDockerfile {
		t.Errorf("expected strategy dockerfile, got %s", res.Strategy)
	}
	if res.ImageID != "sha256:buildres12345" {
		t.Errorf("expected image ID sha256:buildres12345, got %s", res.ImageID)
	}
}

func TestDockerfileBuilder_MissingDockerfile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pikpik-df-missing-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mockCli := &mockDockerClient{}
	builder := build.NewDockerfileBuilder(mockCli)

	opts := build.BuildOptions{
		ImageTag: "pikpik/test-app:latest",
	}

	_, err = builder.Build(context.Background(), tempDir, opts, nil)
	if err == nil {
		t.Fatalf("expected error for missing Dockerfile, got nil")
	}
	if !errors.Is(err, build.ErrNoDockerfile) {
		t.Errorf("expected ErrNoDockerfile, got %v", err)
	}
}

func TestNixpacksBuilder_GracefulFallback(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pikpik-np-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create Dockerfile to test fallback
	_ = os.WriteFile(filepath.Join(tempDir, "Dockerfile"), []byte("FROM alpine\n"), 0644)

	mockCli := &mockDockerClient{
		buildFunc: func(ctx context.Context, buildContext io.Reader, options types.ImageBuildOptions) (types.ImageBuildResponse, error) {
			return types.ImageBuildResponse{
				Body: io.NopCloser(strings.NewReader(`{"stream":"Fallback build success\n"}` + "\n" + `{"aux":{"ID":"sha256:fallback123"}}`)),
			}, nil
		},
	}

	dfBuilder := build.NewDockerfileBuilder(mockCli)
	npBuilder := build.NewNixpacksBuilder(mockCli, dfBuilder)
	// Force nonexistent binary path to test fallback
	npBuilder.SetBinaryPath("/path/to/nonexistent/nixpacks-bin-pikpik")

	if npBuilder.IsAvailable() {
		t.Errorf("expected IsAvailable to be false for non-existent binary")
	}

	var logs []string
	res, err := npBuilder.Build(context.Background(), tempDir, build.BuildOptions{
		ImageTag: "pikpik/np-app:latest",
	}, func(l string) {
		logs = append(logs, l)
	})
	if err != nil {
		t.Fatalf("Nixpacks fallback build failed: %v", err)
	}

	if res.ImageTag != "pikpik/np-app:latest" {
		t.Errorf("expected tag pikpik/np-app:latest, got %s", res.ImageTag)
	}
}

func TestDetectStrategy(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "pikpik-strat-*")
	defer os.RemoveAll(tempDir)

	// Default empty directory -> Nixpacks
	if s := build.DetectStrategy(tempDir, ""); s != build.StrategyNixpacks {
		t.Errorf("expected StrategyNixpacks, got %s", s)
	}

	// Add compose.yml -> Compose
	_ = os.WriteFile(filepath.Join(tempDir, "compose.yml"), []byte("services: {}"), 0644)
	if s := build.DetectStrategy(tempDir, ""); s != build.StrategyCompose {
		t.Errorf("expected StrategyCompose, got %s", s)
	}

	// Add Dockerfile -> Dockerfile takes precedence
	_ = os.WriteFile(filepath.Join(tempDir, "Dockerfile"), []byte("FROM alpine"), 0644)
	if s := build.DetectStrategy(tempDir, ""); s != build.StrategyDockerfile {
		t.Errorf("expected StrategyDockerfile, got %s", s)
	}
}
