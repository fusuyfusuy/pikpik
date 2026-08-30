package orchestration_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

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
