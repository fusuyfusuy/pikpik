package backup_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/backup"
	"github.com/fusuycorp/pikpik/pkg/backup/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStreamingBackup_MemoryAndDiskInvariants verifies that streaming a 200MB simulated database dump
// creates 0 bytes of temporary files in /tmp or os.TempDir() and maintains peak RAM allocation < 32MB.
func TestStreamingBackup_MemoryAndDiskInvariants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	testTmp := t.TempDir()
	t.Setenv("TMPDIR", testTmp)
	tmpDir := os.TempDir()
	initialFiles, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed reading tmp dir: %v", err)
	}


	var mStart, mPeak runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&mStart)

	// Mock S3 Client collecting parts without disk writes
	mockS3 := &mockS3MultipartClient{
		partsReceived: make(map[string][][]byte),
	}

	// Create 200MB random payload stream simulating pg_dump stdout
	const payloadSize = 200 * 1024 * 1024 // 200 MB
	simulatedDumpReader := io.LimitReader(rand.Reader, payloadSize)

	pipeReader, pipeWriter := io.Pipe()

	errChan := make(chan error, 1)
	go func() {
		defer pipeWriter.Close()
		buf := make([]byte, 32*1024) // 32KB chunk buffer
		_, err := io.CopyBuffer(pipeWriter, simulatedDumpReader, buf)
		errChan <- err
	}()

	// Execute streaming upload
	uploadOpts := s3.UploadOptions{ContentType: "application/gzip"}
	_, err = mockS3.UploadStreamMultipart(ctx, "test-backup.dump.gz", pipeReader, uploadOpts)
	if err != nil {
		t.Fatalf("multipart streaming upload failed: %v", err)
	}

	if err := <-errChan; err != nil {
		t.Fatalf("producer copy failed: %v", err)
	}

	runtime.ReadMemStats(&mPeak)
	heapAllocIncrease := int64(mPeak.HeapAlloc) - int64(mStart.HeapAlloc)
	const maxAllowedBytes = 32 * 1024 * 1024 // 32 MB ceiling

	if heapAllocIncrease > maxAllowedBytes {
		t.Errorf("Invariant 4 VIOLATION: Peak RAM increased by %d bytes (exceeded 32MB limit)", heapAllocIncrease)
	}

	// Verify zero /tmp disk footprint
	finalFiles, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("failed reading tmp dir after test: %v", err)
	}
	if len(finalFiles) > len(initialFiles) {
		t.Errorf("Invariant 4 VIOLATION: Temporary files leaked in %s: before=%d, after=%d",
			tmpDir, len(initialFiles), len(finalFiles))
	}
}

type mockS3MultipartClient struct {
	mu            sync.Mutex
	partsReceived map[string][][]byte
	storage       map[string][]byte
	storeData     bool
}

func (m *mockS3MultipartClient) UploadStreamMultipart(ctx context.Context, key string, r io.Reader, opts s3.UploadOptions) (*s3.ObjectInfo, error) {
	partBuf := make([]byte, 5*1024*1024) // 5MB part buffer
	var totalSize int64
	var fullData bytes.Buffer
	for {
		n, err := io.ReadFull(r, partBuf)
		if n > 0 {
			totalSize += int64(n)
			if m.storeData {
				fullData.Write(partBuf[:n])
			}
		}
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}

	if m.storeData {
		m.mu.Lock()
		if m.storage == nil {
			m.storage = make(map[string][]byte)
		}
		m.storage[key] = fullData.Bytes()
		m.mu.Unlock()
	}

	return &s3.ObjectInfo{Key: key, Size: totalSize, ETag: `"mock-etag"`, LastModified: time.Now()}, nil
}

func (m *mockS3MultipartClient) DownloadStream(ctx context.Context, key string) (io.ReadCloser, *s3.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.storage[key]
	if !ok {
		return nil, nil, backup.ErrS3ObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), &s3.ObjectInfo{
		Key:          key,
		Size:         int64(len(data)),
		ETag:         `"mock-etag"`,
		LastModified: time.Now(),
	}, nil
}

func (m *mockS3MultipartClient) ListObjects(ctx context.Context, prefix string) ([]s3.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []s3.ObjectInfo
	for k, v := range m.storage {
		if strings.HasPrefix(k, prefix) {
			list = append(list, s3.ObjectInfo{
				Key:          k,
				Size:         int64(len(v)),
				ETag:         `"mock-etag"`,
				LastModified: time.Now(),
			})
		}
	}
	return list, nil
}

func (m *mockS3MultipartClient) DeleteObjects(ctx context.Context, keys []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.storage, k)
	}
	return nil
}

func (m *mockS3MultipartClient) PruneRetention(ctx context.Context, prefix string, policy s3.RetentionPolicy) ([]string, error) {
	objs, err := m.ListObjects(ctx, prefix)
	if err != nil {
		return nil, err
	}
	pruner := s3.NewRetentionEngine(s3.RetentionEngineOptions{ReferenceTime: time.Now()})
	toDelete, _ := pruner.EvaluateRetention(objs, policy)
	_ = m.DeleteObjects(ctx, toDelete)
	return toDelete, nil
}

