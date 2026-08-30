package build

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/docker/docker/client"
)

var (
	// ErrNixpacksUnavailable is returned when nixpacks is not installed and no fallback is possible.
	ErrNixpacksUnavailable = errors.New("build: nixpacks binary not available and no fallback Dockerfile found")
)

// NixpacksBuilder builds container images from raw application source code using Railway's Nixpacks engine.
type NixpacksBuilder struct {
	cli               client.CommonAPIClient
	dockerfileBuilder *DockerfileBuilder
	binaryPath        string
}

// NewNixpacksBuilder creates a new NixpacksBuilder with fallback Dockerfile builder capabilities.
func NewNixpacksBuilder(cli client.CommonAPIClient, dfBuilder *DockerfileBuilder) *NixpacksBuilder {
	if dfBuilder == nil && cli != nil {
		dfBuilder = NewDockerfileBuilder(cli)
	}
	return &NixpacksBuilder{
		cli:               cli,
		dockerfileBuilder: dfBuilder,
		binaryPath:        "nixpacks",
	}
}

// SetBinaryPath allows overriding the nixpacks executable path (useful for testing).
func (b *NixpacksBuilder) SetBinaryPath(path string) {
	b.binaryPath = path
}

// IsAvailable checks whether the nixpacks CLI binary exists in the system PATH.
func (b *NixpacksBuilder) IsAvailable() bool {
	binary := b.binaryPath
	if binary == "" {
		binary = "nixpacks"
	}
	_, err := exec.LookPath(binary)
	return err == nil
}

// Build compiles source code into an OCI container image using nixpacks, with graceful fallback.
func (b *NixpacksBuilder) Build(ctx context.Context, srcDir string, opts BuildOptions, logCb LogCallback) (*BuildResult, error) {
	if srcDir == "" {
		return nil, errors.New("build: srcDir cannot be empty")
	}

	start := time.Now().UTC()

	// If nixpacks binary is not found, attempt graceful fallback to Dockerfile
	if !b.IsAvailable() {
		if logCb != nil {
			logCb("[nixpacks] nixpacks executable not found on host; inspecting workspace for Dockerfile fallback...")
		}

		dockerfileName := opts.DockerfilePath
		if dockerfileName == "" {
			dockerfileName = "Dockerfile"
		}
		dockerfilePath := filepath.Join(srcDir, dockerfileName)

		if _, err := os.Stat(dockerfilePath); err == nil {
			if logCb != nil {
				logCb(fmt.Sprintf("[nixpacks] Found %s; falling back to Docker BuildKit builder.", dockerfileName))
			}
			if b.dockerfileBuilder != nil {
				return b.dockerfileBuilder.Build(ctx, srcDir, opts, logCb)
			}
		}

		// Check if we can auto-generate a fallback Dockerfile for common runtimes
		if generatedContent := generateFallbackDockerfile(srcDir); generatedContent != "" {
			if logCb != nil {
				logCb("[nixpacks] Auto-generated fallback multi-stage Dockerfile for detected runtime...")
			}
			tempDF := filepath.Join(srcDir, "Dockerfile.pikpik_fallback")
			if err := os.WriteFile(tempDF, []byte(generatedContent), 0644); err == nil {
				defer os.Remove(tempDF)
				fallbackOpts := opts
				fallbackOpts.DockerfilePath = "Dockerfile.pikpik_fallback"
				if b.dockerfileBuilder != nil {
					res, err := b.dockerfileBuilder.Build(ctx, srcDir, fallbackOpts, logCb)
					if err == nil {
						res.Strategy = StrategyNixpacks
						return res, nil
					}
				}
			}
		}

		return nil, fmt.Errorf("%w: please install nixpacks (curl -sSL https://nixpacks.com/install.sh | bash) or provide a Dockerfile", ErrNixpacksUnavailable)
	}

	if logCb != nil {
		logCb(fmt.Sprintf("[nixpacks] Starting Nixpacks build for image tag %s...", opts.ImageTag))
	}

	args := []string{"build", srcDir, "--name", opts.ImageTag}
	if opts.NoCache {
		args = append(args, "--no-cache")
	}

	for k, v := range opts.Env {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range opts.BuildArgs {
		args = append(args, "--env", fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.CommandContext(ctx, b.binaryPath, args...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("build: failed to create stdout pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout // Merge stderr into stdout stream

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("build: failed to start nixpacks process: %w", err)
	}

	scanner := bufio.NewScanner(stdoutPipe)
	for scanner.Scan() {
		line := scanner.Text()
		if logCb != nil {
			logCb(line)
		}
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("build: nixpacks build exited with error: %w", err)
	}

	duration := time.Since(start)
	if logCb != nil {
		logCb(fmt.Sprintf("[nixpacks] Build completed successfully in %v. Image: %s", duration.Round(time.Millisecond), opts.ImageTag))
	}

	return &BuildResult{
		ImageTag: opts.ImageTag,
		Strategy: StrategyNixpacks,
		Duration: duration,
	}, nil
}

// generateFallbackDockerfile detects basic runtime files and generates a lightweight Dockerfile.
func generateFallbackDockerfile(srcDir string) string {
	// 1. Go
	if _, err := os.Stat(filepath.Join(srcDir, "go.mod")); err == nil {
		return `FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum* ./
RUN go mod download || true
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server /app/server
EXPOSE 8080
CMD ["/app/server"]
`
	}

	// 2. Node.js
	if _, err := os.Stat(filepath.Join(srcDir, "package.json")); err == nil {
		return `FROM node:20-alpine
WORKDIR /app
COPY package*.json ./
RUN npm install --production
COPY . .
EXPOSE 3000
CMD ["npm", "start"]
`
	}

	// 3. Python
	if _, err := os.Stat(filepath.Join(srcDir, "requirements.txt")); err == nil {
		return `FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
EXPOSE 8000
CMD ["python", "main.py"]
`
	}

	// 4. Static HTML / Frontend
	if _, err := os.Stat(filepath.Join(srcDir, "index.html")); err == nil {
		return `FROM nginx:alpine
COPY . /usr/share/nginx/html/
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
`
	}

	return ""
}
