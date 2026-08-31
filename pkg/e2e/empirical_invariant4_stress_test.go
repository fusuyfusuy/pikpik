package e2e_test

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/backup"
	"github.com/fusuycorp/pikpik/pkg/backup/s3"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
)

// mockFastStreamReader generates an endless synthetic data stream without memory allocations.
type mockFastStreamReader struct {
	remaining int64
	seed      byte
}

func (r *mockFastStreamReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	for i := 0; i < n; i++ {
		p[i] = byte((int(r.seed) + i*17) & 0xFF)
	}
	r.remaining -= int64(n)
	return n, nil
}

// mockDockerStreamExecRunner simulates container stdout / stdin execution purely in memory.
type mockDockerStreamExecRunner struct {
	streamSize int64
}

func (m *mockDockerStreamExecRunner) ExecStreamStdout(ctx context.Context, containerID string, cmd []string, env []string, stdout io.Writer) (int, error) {
	reader := &mockFastStreamReader{remaining: m.streamSize, seed: 0x42}
	buf := make([]byte, 128*1024)
	_, err := io.CopyBuffer(stdout, reader, buf)
	if err != nil && err != io.EOF {
		return 1, err
	}
	return 0, nil
}

func (m *mockDockerStreamExecRunner) ExecStreamStdin(ctx context.Context, containerID string, cmd []string, env []string, stdin io.Reader) (int, error) {
	buf := make([]byte, 128*1024)
	_, err := io.CopyBuffer(io.Discard, stdin, buf)
	if err != nil && err != io.EOF {
		return 1, err
	}
	return 0, nil
}

// 1. Empirical Verification: 1GB Streaming Backup RAM Peak < 32MB
func TestEmpirical_StreamingBackup1GB_MemoryCeilingUnder32MB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1GB streaming stress test in short mode")
	}

	// Setup mock S3 server that consumes multipart uploads without storing in RAM
	var s3UploadedBytes atomic.Int64
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method == http.MethodPost && q.Has("uploads") {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>test-bucket</Bucket><Key>dump.gz</Key><UploadId>up-123</UploadId></InitiateMultipartUploadResult>`)
			return
		}
		if r.Method == http.MethodPut && q.Has("uploadId") {
			n, _ := io.Copy(io.Discard, r.Body)
			s3UploadedBytes.Add(n)
			w.Header().Set("ETag", `"etag-part"`)
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodPost && q.Has("uploadId") {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<CompleteMultipartUploadResult><Bucket>test-bucket</Bucket><Key>dump.gz</Key><ETag>"final-etag"</ETag></CompleteMultipartUploadResult>`)
			return
		}
		http.Error(w, "not handled", http.StatusBadRequest)
	}))
	defer s3Server.Close()

	s3Client, err := s3.NewClient(s3.ClientOptions{
		Provider:        s3.ProviderMinIO,
		Endpoint:        s3Server.URL,
		Bucket:          "test-bucket",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		PartSizeBytes:   5 * 1024 * 1024, // 5MB S3 part chunks
		MaxConcurrency:  2,
	})
	require.NoError(t, err)

	const oneGigabyte = int64(1024 * 1024 * 1024) // 1GB
	execRunner := &mockDockerStreamExecRunner{streamSize: oneGigabyte}
	engine := backup.NewBackupEngine(s3Client, execRunner)

	runtime.GC()
	var baselineMem runtime.MemStats
	runtime.ReadMemStats(&baselineMem)

	// Background memory poller to track maximum heap allocation
	var maxHeapAlloc atomic.Uint64
	maxHeapAlloc.Store(baselineMem.Alloc)
	stopPoller := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopPoller:
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				currentAlloc := m.Alloc
				for {
					oldMax := maxHeapAlloc.Load()
					if currentAlloc <= oldMax || maxHeapAlloc.CompareAndSwap(oldMax, currentAlloc) {
						break
					}
				}
			}
		}
	}()

	ctx := context.Background()
	result, err := engine.StreamBackup(ctx, backup.BackupJobConfig{
		ContainerID:  "db-container-1",
		Engine:       backup.EnginePostgres17,
		DatabaseName: "production_db",
		Username:     "postgres",
		Password:     "secret",
		ProjectSlug:  "prj_emp",
		ServiceSlug:  "srv_db",
	})

	close(stopPoller)
	require.NoError(t, err)
	require.NotNil(t, result)

	peakAllocMB := float64(maxHeapAlloc.Load()) / (1024 * 1024)
	t.Logf("Empirical 1GB Backup Streaming: Streamed %d uncompressed bytes. Compressed & uploaded %d bytes to S3. Peak Heap: %.2f MB",
		result.UncompressedBytes, s3UploadedBytes.Load(), peakAllocMB)

	assert.Equal(t, oneGigabyte, result.UncompressedBytes, "Full 1GB must have been streamed")
	assert.Greater(t, s3UploadedBytes.Load(), int64(0), "Compressed data was uploaded to S3")
	assert.Less(t, peakAllocMB, 32.0, "Invariant 4 Ceiling Violation: Peak Heap must remain < 32MB during 1GB streaming")
}