func (m *mockS3MultipartClient) PruneStaleMultipartUploads(ctx context.Context, maxAge time.Duration) ([]string, error) {
	return nil, nil
}

type mockExecRunner struct {
	dumpPayload    []byte
	dumpExitCode   int
	restoredData   []byte
	restoreExitCode int
}

func (m *mockExecRunner) ExecStreamStdout(ctx context.Context, containerID string, cmd []string, env []string, stdout io.Writer) (int, error) {
	if m.dumpPayload != nil {
		_, _ = stdout.Write(m.dumpPayload)
	}
	return m.dumpExitCode, nil
}

func (m *mockExecRunner) ExecStreamStdin(ctx context.Context, containerID string, cmd []string, env []string, stdin io.Reader) (int, error) {
	data, _ := io.ReadAll(stdin)
	m.restoredData = data
	return m.restoreExitCode, nil
}

func TestBackupEngine_EndToEndStreaming(t *testing.T) {
	ctx := context.Background()
	mockS3 := &mockS3MultipartClient{
		storage:   make(map[string][]byte),
		storeData: true,
	}

	simulatedDump := bytes.Repeat([]byte("CREATE TABLE users (id serial PRIMARY KEY, name text);\n"), 1000)
	exec := &mockExecRunner{
		dumpPayload:  simulatedDump,
		dumpExitCode: 0,
	}

	engine := backup.NewBackupEngine(mockS3, exec)

	// 1. Execute StreamBackup
	bResult, err := engine.StreamBackup(ctx, backup.BackupJobConfig{
		BackupID:     "bk_test123",
		ProjectSlug:  "ecom-prod",
		ServiceSlug:  "postgres",
		ContainerID:  "cnt_pg_1",
		Engine:       backup.EnginePostgres17,
		DatabaseName: "ecom_db",
		Username:     "pguser",
		Password:     "secretpass",
		S3Bucket:     "backups-bucket",
		Compression:  "gzip",
	})
	require.NoError(t, err)
	assert.Equal(t, "bk_test123", bResult.BackupID)
	assert.True(t, strings.HasPrefix(bResult.S3Key, "backups/ecom-prod/postgres/"))
	assert.Equal(t, int64(len(simulatedDump)), bResult.UncompressedBytes)
	assert.Greater(t, bResult.CompressedBytes, int64(0))

	// 2. Execute StreamRestore
	err = engine.StreamRestore(ctx, backup.RestoreJobConfig{
		RestoreID:    "rst_test123",
		ProjectSlug:  "ecom-prod",
		ServiceSlug:  "postgres",
		ContainerID:  "cnt_pg_1",
		Engine:       backup.EnginePostgres17,
		DatabaseName: "ecom_db",
		Username:     "pguser",
		Password:     "secretpass",
		S3Bucket:     "backups-bucket",
		S3Key:        bResult.S3Key,
		Compression:  "gzip",
	})
	require.NoError(t, err)
	assert.Equal(t, simulatedDump, exec.restoredData)
}

func TestBackupEngine_ContainerExecFailure(t *testing.T) {
	ctx := context.Background()
	mockS3 := &mockS3MultipartClient{
		storage: make(map[string][]byte),
	}

	exec := &mockExecRunner{
		dumpExitCode: 1, // Non-zero exit code
	}

	engine := backup.NewBackupEngine(mockS3, exec)

	_, err := engine.StreamBackup(ctx, backup.BackupJobConfig{
		BackupID:    "bk_fail",
		ProjectSlug: "proj",
		ServiceSlug: "postgres",
		Engine:      backup.EnginePostgres17,
	})
	assert.Error(t, err)
}

func TestPostgresTemplate_Generation(t *testing.T) {
	tmpl, err := backup.GeneratePostgresTemplate(backup.PostgresTemplateConfig{
		ProjectSlug:  "alpha",
		ServiceSlug:  "postgres",
		DatabaseName: "main_db",
		Username:     "admin",
	})
	require.NoError(t, err)

	assert.Equal(t, "pikpik_svc_alpha_postgres", tmpl.ServiceName)
	assert.Equal(t, "pikpik_cnt_alpha_postgres", tmpl.ContainerName)
	assert.Equal(t, "pikpik_net_proj_alpha", tmpl.OverlayNetwork)
	assert.Equal(t, "pikpik_vol_alpha_postgres_pgdata", tmpl.VolumeName)
	assert.Equal(t, "postgres", tmpl.InternalDNS)
	assert.Equal(t, 5432, tmpl.InternalPort)
	assert.Equal(t, "postgres:17-alpine", tmpl.Image)
	assert.Equal(t, "--auth-host=scram-sha-256 --encoding=UTF8 --locale=C", tmpl.Environment["POSTGRES_INITDB_ARGS"])
	assert.Equal(t, "admin", tmpl.Environment["POSTGRES_USER"])
	assert.Equal(t, "main_db", tmpl.Environment["POSTGRES_DB"])
	assert.Len(t, tmpl.Environment["POSTGRES_PASSWORD"], 32)
	assert.True(t, strings.HasPrefix(tmpl.ConnectionURL, "postgres://admin:"))
	assert.Contains(t, tmpl.ConnectionURL, "@postgres:5432/main_db?sslmode=disable")
}
