package s3

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultPartSize       = 5 * 1024 * 1024 // 5 MB
	defaultMaxConcurrency = 4
)

// DefaultS3Client implements the S3Client interface using pure SigV4 HTTP requests.
type DefaultS3Client struct {
	opts       ClientOptions
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new Universal S3 client.
func NewClient(opts ClientOptions) (*DefaultS3Client, error) {
	if opts.Region == "" {
		if opts.Provider == ProviderR2 {
			opts.Region = "auto"
		} else {
			opts.Region = "us-east-1"
		}
	}
	if opts.PartSizeBytes <= 0 {
		opts.PartSizeBytes = defaultPartSize
	}
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = defaultMaxConcurrency
	}

	endpoint := opts.Endpoint
	if endpoint == "" {
		scheme := "https"
		if !opts.UseSSL && opts.Endpoint != "" {
			scheme = "http"
		}
		switch opts.Provider {
		case ProviderAWS:
			endpoint = fmt.Sprintf("%s://s3.%s.amazonaws.com", scheme, opts.Region)
		case ProviderBackblaze:
			endpoint = fmt.Sprintf("%s://s3.%s.backblazeb2.com", scheme, opts.Region)
		default:
			endpoint = fmt.Sprintf("%s://s3.%s.amazonaws.com", scheme, opts.Region)
		}
	}

	// Ensure endpoint has scheme
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		if opts.UseSSL {
			endpoint = "https://" + endpoint
		} else {
			endpoint = "http://" + endpoint
		}
	}
	endpoint = strings.TrimSuffix(endpoint, "/")

	return &DefaultS3Client{
		opts:       opts,
		httpClient: &http.Client{Timeout: 0}, // No client-level timeout for streaming
		baseURL:    endpoint,
	}, nil
}

// buildURL constructs the target URL for a given bucket and key.
func (c *DefaultS3Client) buildURL(key string, query url.Values) (*url.URL, error) {
	var rawURL string
	if c.opts.ForcePathStyle || c.opts.Provider == ProviderMinIO || c.opts.Provider == ProviderR2 || c.opts.Provider == ProviderBackblaze {
		if c.opts.Bucket != "" {
			if key != "" {
				rawURL = fmt.Sprintf("%s/%s/%s", c.baseURL, c.opts.Bucket, strings.TrimPrefix(key, "/"))
			} else {
				rawURL = fmt.Sprintf("%s/%s", c.baseURL, c.opts.Bucket)
			}
		} else {
			rawURL = fmt.Sprintf("%s/%s", c.baseURL, strings.TrimPrefix(key, "/"))
		}
	} else {
		// Virtual-hosted style
		u, err := url.Parse(c.baseURL)
		if err != nil {
			return nil, err
		}
		host := u.Host
		if c.opts.Bucket != "" {
			host = c.opts.Bucket + "." + host
		}
		if key != "" {
			rawURL = fmt.Sprintf("%s://%s/%s", u.Scheme, host, strings.TrimPrefix(key, "/"))
		} else {
			rawURL = fmt.Sprintf("%s://%s/", u.Scheme, host)
		}
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u, nil
}

type initiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadID string   `xml:"UploadId"`
}

type completePart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

type completeMultipartUpload struct {
	XMLName xml.Name       `xml:"CompleteMultipartUpload"`
	Parts   []completePart `xml:"Part"`
}