// 2. Empirical Verification: 1GB Streaming Restore RAM Peak < 32MB
func TestEmpirical_StreamingRestore1GB_MemoryCeilingUnder32MB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 1GB restore stress test in short mode")
	}

	const oneGigabyte = int64(1024 * 1024 * 1024) // 1GB

	// Setup mock S3 server that generates a gzipped stream of 1GB on the fly
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/gzip")
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)

			gw := gzip.NewWriter(w)
			reader := &mockFastStreamReader{remaining: oneGigabyte, seed: 0x88}
			buf := make([]byte, 128*1024)
			_, _ = io.CopyBuffer(gw, reader, buf)
			_ = gw.Close()
			return
		}
		http.Error(w, "not handled", http.StatusBadRequest)
	}))
	defer s3Server.Close()

	s3Client, err := s3.NewClient(s3.ClientOptions{
		Provider:        s3.ProviderMinIO,
		Endpoint:        s3Server.URL,
		Bucket:          "test-bucket",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
	})
	require.NoError(t, err)

	execRunner := &mockDockerStreamExecRunner{streamSize: 0}
	engine := backup.NewBackupEngine(s3Client, execRunner)

	runtime.GC()
	var baselineMem runtime.MemStats
	runtime.ReadMemStats(&baselineMem)

	var maxHeapAlloc atomic.Uint64
	maxHeapAlloc.Store(baselineMem.Alloc)
	stopPoller := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopPoller:
				return
			case <-ticker.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				currentAlloc := m.Alloc
				for {
					oldMax := maxHeapAlloc.Load()
					if currentAlloc <= oldMax || maxHeapAlloc.CompareAndSwap(oldMax, currentAlloc) {
						break
					}
				}
			}
		}
	}()

	ctx := context.Background()
	err = engine.StreamRestore(ctx, backup.RestoreJobConfig{
		ContainerID:  "db-container-1",
		Engine:       backup.EnginePostgres17,
		DatabaseName: "production_db",
		Username:     "postgres",
		Password:     "secret",
		S3Key:        "backups/prj_emp/srv_db/2026-08-31T00-00-00Z_postgres_bk_123.dump.gz",
	})

	close(stopPoller)
	require.NoError(t, err)

	peakAllocMB := float64(maxHeapAlloc.Load()) / (1024 * 1024)
	t.Logf("Empirical 1GB Restore Streaming: Decompressed and piped 1GB stream into container stdin. Peak Heap: %.2f MB", peakAllocMB)

	assert.Less(t, peakAllocMB, 32.0, "Invariant 4 Ceiling Violation: Peak Heap must remain < 32MB during 1GB restore")
}

