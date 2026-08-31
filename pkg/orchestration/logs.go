package orchestration

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// LogFrameProcessor consumes raw Docker multiplexed streams and demuxes stdout and stderr.
type LogFrameProcessor struct {
	bufferPool sync.Pool
}

// NewLogFrameProcessor creates a new LogFrameProcessor with pre-allocated 32KB buffers.
func NewLogFrameProcessor() *LogFrameProcessor {
	return &LogFrameProcessor{
		bufferPool: sync.Pool{
			New: func() interface{} {
				// Allocate 32KB chunk buffers for high-throughput streaming
				b := make([]byte, 32*1024)
				return &b
			},
		},
	}
}

// DecodeStream parses raw multiplexed reader into dedicated stdout and stderr writers.
// Uses official docker stdcopy.StdCopy to guarantee 100% binary wire compatibility.
func (p *LogFrameProcessor) DecodeStream(ctx context.Context, src io.Reader, stdout, stderr io.Writer) error {
	if src == nil {
		return errors.New("orchestrator: nil log reader")
	}

	errCh := make(chan error, 1)

	go func() {
		_, err := stdcopy.StdCopy(stdout, stderr, src)
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		if closer, ok := src.(io.Closer); ok {
			_ = closer.Close()
		}
		return ctx.Err()
	case err := <-errCh:
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("stdcopy decoding failed: %w", err)
		}
		return nil
	}
}

// DockerLogStreamer implements LogStreamer using the Docker Engine client.
type DockerLogStreamer struct {
	cli       client.CommonAPIClient
	processor *LogFrameProcessor
}

// NewDockerLogStreamer initializes a new DockerLogStreamer.
func NewDockerLogStreamer(cli client.CommonAPIClient) *DockerLogStreamer {
	return &DockerLogStreamer{
		cli:       cli,
		processor: NewLogFrameProcessor(),
	}
}

func (s *DockerLogStreamer) toDockerLogsOptions(opts LogOptions) container.LogsOptions {
	showOut := opts.ShowStdout
	showErr := opts.ShowStderr
	if !showOut && !showErr {
		showOut = true
		showErr = true
	}

	tail := opts.Tail
	if tail == "" {
		tail = "all"
	}

	return container.LogsOptions{
		ShowStdout: showOut,
		ShowStderr: showErr,
		Since:      opts.Since,
		Timestamps: opts.Timestamps,
		Follow:     opts.Follow,
		Tail:       tail,
		Details:    opts.Details,
	}
}

// StreamContainerLogs retrieves a raw log stream for a specific container.
func (s *DockerLogStreamer) StreamContainerLogs(ctx context.Context, containerID string, opts LogOptions) (io.ReadCloser, error) {
	if containerID == "" {
		return nil, ErrContainerNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.cli.ContainerLogs(ctx, containerID, s.toDockerLogsOptions(opts))
}

// StreamServiceLogs retrieves a raw log stream for a Swarm service.
func (s *DockerLogStreamer) StreamServiceLogs(ctx context.Context, serviceID string, opts LogOptions) (io.ReadCloser, error) {
	if serviceID == "" {
		return nil, ErrServiceNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.cli.ServiceLogs(ctx, serviceID, s.toDockerLogsOptions(opts))
}

// StreamDemux streams and decodes multiplexed container logs directly into stdout and stderr writers.
func (s *DockerLogStreamer) StreamDemux(ctx context.Context, containerID string, opts LogOptions, stdout, stderr io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	stream, err := s.StreamContainerLogs(ctx, containerID, opts)
	if err != nil {
		return err
	}
	defer stream.Close()

	return s.processor.DecodeStream(ctx, stream, stdout, stderr)
}
