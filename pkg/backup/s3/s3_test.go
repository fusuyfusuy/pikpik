package s3_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/backup/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRetentionPruning_GFSAlgorithm verifies Grandfather-Father-Son pruning logic.
func TestRetentionPruning_GFSAlgorithm(t *testing.T) {
	now := time.Date(2026, 8, 30, 17, 0, 0, 0, time.UTC)

	// Create 30 days of mock hourly backups
	var mockObjects []s3.ObjectInfo
	for day := 30; day >= 0; day-- {
		for hour := 0; hour < 24; hour += 6 { // 4 backups a day
			ts := now.AddDate(0, 0, -day).Add(time.Duration(hour) * time.Hour)
			mockObjects = append(mockObjects, s3.ObjectInfo{
				Key:          "backups/proj/svc/" + ts.Format(time.RFC3339) + "_dump.gz",
				LastModified: ts,
			})
		}
	}

	policy := s3.RetentionPolicy{
		KeepHourly:  12, // Keep last 12 hours
		KeepDaily:   7,  // Keep 1 per day for 7 days
		KeepWeekly:  4,  // Keep 1 per week for 4 weeks
		KeepMonthly: 12, // Keep 1 per month
		MaxBackups:  50,
	}

	pruner := s3.NewRetentionEngine(s3.RetentionEngineOptions{ReferenceTime: now})
	toDelete, toRetain := pruner.EvaluateRetention(mockObjects, policy)

	if len(toRetain) > policy.MaxBackups {
		t.Errorf("retained backups (%d) exceeded MaxBackups limit (%d)", len(toRetain), policy.MaxBackups)
	}

	if len(toDelete)+len(toRetain) != len(mockObjects) {
		t.Errorf("total evaluated objects mismatch: %d != %d", len(toDelete)+len(toRetain), len(mockObjects))
	}

	assert.NotEmpty(t, toRetain)
	assert.NotEmpty(t, toDelete)
}

func TestSigV4Signer_RequestHeaders(t *testing.T) {
	req, err := http.NewRequest("GET", "https://s3.us-east-1.amazonaws.com/my-bucket/test-key.txt", nil)
	require.NoError(t, err)

	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	s3.SignRequest(req, "", "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "us-east-1", now)

	auth := req.Header.Get("Authorization")
	assert.True(t, strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20260830/us-east-1/s3/aws4_request"))
	assert.Contains(t, auth, "SignedHeaders=")
	assert.Contains(t, auth, "Signature=")
	assert.Equal(t, "20260830T120000Z", req.Header.Get("x-amz-date"))
}

func TestS3Client_MockServer_MultipartUploadAndDownload(t *testing.T) {
	var mu sync.Mutex
	uploadedData := make(map[string][]byte)
	uploadParts := make(map[string]map[int][]byte)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		q := r.URL.Query()
		key := strings.TrimPrefix(r.URL.Path, "/test-bucket/")

		if r.Method == http.MethodPost && q.Has("uploads") {
			// Initiate Multipart
			uploadID := "mock-upload-123"
			uploadParts[uploadID] = make(map[int][]byte)
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<InitiateMultipartUploadResult><Bucket>test-bucket</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, key, uploadID)
			return
		}

		if r.Method == http.MethodPut && q.Has("uploadId") {
			// Upload Part
			uploadID := q.Get("uploadId")
			partNum := 0
			fmt.Sscanf(q.Get("partNumber"), "%d", &partNum)
			data, _ := io.ReadAll(r.Body)
			uploadParts[uploadID][partNum] = data
			w.Header().Set("ETag", fmt.Sprintf(`"etag-part-%d"`, partNum))
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodPost && q.Has("uploadId") {
			// Complete Multipart
			uploadID := q.Get("uploadId")
			parts := uploadParts[uploadID]
			var combined bytes.Buffer
			for i := 1; i <= len(parts); i++ {
				combined.Write(parts[i])
			}
			uploadedData[key] = combined.Bytes()
			w.Header().Set("Content-Type", "application/xml")
			w.Header().Set("ETag", `"final-etag-123"`)
			fmt.Fprintf(w, `<CompleteMultipartUploadResult><Bucket>test-bucket</Bucket><Key>%s</Key><ETag>"final-etag-123"</ETag></CompleteMultipartUploadResult>`, key)
			return
		}

		if r.Method == http.MethodGet && q.Get("list-type") == "2" {
			// List Objects
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<ListBucketResult><Name>test-bucket</Name><Contents><Key>%s</Key><Size>%d</Size><ETag>"final-etag-123"</ETag><LastModified>2026-08-30T17:00:00Z</LastModified></Contents></ListBucketResult>`, key, len(uploadedData[key]))
			return
		}

		if r.Method == http.MethodGet {
			// Download Object
			data, exists := uploadedData[key]
			if !exists {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("ETag", `"final-etag-123"`)
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data)
			return
		}

		if r.Method == http.MethodPost && q.Has("delete") {
			// Delete Objects
			w.WriteHeader(http.StatusOK)
			return
		}

		http.Error(w, "Not Handled", http.StatusBadRequest)
	}))
	defer ts.Close()

	client, err := s3.NewClient(s3.ClientOptions{
		Provider:        s3.ProviderMinIO,
		Endpoint:        ts.URL,
		Bucket:          "test-bucket",
		AccessKeyID:     "minio-access",
		SecretAccessKey: "minio-secret",
		ForcePathStyle:  true,
		PartSizeBytes:   1024 * 1024, // 1MB parts for fast test
	})
	require.NoError(t, err)

	ctx := context.Background()

	// 1. Upload Stream Multipart
	payload := bytes.Repeat([]byte("PIKPIK_STREAM_DATA_CHUNK_"), 100000) // ~2.5 MB (crosses 1MB part boundary)
	info, err := client.UploadStreamMultipart(ctx, "test-stream.dump.gz", bytes.NewReader(payload), s3.UploadOptions{
		ContentType: "application/gzip",
		Metadata: map[string]string{
			"backup-id": "bk_123",
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "test-stream.dump.gz", info.Key)
	assert.Equal(t, int64(len(payload)), info.Size)

	// 2. Download Stream
	reader, downInfo, err := client.DownloadStream(ctx, "test-stream.dump.gz")
	require.NoError(t, err)
	defer reader.Close()

	downloaded, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, payload, downloaded)
	assert.Equal(t, int64(len(payload)), downInfo.Size)

	// 3. List Objects
	objs, err := client.ListObjects(ctx, "test-stream")
	require.NoError(t, err)
	assert.NotEmpty(t, objs)

	// 4. Delete Objects
	err = client.DeleteObjects(ctx, []string{"test-stream.dump.gz"})
	require.NoError(t, err)
}