// 3. Empirical Verification: Zero /tmp Staging Files Across Entire Backup/Restore Lifecycle
func TestEmpirical_ZeroTmpStagingFiles_FullLifecycle(t *testing.T) {
	tmpDir := os.TempDir()

	getTmpFiles := func() map[string]struct{} {
		files := make(map[string]struct{})
		entries, err := os.ReadDir(tmpDir)
		if err == nil {
			for _, e := range entries {
				files[e.Name()] = struct{}{}
			}
		}
		return files
	}

	initialTmpFiles := getTmpFiles()

	// Perform 50MB backup stream
	s3Server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if r.Method == http.MethodPost && q.Has("uploads") {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>b</Bucket><Key>k</Key><UploadId>u1</UploadId></InitiateMultipartUploadResult>`)
			return
		}
		if r.Method == http.MethodPut && q.Has("uploadId") {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("ETag", `"e1"`)
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Method == http.MethodPost && q.Has("uploadId") {
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, `<CompleteMultipartUploadResult><Bucket>b</Bucket><Key>k</Key><ETag>"e1"</ETag></CompleteMultipartUploadResult>`)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/gzip")
			w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)
			gw := gzip.NewWriter(w)
			_, _ = gw.Write(make([]byte, 1024*1024))
			_ = gw.Close()
			return
		}
		http.Error(w, "not handled", http.StatusBadRequest)
	}))
	defer s3Server.Close()

	s3Client, err := s3.NewClient(s3.ClientOptions{
		Provider:    s3.ProviderMinIO,
		Endpoint:    s3Server.URL,
		Bucket:      "b",
		AccessKeyID: "admin", SecretAccessKey: "admin",
		PartSizeBytes: 5 * 1024 * 1024,
	})
	require.NoError(t, err)

	execRunner := &mockDockerStreamExecRunner{streamSize: 50 * 1024 * 1024} // 50MB
	engine := backup.NewBackupEngine(s3Client, execRunner)

	// Stream Backup
	_, err = engine.StreamBackup(context.Background(), backup.BackupJobConfig{
		ContainerID:  "c1",
		Engine:       backup.EngineMySQL84,
		DatabaseName: "shop",
	})
	require.NoError(t, err)

	// Check /tmp during lifecycle
	midTmpFiles := getTmpFiles()
	for f := range midTmpFiles {
		if _, existed := initialTmpFiles[f]; !existed {
			if strings.Contains(f, "pikpik") || strings.Contains(f, "backup") || strings.Contains(f, "staging") {
				t.Fatalf("Invariant 4 Violation: Found temporary staging file in /tmp: %s", f)
			}
		}
	}

	// Stream Restore
	err = engine.StreamRestore(context.Background(), backup.RestoreJobConfig{
		ContainerID:  "c1",
		Engine:       backup.EngineMySQL84,
		DatabaseName: "shop",
		S3Key:        "backups/default/shop/test.dump.gz",
	})
	require.NoError(t, err)

	// Check /tmp after restore
	finalTmpFiles := getTmpFiles()
	for f := range finalTmpFiles {
		if _, existed := initialTmpFiles[f]; !existed {
			if strings.Contains(f, "pikpik") || strings.Contains(f, "backup") || strings.Contains(f, "staging") {
				t.Fatalf("Invariant 4 Violation: Found temporary staging file in /tmp after restore: %s", f)
			}
		}
	}
}

// 4. Empirical Verification: Slow SSE Consumers (50 slow clients) - Non-Blocking & Zero Goroutine Leaks
func TestEmpirical_SSE_SlowConsumers_NoGoroutineLeak_NoBlocking(t *testing.T) {
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	initialGoroutines := runtime.NumGoroutine()

	broadcaster := api.NewSSEBroadcaster()
	defer broadcaster.Close()

	const numSlowClients = 50
	var clients []*api.SSEClient

	// Register 50 clients that never read from SendChan
	for i := 0; i < numSlowClients; i++ {
		c := broadcaster.Register("logs", fmt.Sprintf("srv-%d", i%5))
		clients = append(clients, c)
	}

	// Rapidly broadcast 10,000 frames
	start := time.Now()
	const numMessages = 10000
	for i := 0; i < numMessages; i++ {
		target := fmt.Sprintf("srv-%d", i%5)
		broadcaster.Broadcast("logs", target, "log", fmt.Sprintf("streaming line %d payload", i))
	}
	broadcastDuration := time.Since(start)

	t.Logf("Empirical SSE: Broadcasted %d messages across %d slow clients in %v",
		numMessages, numSlowClients, broadcastDuration)

	// Broadcaster must complete without deadlocking (ceiling 1s for 10k messages under -race)
	assert.Less(t, broadcastDuration, 1000*time.Millisecond, "SSE Broadcaster must not block on slow consumers")

	// Unregister all clients
	for _, c := range clients {
		broadcaster.Unregister(c)
	}

	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()

	// Verify goroutines returned to baseline
	goroutineDiff := finalGoroutines - initialGoroutines
	t.Logf("Goroutine count: initial=%d, final=%d (diff=%d)", initialGoroutines, finalGoroutines, goroutineDiff)
	assert.LessOrEqual(t, goroutineDiff, 3, "Goroutine leak detected in SSE broadcaster")
}

// 5. Empirical Verification: Slow WebSocket Clients (30 slow clients) - Non-Blocking & Zero Goroutine Leaks
func TestEmpirical_WS_SlowConsumers_NoGoroutineLeak_NoBlocking(t *testing.T) {
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	initialGoroutines := runtime.NumGoroutine()

	hub := telemetry.NewWebSocketHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := hub.(interface{ HandleWebSocket(http.ResponseWriter, *http.Request) }); ok {
			h.HandleWebSocket(w, r)
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	const numSlowClients = 30
	var conns []*websocket.Conn
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i := 0; i < numSlowClients; i++ {
		conn, _, err := websocket.Dial(ctx, wsURL, nil)
		require.NoError(t, err)
		conns = append(conns, conn)

		subFrame := telemetry.ClientCommandFrame{
			Action:  "subscribe",
			Channel: "metrics",
			Target:  fmt.Sprintf("srv-%d", i%3),
		}
		subBytes, _ := json.Marshal(subFrame)
		err = conn.Write(ctx, websocket.MessageText, subBytes)
		require.NoError(t, err)
	}

	time.Sleep(100 * time.Millisecond)

	// Broadcast 2,000 metrics and logs
	start := time.Now()
	for i := 0; i < 2000; i++ {
		hub.Broadcast(&telemetry.StreamMessage{
			Channel:   "metrics",
			TargetID:  fmt.Sprintf("srv-%d", i%3),
			Type:      "metric",
			Timestamp: time.Now().Unix(),
			Payload:   map[string]float64{"cpu": float64(i % 100)},
		})
	}
	broadcastDuration := time.Since(start)

	t.Logf("Empirical WS: Broadcasted 2000 messages across %d slow WS clients in %v",
		numSlowClients, broadcastDuration)

	assert.Less(t, broadcastDuration, 500*time.Millisecond, "WebSocketHub must not block on slow consumers")

	// Close all client connections gracefully
	for _, c := range conns {
		_ = c.Close(websocket.StatusNormalClosure, "done")
	}

	// Allow server handler goroutines to exit
	time.Sleep(300 * time.Millisecond)
	runtime.GC()
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()
	goroutineDiff := finalGoroutines - initialGoroutines
	t.Logf("Goroutine count WS: initial=%d, final=%d (diff=%d)", initialGoroutines, finalGoroutines, goroutineDiff)
	assert.LessOrEqual(t, goroutineDiff, 5, "Goroutine leak detected in WebSocketHub handlers")
}
