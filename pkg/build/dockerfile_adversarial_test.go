package build_test

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/build"
)

func TestAdversarial_Build_TruncatedTarStream(t *testing.T) {
	// TAR-01: Simulated truncated tar stream (pipe broken prematurely)
	pr, pw := io.Pipe()

	go func() {
		// Write partial tar header and abruptly close with error
		tw := tar.NewWriter(pw)
		_ = tw.WriteHeader(&tar.Header{
			Name: "app/main.go",
			Mode: 0644,
			Size: 10 * 1024 * 1024, // Declare 10MB
		})
		_, _ = tw.Write([]byte("package main // truncated"))
		_ = pw.CloseWithError(io.ErrUnexpectedEOF)
	}()

	tr := tar.NewReader(pr)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("unexpected error reading first header: %v", err)
	}
	if hdr.Name != "app/main.go" {
		t.Fatalf("expected header name app/main.go, got %s", hdr.Name)
	}

	buf := make([]byte, 1024)
	_, err = io.CopyBuffer(io.Discard, tr, buf)
	if err == nil {
		t.Fatal("expected error on truncated tar stream, got nil")
	}
}

func TestAdversarial_Build_TarSlipDirectoryTraversal(t *testing.T) {
	// TAR-02: Validate tar slip path normalization
	tmpDir := t.TempDir()

	// Create nested structure
	subDir := filepath.Join(tmpDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "Dockerfile"), []byte("FROM alpine\n"), 0644); err != nil {
		t.Fatal(err)
	}

	tarStream, err := build.CreateTarStream(tmpDir)
	if err != nil {
		t.Fatalf("failed to create tar stream: %v", err)
	}
	defer tarStream.Close()

	tr := tar.NewReader(tarStream)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("error reading tar entry: %v", err)
		}

		// Ensure no entry starts with / or contains ..
		if strings.HasPrefix(hdr.Name, "/") {
			t.Fatalf("TAR-02 TarSlip violation: absolute path found in tar header: %s", hdr.Name)
		}
		if strings.Contains(hdr.Name, "..") {
			t.Fatalf("TAR-02 TarSlip violation: relative traversal found in tar header: %s", hdr.Name)
		}
	}
}

func TestAdversarial_Build_CorruptedHeaderAndSparseBomb(t *testing.T) {
	// TAR-03: Broken magic bytes in tar stream
	brokenTar := []byte{0x00, 0x00, 0xff, 0xfe, 0x12, 0x34}
	tr := tar.NewReader(bytes.NewReader(brokenTar))
	_, err := tr.Next()
	if err == nil {
		t.Fatal("expected error reading corrupted tar header, got nil")
	}

	// Broken BuildKit output streaming
	corruptedJSONLines := strings.NewReader("{\"stream\":\"step 1\\n\"}\n{broken_json}\n{\"error\":\"compilation failed: syntax error\"}\n")
	var logs []string
	imageID, err := build.ParseDockerBuildOutput(corruptedJSONLines, func(line string) {
		logs = append(logs, line)
	})

	if err == nil {
		t.Fatal("expected build failure error from parsed docker output")
	}
	if imageID != "" {
		t.Fatalf("expected empty imageID on failure, got %s", imageID)
	}
	if !strings.Contains(err.Error(), "compilation failed") {
		t.Fatalf("expected error message to contain failure reason, got %v", err)
	}
}

func TestAdversarial_Build_EmptyDirectoryTarStream(t *testing.T) {
	// 0-Byte empty directory tar stream produces valid EOF archive
	emptyDir := t.TempDir()

	tarStream, err := build.CreateTarStream(emptyDir)
	if err != nil {
		t.Fatalf("failed to create tar stream from empty dir: %v", err)
	}
	defer tarStream.Close()

	tr := tar.NewReader(tarStream)
	count := 0
	for {
		_, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error reading empty tar stream: %v", err)
		}
		count++
	}

	if count != 0 {
		t.Fatalf("expected 0 files in empty tar stream, got %d", count)
	}
}

func TestAdversarial_Build_SymlinkLoopHandling(t *testing.T) {
	// TAR-04: Symlink recursion defense
	tmpDir := t.TempDir()
	dirA := filepath.Join(tmpDir, "dirA")
	dirB := filepath.Join(tmpDir, "dirB")

	if err := os.MkdirAll(dirA, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dirB, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dirA, "Dockerfile"), []byte("FROM scratch\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create symlink: dirA/toB -> dirB and dirB/toA -> dirA (mutual symlink)
	_ = os.Symlink(dirB, filepath.Join(dirA, "toB"))
	_ = os.Symlink(dirA, filepath.Join(dirB, "toA"))

	done := make(chan bool)
	go func() {
		tarStream, err := build.CreateTarStream(tmpDir)
		if err != nil {
			done <- false
			return
		}
		defer tarStream.Close()

		// Drain stream
		_, _ = io.Copy(io.Discard, tarStream)
		done <- true
	}()

	select {
	case ok := <-done:
		if !ok {
			t.Fatal("CreateTarStream returned error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("TAR-04: CreateTarStream hung indefinitely on symlink loop!")
	}
}

func TestAdversarial_Build_NonExistentDirectory(t *testing.T) {
	builder := build.NewDockerfileBuilder(nil)
	ctx := context.Background()

	_, err := builder.Build(ctx, "/path/does/not/exist/999", build.BuildOptions{}, nil)
	if err == nil {
		t.Fatal("expected error on non-existent directory")
	}
}
