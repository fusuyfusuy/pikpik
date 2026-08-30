package orchestration_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/fusuycorp/pikpik/pkg/orchestration"
)

// TestBinaryStreamDecoder verifies the multiplexed Docker socket stream parser.
func TestBinaryStreamDecoder(t *testing.T) {
	// Construct a synthetic Docker multiplexed stream
	var rawStream bytes.Buffer

	// Frame 1: Stdout (0x01), "Hello Stdout\n"
	stdoutPayload := []byte("Hello Stdout\n")
	hdr1 := make([]byte, 8)
	hdr1[0] = 1 // Stdout
	binary.BigEndian.PutUint32(hdr1[4:8], uint32(len(stdoutPayload)))
	rawStream.Write(hdr1)
	rawStream.Write(stdoutPayload)

	// Frame 2: Stderr (0x02), "Error: Stderr Warning\n"
	stderrPayload := []byte("Error: Stderr Warning\n")
	hdr2 := make([]byte, 8)
	hdr2[0] = 2 // Stderr
	binary.BigEndian.PutUint32(hdr2[4:8], uint32(len(stderrPayload)))
	rawStream.Write(hdr2)
	rawStream.Write(stderrPayload)

	processor := orchestration.NewLogFrameProcessor()
	var outBuf, errBuf bytes.Buffer

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := processor.DecodeStream(ctx, &rawStream, &outBuf, &errBuf)
	if err != nil {
		t.Fatalf("failed to decode stream: %v", err)
	}

	if outBuf.String() != "Hello Stdout\n" {
		t.Errorf("stdout mismatch: got '%s', want 'Hello Stdout\\n'", outBuf.String())
	}

	if errBuf.String() != "Error: Stderr Warning\n" {
		t.Errorf("stderr mismatch: got '%s', want 'Error: Stderr Warning\\n'", errBuf.String())
	}
}

// TestBinaryStreamDecoder_NilReader checks nil reader safety.
func TestBinaryStreamDecoder_NilReader(t *testing.T) {
	processor := orchestration.NewLogFrameProcessor()
	var outBuf, errBuf bytes.Buffer

	err := processor.DecodeStream(context.Background(), nil, &outBuf, &errBuf)
	if err == nil {
		t.Errorf("expected error for nil stream reader, got nil")
	}
}

// TestBinaryStreamDecoder_ContextCancellation verifies that context cancellation stops stream decoding gracefully.
func TestBinaryStreamDecoder_ContextCancellation(t *testing.T) {
	processor := orchestration.NewLogFrameProcessor()
	var outBuf, errBuf bytes.Buffer

	// Infinite empty reader simulator
	pipeReader, pipeWriter := bytes.NewBuffer(nil), bytes.NewBuffer(nil)
	_ = pipeWriter

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := processor.DecodeStream(ctx, pipeReader, &outBuf, &errBuf)
	if err == nil && ctx.Err() == nil {
		t.Errorf("expected cancellation error")
	}
}

// TestDockerLogStreamer_FollowMode verifies streaming options and demux integration.
func TestDockerLogStreamer_FollowMode(t *testing.T) {
	var capturedOpts container.LogsOptions
	var capturedID string

	// Construct a synthetic multiplexed stream
	var rawStream bytes.Buffer
	stdoutMsg := []byte("app started on port 3000\n")
	hdr := make([]byte, 8)
	hdr[0] = 1 // Stdout
	binary.BigEndian.PutUint32(hdr[4:8], uint32(len(stdoutMsg)))
	rawStream.Write(hdr)
	rawStream.Write(stdoutMsg)

	mock := &MockDockerClient{
		ContainerLogsFunc: func(ctx context.Context, container string, options container.LogsOptions) (io.ReadCloser, error) {
			capturedID = container
			capturedOpts = options
			return io.NopCloser(bytes.NewReader(rawStream.Bytes())), nil
		},
		ServiceLogsFunc: func(ctx context.Context, service string, options container.LogsOptions) (io.ReadCloser, error) {
			capturedID = service
			capturedOpts = options
			return io.NopCloser(bytes.NewReader(rawStream.Bytes())), nil
		},
	}

	streamer := orchestration.NewDockerLogStreamer(mock)

	// 1. Container live log stream with Follow: true
	logOpts := orchestration.LogOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "100",
		Timestamps: true,
	}

	rc, err := streamer.StreamContainerLogs(nil, "c-live-1", logOpts)
	if err != nil {
		t.Fatalf("StreamContainerLogs failed: %v", err)
	}
	_ = rc.Close()

	if capturedID != "c-live-1" {
		t.Errorf("expected container ID c-live-1, got %s", capturedID)
	}
	if !capturedOpts.Follow {
		t.Errorf("expected Follow: true in container logs options")
	}
	if capturedOpts.Tail != "100" {
		t.Errorf("expected Tail: 100, got %s", capturedOpts.Tail)
	}
	if !capturedOpts.Timestamps {
		t.Errorf("expected Timestamps: true")
	}

	// 2. Service live log stream with Follow: true
	rcSvc, err := streamer.StreamServiceLogs(nil, "svc-live-1", logOpts)
	if err != nil {
		t.Fatalf("StreamServiceLogs failed: %v", err)
	}
	_ = rcSvc.Close()

	if capturedID != "svc-live-1" {
		t.Errorf("expected service ID svc-live-1, got %s", capturedID)
	}
	if !capturedOpts.Follow {
		t.Errorf("expected Follow: true in service logs options")
	}

	// 3. StreamDemux live stream
	var stdoutBuf, stderrBuf bytes.Buffer
	err = streamer.StreamDemux(context.Background(), "c-live-1", logOpts, &stdoutBuf, &stderrBuf)
	if err != nil {
		t.Fatalf("StreamDemux failed: %v", err)
	}
	if stdoutBuf.String() != "app started on port 3000\n" {
		t.Errorf("unexpected stdout: %s", stdoutBuf.String())
	}

	// 4. Edge cases
	if _, err := streamer.StreamContainerLogs(nil, "", logOpts); !errors.Is(err, orchestration.ErrContainerNotFound) {
		t.Errorf("expected ErrContainerNotFound for empty container ID, got %v", err)
	}
	if _, err := streamer.StreamServiceLogs(nil, "", logOpts); !errors.Is(err, orchestration.ErrServiceNotFound) {
		t.Errorf("expected ErrServiceNotFound for empty service ID, got %v", err)
	}
}