type completeMultipartUploadResult struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
	Location string   `xml:"Location"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	ETag     string   `xml:"ETag"`
}

type listBucketResult struct {
	XMLName               xml.Name         `xml:"ListBucketResult"`
	Name                  string           `xml:"Name"`
	Prefix                string           `xml:"Prefix"`
	IsTruncated           bool             `xml:"IsTruncated"`
	NextContinuationToken string           `xml:"NextContinuationToken"`
	Contents              []objectMetadata `xml:"Contents"`
}

type objectMetadata struct {
	Key          string `xml:"Key"`
	LastModified string `xml:"LastModified"`
	ETag         string `xml:"ETag"`
	Size         int64  `xml:"Size"`
}

type deleteObjectsPayload struct {
	XMLName xml.Name         `xml:"Delete"`
	Quiet   bool             `xml:"Quiet"`
	Objects []deleteKeyEntry `xml:"Object"`
}

type deleteKeyEntry struct {
	Key string `xml:"Key"`
}

type listMultipartUploadsResult struct {
	XMLName xml.Name          `xml:"ListMultipartUploadsResult"`
	Bucket  string            `xml:"Bucket"`
	Uploads []multipartUpload `xml:"Upload"`
}

type multipartUpload struct {
	Key       string `xml:"Key"`
	UploadID  string `xml:"UploadId"`
	Initiated string `xml:"Initiated"`
}

func parseS3Time(s string) (time.Time, error) {
	formats := []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05Z",
		time.RFC1123,
		time.RFC1123Z,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unable to parse s3 time %q", s)
}

func sleepWithJitter(ctx context.Context, attempt int) error {
	baseDelays := []time.Duration{
		200 * time.Millisecond,
		500 * time.Millisecond,
		1000 * time.Millisecond,
	}
	idx := attempt - 1
	if idx < 0 {
		idx = 0
	} else if idx >= len(baseDelays) {
		idx = len(baseDelays) - 1
	}

	base := baseDelays[idx]
	var b [1]byte
	_, _ = rand.Read(b[:])
	jitter := time.Duration(int64(b[0]) % int64(base/4+1))
	delay := base + jitter

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// UploadStreamMultipart streams data from reader directly to S3 via concurrent multipart uploads.
func (c *DefaultS3Client) UploadStreamMultipart(ctx context.Context, key string, reader io.Reader, opts UploadOptions) (*ObjectInfo, error) {
	// 1. Initiate Multipart Upload
	q := url.Values{}
	q.Set("uploads", "")
	targetURL, err := c.buildURL(key, q)
	if err != nil {
		return nil, fmt.Errorf("failed to build initiate upload url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL.String(), nil)
	if err != nil {
		return nil, err
	}
	if opts.ContentType != "" {
		req.Header.Set("Content-Type", opts.ContentType)
	} else {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	for k, v := range opts.Metadata {
		req.Header.Set("x-amz-meta-"+strings.ToLower(k), v)
	}

	SignRequest(req, "", c.opts.AccessKeyID, c.opts.SecretAccessKey, c.opts.Region, time.Now())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("initiate multipart upload failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("initiate multipart upload error HTTP %d: %s", resp.StatusCode, string(body))
	}

	var initResult initiateMultipartUploadResult
	if err := xml.NewDecoder(resp.Body).Decode(&initResult); err != nil {
		return nil, fmt.Errorf("failed to decode initiate upload response xml: %w", err)
	}
	uploadID := initResult.UploadID
	if uploadID == "" {
		return nil, errors.New("empty uploadId received from s3")
	}

	// 2. Stream & Upload Parts concurrently
	type partResult struct {
		partNumber int
		etag       string
		err        error
	}

	partSize := c.opts.PartSizeBytes
	partsChan := make(chan partResult, c.opts.MaxConcurrency)
	var totalUploadedBytes int64
	var uploadErr error

	var uploadedParts []completePart
	collectorDone := make(chan struct{})

	// Drain result channel concurrently as worker goroutines finish to eliminate deadlocks
	go func() {
		defer close(collectorDone)
		for res := range partsChan {
			if res.err != nil && uploadErr == nil {
				uploadErr = res.err
			}
			if res.err == nil {
				uploadedParts = append(uploadedParts, completePart{
					PartNumber: res.partNumber,
					ETag:       res.etag,
				})
			}
		}
	}()

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, c.opts.MaxConcurrency)

	partNumber := 1
	for {
		if ctx.Err() != nil {
			if uploadErr == nil {
				uploadErr = ctx.Err()
			}
			break
		}

		// Read up to partSize in memory (< 5MB buffer per worker)
		buf := make([]byte, partSize)
		n, rErr := io.ReadFull(reader, buf)
		if n > 0 {
			chunk := buf[:n]
			totalUploadedBytes += int64(n)
			currPart := partNumber
			partNumber++

			select {
			case semaphore <- struct{}{}:
			case <-ctx.Done():
				if uploadErr == nil {
					uploadErr = ctx.Err()
				}
				break
			}
			if uploadErr != nil {
				break
			}

			wg.Add(1)

			go func(pNum int, pData []byte) {
				defer func() {
					<-semaphore
					wg.Done()
				}()

				etag, err := c.uploadPart(ctx, key, uploadID, pNum, pData)
				select {
				case partsChan <- partResult{
					partNumber: pNum,
					etag:       etag,
					err:        err,
				}:
				case <-ctx.Done():
				}
			}(currPart, chunk)
		}

		if rErr == io.EOF || rErr == io.ErrUnexpectedEOF {
			break
		}
		if rErr != nil {
			if uploadErr == nil {
				uploadErr = rErr
			}
			break
		}
	}

	// Wait for all workers to finish and close result channel
	wg.Wait()
	close(partsChan)
	<-collectorDone

	// If error occurred, abort multipart upload to prevent orphan leaks
	if uploadErr != nil {
		_ = c.abortMultipartUpload(context.Background(), key, uploadID)
		return nil, fmt.Errorf("multipart upload failed: %w", uploadErr)
	}

	// If 0 bytes was uploaded and no parts, upload an empty part 1
	if len(uploadedParts) == 0 {
		etag, err := c.uploadPart(ctx, key, uploadID, 1, []byte{})
		if err != nil {
			_ = c.abortMultipartUpload(context.Background(), key, uploadID)
			return nil, err
		}
		uploadedParts = append(uploadedParts, completePart{PartNumber: 1, ETag: etag})
	}

	// Sort parts by PartNumber ascending
	sort.Slice(uploadedParts, func(i, j int) bool {
		return uploadedParts[i].PartNumber < uploadedParts[j].PartNumber
	})

	// 3. Complete Multipart Upload
	compPayload := completeMultipartUpload{Parts: uploadedParts}
	compXML, err := xml.Marshal(compPayload)
	if err != nil {
		_ = c.abortMultipartUpload(context.Background(), key, uploadID)
		return nil, err
	}

	cq := url.Values{}
	cq.Set("uploadId", uploadID)
	compURL, err := c.buildURL(key, cq)
	if err != nil {
		_ = c.abortMultipartUpload(context.Background(), key, uploadID)
		return nil, err
	}

	compReq, err := http.NewRequestWithContext(ctx, http.MethodPost, compURL.String(), bytes.NewReader(compXML))
	if err != nil {
		_ = c.abortMultipartUpload(context.Background(), key, uploadID)
		return nil, err
	}
	compReq.Header.Set("Content-Type", "application/xml")
	bodyHash := sha256Hex(compXML)
	SignRequest(compReq, bodyHash, c.opts.AccessKeyID, c.opts.SecretAccessKey, c.opts.Region, time.Now())

	compResp, err := c.httpClient.Do(compReq)
	if err != nil {
		_ = c.abortMultipartUpload(context.Background(), key, uploadID)
		return nil, fmt.Errorf("complete multipart upload failed: %w", err)
	}
	defer compResp.Body.Close()

	if compResp.StatusCode < 200 || compResp.StatusCode >= 300 {
		body, _ := io.ReadAll(compResp.Body)
		_ = c.abortMultipartUpload(context.Background(), key, uploadID)
		return nil, fmt.Errorf("complete multipart upload error HTTP %d: %s", compResp.StatusCode, string(body))
	}

	var compResult completeMultipartUploadResult
	_ = xml.NewDecoder(compResp.Body).Decode(&compResult)

	finalETag := compResult.ETag
	if finalETag == "" {
		finalETag = compResp.Header.Get("ETag")
	}

	return &ObjectInfo{
		Key:          key,
		Size:         totalUploadedBytes,
		ETag:         finalETag,
		LastModified: time.Now().UTC(),
	}, nil
}

func (c *DefaultS3Client) uploadPart(ctx context.Context, key, uploadID string, partNumber int, data []byte) (string, error) {
	q := url.Values{}
	q.Set("partNumber", fmt.Sprintf("%d", partNumber))
	q.Set("uploadId", uploadID)

	partURL, err := c.buildURL(key, q)
	if err != nil {
		return "", err
	}

	bodyHash := sha256Hex(data)
	const maxAttempts = 3
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPut, partURL.String(), bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Length", fmt.Sprintf("%d", len(data)))
		SignRequest(req, bodyHash, c.opts.AccessKeyID, c.opts.SecretAccessKey, c.opts.Region, time.Now())

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if attempt < maxAttempts {
				if sleepErr := sleepWithJitter(ctx, attempt); sleepErr != nil {
					return "", sleepErr
				}
				continue
			}
			return "", fmt.Errorf("upload part %d failed: %w", partNumber, lastErr)
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			etag := resp.Header.Get("ETag")
			_ = resp.Body.Close()
			return etag, nil
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		lastErr = fmt.Errorf("upload part %d error HTTP %d: %s", partNumber, resp.StatusCode, string(body))

		// Retry on 5xx server errors
		if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
			if attempt < maxAttempts {
				if sleepErr := sleepWithJitter(ctx, attempt); sleepErr != nil {
					return "", sleepErr
				}
				continue
			}
		}

		// Non-retryable error (e.g. 4xx)
		return "", lastErr
	}

	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("upload part %d failed after %d attempts", partNumber, maxAttempts)
}

func (c *DefaultS3Client) abortMultipartUpload(ctx context.Context, key, uploadID string) error {
	q := url.Values{}
	q.Set("uploadId", uploadID)
	abortURL, err := c.buildURL(key, q)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, abortURL.String(), nil)
	if err != nil {
		return err
	}
	SignRequest(req, "", c.opts.AccessKeyID, c.opts.SecretAccessKey, c.opts.Region, time.Now())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("abort multipart upload HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// DownloadStream returns an io.ReadCloser streaming data directly from S3 without saving to disk.
func (c *DefaultS3Client) DownloadStream(ctx context.Context, key string) (io.ReadCloser, *ObjectInfo, error) {
	targetURL, err := c.buildURL(key, nil)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	SignRequest(req, "", c.opts.AccessKeyID, c.opts.SecretAccessKey, c.opts.Region, time.Now())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("download request failed: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, nil, fmt.Errorf("s3 object %q not found", key)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, nil, fmt.Errorf("download stream error HTTP %d: %s", resp.StatusCode, string(body))
	}

	lastMod, _ := time.Parse(time.RFC1123, resp.Header.Get("Last-Modified"))
	info := &ObjectInfo{
		Key:          key,
		Size:         resp.ContentLength,
		ETag:         resp.Header.Get("ETag"),
		LastModified: lastMod,
	}

	return resp.Body, info, nil
}

// ListObjects returns objects matching a given prefix with continuation-token pagination.
func (c *DefaultS3Client) ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	var objects []ObjectInfo
	var continuationToken string

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		q := url.Values{}
		q.Set("list-type", "2")
		if prefix != "" {
			q.Set("prefix", prefix)
		}
		if continuationToken != "" {
			q.Set("continuation-token", continuationToken)
		}

		targetURL, err := c.buildURL("", q)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
		if err != nil {
			return nil, err
		}
		SignRequest(req, "", c.opts.AccessKeyID, c.opts.SecretAccessKey, c.opts.Region, time.Now())

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("list objects failed: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("list objects error HTTP %d: %s", resp.StatusCode, string(body))
		}

		var listResult listBucketResult
		if err := xml.NewDecoder(resp.Body).Decode(&listResult); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode list objects xml: %w", err)
		}
		resp.Body.Close()

		for _, item := range listResult.Contents {
			t, _ := time.Parse(time.RFC3339, item.LastModified)
			if t.IsZero() {
				t, _ = parseS3Time(item.LastModified)
			}
			objects = append(objects, ObjectInfo{
				Key:          item.Key,
				Size:         item.Size,
				ETag:         item.ETag,
				LastModified: t,
			})
		}

		if !listResult.IsTruncated || listResult.NextContinuationToken == "" {
			break
		}
		continuationToken = listResult.NextContinuationToken
	}

	return objects, nil
}

// DeleteObjects performs batch deletion of specified keys.
func (c *DefaultS3Client) DeleteObjects(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	var entries []deleteKeyEntry
	for _, k := range keys {
		entries = append(entries, deleteKeyEntry{Key: k})
	}
	payload := deleteObjectsPayload{
		Quiet:   true,
		Objects: entries,
	}
	data, err := xml.Marshal(payload)
	if err != nil {
		return err
	}

	q := url.Values{}
	q.Set("delete", "")
	targetURL, err := c.buildURL("", q)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")
	bodyHash := sha256Hex(data)
	SignRequest(req, bodyHash, c.opts.AccessKeyID, c.opts.SecretAccessKey, c.opts.Region, time.Now())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete objects failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete objects error HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// PruneRetention evaluates existing objects under a prefix and deletes those exceeding retention policy.
func (c *DefaultS3Client) PruneRetention(ctx context.Context, prefix string, policy RetentionPolicy) ([]string, error) {
	objects, err := c.ListObjects(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list objects for retention pruning: %w", err)
	}

	pruner := NewRetentionEngine(RetentionEngineOptions{ReferenceTime: time.Now().UTC()})
	toDelete, _ := pruner.EvaluateRetention(objects, policy)

	if len(toDelete) > 0 {
		if err := c.DeleteObjects(ctx, toDelete); err != nil {
			return nil, fmt.Errorf("failed to delete pruned objects: %w", err)
		}
	}

	return toDelete, nil
}

// PruneStaleMultipartUploads aborts in-progress multipart uploads older than maxAge.
func (c *DefaultS3Client) PruneStaleMultipartUploads(ctx context.Context, maxAge time.Duration) ([]string, error) {
	if maxAge <= 0 {
		maxAge = 24 * time.Hour
	}

	q := url.Values{}
	q.Set("uploads", "")
	targetURL, err := c.buildURL("", q)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
	if err != nil {
		return nil, err
	}
	SignRequest(req, "", c.opts.AccessKeyID, c.opts.SecretAccessKey, c.opts.Region, time.Now())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list multipart uploads failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list multipart uploads error HTTP %d: %s", resp.StatusCode, string(body))
	}

	var listResult listMultipartUploadsResult
	if err := xml.NewDecoder(resp.Body).Decode(&listResult); err != nil {
		return nil, fmt.Errorf("failed to decode list multipart uploads xml: %w", err)
	}

	now := time.Now().UTC()
	var pruned []string
	for _, upload := range listResult.Uploads {
		if upload.UploadID == "" || upload.Key == "" {
			continue
		}
		initiatedTime, err := parseS3Time(upload.Initiated)
		if err != nil {
			continue
		}
		if now.Sub(initiatedTime) > maxAge {
			if abortErr := c.abortMultipartUpload(ctx, upload.Key, upload.UploadID); abortErr == nil {
				pruned = append(pruned, upload.UploadID)
			}
		}
	}

	return pruned, nil
}
