package build

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

var (
	// ErrNoDockerfile is returned when no Dockerfile exists in the build context.
	ErrNoDockerfile = errors.New("build: dockerfile not found")

	// ErrBuildFailed is returned when Docker daemon reports a build error.
	ErrBuildFailed = errors.New("build: docker build failed")
)

// DockerfileBuilder compiles source code containing a Dockerfile via the Docker Socket BuildKit API.
type DockerfileBuilder struct {
	cli client.CommonAPIClient
}

// NewDockerfileBuilder creates a new DockerfileBuilder instance.
func NewDockerfileBuilder(cli client.CommonAPIClient) *DockerfileBuilder {
	return &DockerfileBuilder{cli: cli}
}

// Build packages the source directory into an in-memory tar stream and executes ImageBuild via the Docker client.
func (b *DockerfileBuilder) Build(ctx context.Context, srcDir string, opts BuildOptions, logCb LogCallback) (*BuildResult, error) {
	if srcDir == "" {
		return nil, errors.New("build: srcDir cannot be empty")
	}

	start := time.Now().UTC()

	contextDir := srcDir
	if opts.ContextDir != "" {
		contextDir = filepath.Join(srcDir, opts.ContextDir)
	}

	dockerfileName := opts.DockerfilePath
	if dockerfileName == "" {
		dockerfileName = "Dockerfile"
	}

	fullDockerfilePath := filepath.Join(contextDir, dockerfileName)
	if _, err := os.Stat(fullDockerfilePath); os.IsNotExist(err) {
		// Also check root srcDir if contextDir was shifted
		if _, errRoot := os.Stat(filepath.Join(srcDir, dockerfileName)); os.IsNotExist(errRoot) {
			return nil, fmt.Errorf("%w: %s", ErrNoDockerfile, fullDockerfilePath)
		}
	}

	if logCb != nil {
		logCb(fmt.Sprintf("[buildkit] Packaging context directory %s into tar stream...", contextDir))
	}

	tarStream, err := CreateTarStream(contextDir)
	if err != nil {
		return nil, fmt.Errorf("build: failed to create context tar stream: %w", err)
	}
	defer tarStream.Close()

	buildArgs := make(map[string]*string, len(opts.BuildArgs))
	for k, v := range opts.BuildArgs {
		val := v
		buildArgs[k] = &val
	}

	tags := []string{}
	if opts.ImageTag != "" {
		tags = append(tags, opts.ImageTag)
	}

	buildOpts := types.ImageBuildOptions{
		Tags:        tags,
		Dockerfile:  dockerfileName,
		BuildArgs:   buildArgs,
		Target:      opts.Target,
		NoCache:     opts.NoCache,
		Remove:      true,
		ForceRemove: true,
		Version:     types.BuilderBuildKit,
		Labels:      opts.Labels,
	}

	if logCb != nil {
		logCb(fmt.Sprintf("[buildkit] Sending build request to Docker daemon (tag: %s)...", opts.ImageTag))
	}

	resp, err := b.cli.ImageBuild(ctx, tarStream, buildOpts)
	if err != nil {
		return nil, fmt.Errorf("build: failed to initiate image build: %w", err)
	}
	defer resp.Body.Close()

	imageID, err := ParseDockerBuildOutput(resp.Body, logCb)
	if err != nil {
		return nil, err
	}

	duration := time.Since(start)
	if logCb != nil {
		logCb(fmt.Sprintf("[buildkit] Build successfully completed in %v. Image: %s", duration.Round(time.Millisecond), opts.ImageTag))
	}

	return &BuildResult{
		ImageTag: opts.ImageTag,
		Strategy: StrategyDockerfile,
		Duration: duration,
		ImageID:  imageID,
	}, nil
}

// CreateTarStream packages a directory tree into a streaming tar pipeline without in-memory buffering.
// Standard ignore patterns like .git are pruned to ensure efficient transmission.
func CreateTarStream(srcDir string, excludes ...string) (io.ReadCloser, error) {
	pr, pw := io.Pipe()
	tw := tar.NewWriter(pw)

	excludeMap := make(map[string]bool)
	excludeMap[".git"] = true
	excludeMap[".DS_Store"] = true
	for _, ex := range excludes {
		excludeMap[ex] = true
	}

	go func() {
		defer pw.Close()
		defer tw.Close()

		err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			relPath, err := filepath.Rel(srcDir, path)
			if err != nil {
				return err
			}

			if relPath == "." {
				return nil
			}

			// Check exclusion patterns
			parts := strings.Split(filepath.ToSlash(relPath), "/")
			for _, part := range parts {
				if excludeMap[part] {
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}

			var link string
			if info.Mode()&os.ModeSymlink != 0 {
				var readErr error
				link, readErr = os.Readlink(path)
				if readErr != nil {
					return readErr
				}
			}

			header, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}

			header.Name = filepath.ToSlash(relPath)
			if info.IsDir() {
				header.Name += "/"
			}

			if err := tw.WriteHeader(header); err != nil {
				return err
			}

			if info.Mode().IsRegular() {
				file, err := os.Open(path)
				if err != nil {
					return err
				}
				_, copyErr := io.Copy(tw, file)
				_ = file.Close()
				if copyErr != nil {
					return copyErr
				}
			}

			return nil
		})

		if err != nil {
			_ = pw.CloseWithError(err)
		}
	}()

	return pr, nil
}

// ParseDockerBuildOutput parses line-delimited JSON progress messages from Docker BuildKit.
// Streams human-readable log lines to the log callback and extracts image ID or failure reasons.
func ParseDockerBuildOutput(r io.Reader, logCb LogCallback) (string, error) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	var lastError string
	var auxImageID string

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}

		var msg DockerJSONMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// If not JSON formatted, output raw line
			if logCb != nil {
				logCb(string(line))
			}
			continue
		}

		if msg.Error != "" {
			lastError = msg.Error
			if logCb != nil {
				logCb(fmt.Sprintf("ERROR: %s", msg.Error))
			}
		} else if msg.ErrorDetail != nil && msg.ErrorDetail.Message != "" {
			lastError = msg.ErrorDetail.Message
			if logCb != nil {
				logCb(fmt.Sprintf("ERROR: %s", msg.ErrorDetail.Message))
			}
		}

		if msg.Stream != "" {
			if logCb != nil {
				streamText := strings.TrimRight(msg.Stream, "\r\n")
				if streamText != "" {
					logCb(streamText)
				}
			}
		}

		if msg.Status != "" {
			if logCb != nil {
				if msg.ID != "" {
					logCb(fmt.Sprintf("%s: %s", msg.ID, msg.Status))
				} else {
					logCb(msg.Status)
				}
			}
		}

		if msg.Aux != nil {
			if idVal, ok := msg.Aux["ID"]; ok {
				auxImageID = fmt.Sprintf("%v", idVal)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("build: error reading docker stream: %w", err)
	}

	if lastError != "" {
		return "", fmt.Errorf("%w: %s", ErrBuildFailed, lastError)
	}

	return auxImageID, nil
}
