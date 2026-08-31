package s3_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fusuycorp/pikpik/pkg/backup/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdversarial_S3_ZeroByteStreamMultipartUpload(t *testing.T) {
	var mu sync.Mutex
	uploadedData := make(map[string][]byte)
	uploadParts := make(map[string]map[int][]byte)
	var abortedUploads []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		q := r.URL.Query()
		key := strings.TrimPrefix(r.URL.Path, "/zero-bucket/")

		if r.Method == http.MethodPost && q.Has("uploads") {
			uploadID := "zero-upload-id-1"
			uploadParts[uploadID] = make(map[int][]byte)
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<InitiateMultipartUploadResult><Bucket>zero-bucket</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, key, uploadID)
			return
		}

		if r.Method == http.MethodPut && q.Has("uploadId") {
			uploadID := q.Get("uploadId")
			partNum := 0
			fmt.Sscanf(q.Get("partNumber"), "%d", &partNum)
			data, _ := io.ReadAll(r.Body)
			uploadParts[uploadID][partNum] = data
			w.Header().Set("ETag", fmt.Sprintf(`"etag-zero-%d"`, partNum))
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodPost && q.Has("uploadId") {
			uploadID := q.Get("uploadId")
			parts := uploadParts[uploadID]
			var combined bytes.Buffer
			for i := 1; i <= len(parts); i++ {
				combined.Write(parts[i])
			}
			uploadedData[key] = combined.Bytes()
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<CompleteMultipartUploadResult><Bucket>zero-bucket</Bucket><Key>%s</Key><ETag>"final-zero-etag"</ETag></CompleteMultipartUploadResult>`, key)
			return
		}

		if r.Method == http.MethodDelete && q.Has("uploadId") {
			abortedUploads = append(abortedUploads, q.Get("uploadId"))
			w.WriteHeader(http.StatusNoContent)
			return
		}

		http.Error(w, "Not Handled", http.StatusBadRequest)
	}))
	defer ts.Close()

	client, err := s3.NewClient(s3.ClientOptions{
		Provider:        s3.ProviderMinIO,
		Endpoint:        ts.URL,
		Bucket:          "zero-bucket",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		PartSizeBytes:   5 * 1024 * 1024,
	})
	require.NoError(t, err)

	ctx := context.Background()
	zeroReader := bytes.NewReader([]byte{})

	objInfo, err := client.UploadStreamMultipart(ctx, "empty-file.txt", zeroReader, s3.UploadOptions{
		ContentType: "text/plain",
	})
	require.NoError(t, err)
	assert.NotNil(t, objInfo)
	assert.Equal(t, int64(0), objInfo.Size)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, 0, len(abortedUploads), "0-byte upload should not abort")
}

func TestAdversarial_S3_AbortedMultipartUploadCleansOrphanParts(t *testing.T) {
	var mu sync.Mutex
	uploadParts := make(map[string]map[int][]byte)
	var abortedUploads []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		q := r.URL.Query()
		key := strings.TrimPrefix(r.URL.Path, "/abort-bucket/")

		if r.Method == http.MethodPost && q.Has("uploads") {
			uploadID := "abort-upload-id-99"
			uploadParts[uploadID] = make(map[int][]byte)
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprintf(w, `<InitiateMultipartUploadResult><Bucket>abort-bucket</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, key, uploadID)
			return
		}

		if r.Method == http.MethodPut && q.Has("uploadId") {
			uploadID := q.Get("uploadId")
			partNum := 0
			fmt.Sscanf(q.Get("partNumber"), "%d", &partNum)
			data, _ := io.ReadAll(r.Body)
			uploadParts[uploadID][partNum] = data
			w.Header().Set("ETag", fmt.Sprintf(`"etag-abort-%d"`, partNum))
			w.WriteHeader(http.StatusOK)
			return
		}

		if r.Method == http.MethodDelete && q.Has("uploadId") {
			uploadID := q.Get("uploadId")
			abortedUploads = append(abortedUploads, uploadID)
			delete(uploadParts, uploadID)
			w.WriteHeader(http.StatusNoContent)
			return
		}

		http.Error(w, "Not Handled", http.StatusBadRequest)
	}))
	defer ts.Close()

	client, err := s3.NewClient(s3.ClientOptions{
		Provider:        s3.ProviderMinIO,
		Endpoint:        ts.URL,
		Bucket:          "abort-bucket",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
		PartSizeBytes:   1024, // 1KB chunks
		MaxConcurrency:  1,
	})
	require.NoError(t, err)

	// Stream that fails abruptly on 3rd chunk
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write(make([]byte, 2048)) // Write 2 parts
		_ = pw.CloseWithError(errors.New("simulated network connection drop mid-stream"))
	}()

	ctx := context.Background()
	_, uploadErr := client.UploadStreamMultipart(ctx, "aborted-stream.bin", pr, s3.UploadOptions{})

	assert.Error(t, uploadErr, "expected error on broken stream")

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, abortedUploads, "abort-upload-id-99", "expected S3 client to send DELETE /?uploadId to clean orphaned parts")
}

func TestAdversarial_S3_CorruptedGzipHeaderAndDecompression(t *testing.T) {
	// 1. Truncated gzip header (only 2 bytes)
	truncatedHeader := []byte{0x1f, 0x8b}
	_, err := gzip.NewReader(bytes.NewReader(truncatedHeader))
	assert.Error(t, err, "expected error on truncated gzip header")

	// 2. Corrupted payload mid-stream
	var validBuf bytes.Buffer
	gw := gzip.NewWriter(&validBuf)
	_, _ = gw.Write([]byte("Hello world, this is a valid compressible payload for testing!"))
	_ = gw.Close()

	validBytes := validBuf.Bytes()
	corruptedBytes := make([]byte, len(validBytes))
	copy(corruptedBytes, validBytes)
	// Corrupt middle bytes
	for i := len(corruptedBytes) / 2; i < len(corruptedBytes)/2+5; i++ {
		corruptedBytes[i] ^= 0xff
	}

	gr, err := gzip.NewReader(bytes.NewReader(corruptedBytes))
	if err == nil {
		// Reading corrupted decompressed data should fail with checksum or header error
		_, readErr := io.ReadAll(gr)
		assert.Error(t, readErr, "expected error decompressing tampered gzip body")
		_ = gr.Close()
	}

	// 3. Tampered CRC32 footer
	tamperedFooterBytes := make([]byte, len(validBytes))
	copy(tamperedFooterBytes, validBytes)
	tamperedFooterBytes[len(tamperedFooterBytes)-2] ^= 0x55 // Flip CRC32 footer byte

	grFooter, err := gzip.NewReader(bytes.NewReader(tamperedFooterBytes))
	if err == nil {
		_, readErr := io.ReadAll(grFooter)
		assert.Error(t, readErr, "expected CRC32 error on tampered footer")
		_ = grFooter.Close()
	}
}
