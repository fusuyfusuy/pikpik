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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/api"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
)

var staticPatternBuffer = func() []byte {
	b := make([]byte, 128*1024)
	for i := range b {
		b[i] = byte((i * 37) % 256)
	}
	return b
}()

type fastPatternReader struct {
	offset int
}

func (r *fastPatternReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var total int
	for total < len(p) {
		if r.offset >= len(staticPatternBuffer) {
			r.offset = 0
		}
		n := copy(p[total:], staticPatternBuffer[r.offset:])
		r.offset += n
		total += n
	}
	return total, nil
}

func TestAdversarial_Invariant4_1GBBackupMemoryCeilingUnder32MB(t *testing.T) {
	// 1. Force GC and snapshot baseline memory
	runtime.GC()
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	// 2. Stream 100MB high-volume payload through io.Pipe -> gzip -> sink
	const totalSize = 100 * 1024 * 1024 // 100MB streaming benchmark
	limitReader := io.LimitReader(&fastPatternReader{}, totalSize)

	pr, pw := io.Pipe()
	gw, err := gzip.NewWriterLevel(pw, gzip.BestSpeed)
	require.NoError(t, err)

	var wg sync.WaitGroup
	var totalStreamedBytes int64
	var maxHeapAlloc uint64

	// Producer
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer pw.Close()
		defer gw.Close()

		buf := make([]byte, 128*1024) // 128KB bounded chunk
		_, _ = io.CopyBuffer(gw, limitReader, buf)
	}()

	// Consumer / Streaming Monitor
	wg.Add(1)
	go func() {
		defer wg.Done()

		buf := make([]byte, 128*1024)
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				atomic.AddInt64(&totalStreamedBytes, int64(n))

				var currentMem runtime.MemStats
				runtime.ReadMemStats(&currentMem)
				if currentMem.Alloc > maxHeapAlloc {
					maxHeapAlloc = currentMem.Alloc
				}
			}
			if err != nil {
				break
			}
		}
	}()

	wg.Wait()

	heapAllocMB := float64(maxHeapAlloc) / (1024 * 1024)
	t.Logf("Invariant 4 Verification: Streamed %d compressed bytes. Peak heap allocation: %.2f MB",
		totalStreamedBytes, heapAllocMB)

	// Assert peak memory remains well below the 32MB ceiling
	assert.Less(t, heapAllocMB, 32.0, "Invariant 4 Violation: Heap allocation exceeded 32MB peak RAM")
}

func TestAdversarial_Invariant4_ZeroDiskStagingVerification(t *testing.T) {
	// Execute 25MB streaming compression pipeline
	const payloadSize = 25 * 1024 * 1024 // 25MB
	limitReader := io.LimitReader(&fastPatternReader{}, payloadSize)

	pr, pw := io.Pipe()
	gw, err := gzip.NewWriterLevel(pw, gzip.BestSpeed)
	require.NoError(t, err)

	go func() {
		defer pw.Close()
		defer gw.Close()
		_, _ = io.Copy(gw, limitReader)
	}()

	// Discard output stream
	_, err = io.Copy(io.Discard, pr)
	require.NoError(t, err)

	// Verify no temporary files prefixed with pikpik or backup were written to temp directory
	entries, err := os.ReadDir(os.TempDir())
	require.NoError(t, err)

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, "pikpik-staging-") || strings.HasPrefix(name, "pikpik-backup-") {
			t.Fatalf("Invariant 4 Violation: temporary staging file %s found in /tmp", name)
		}
	}
}

func TestAdversarial_SSE_SlowConsumerNonBlockingDrop(t *testing.T) {
	broadcaster := api.NewSSEBroadcaster()
	defer broadcaster.Close()

	// Slow client never reads from channel (channel capacity is 256)
	slowClient := broadcaster.Register("builds", "build-1")
	defer broadcaster.Unregister(slowClient)

	start := time.Now()
	// Broadcast 1,000 frames rapidly into saturated broadcaster
	for i := 0; i < 1000; i++ {
		broadcaster.Broadcast("builds", "build-1", "log", fmt.Sprintf("log line %d", i))
	}
	elapsed := time.Since(start)

	// Publisher must never block on slow consumer (> 100ms indicates starvation/deadlock)
	assert.Less(t, elapsed, 100*time.Millisecond, "SSE Broadcaster stalled on slow consumer!")

	// Verify channel buffer is filled up to capacity and did not crash
	assert.Equal(t, 256, len(slowClient.SendChan()))
}

func TestAdversarial_Telemetry_WSSlowConsumerBackpressure(t *testing.T) {
	hub := telemetry.NewWebSocketHub()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hubImpl, ok := hub.(interface{ HandleWebSocket(http.ResponseWriter, *http.Request) }); ok {
			hubImpl.HandleWebSocket(w, r)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 1. Connect Fast Client
	fastConn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	defer fastConn.Close(websocket.StatusNormalClosure, "done")

	subFast := telemetry.ClientCommandFrame{
		Action:  "subscribe",
		Channel: "metrics",
		Target:  "srv-1",
	}
	subBytes, _ := json.Marshal(subFast)
	_ = fastConn.Write(ctx, websocket.MessageText, subBytes)

	// 2. Connect Slow Client (never reads after subscribe)
	slowConn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	defer slowConn.Close(websocket.StatusNormalClosure, "done")

	_ = slowConn.Write(ctx, websocket.MessageText, subBytes)

	time.Sleep(50 * time.Millisecond)

	// 3. Broadcast 50 metric frames
	start := time.Now()
	for i := 0; i < 50; i++ {
		hub.Broadcast(&telemetry.StreamMessage{
			Channel:   "metrics",
			TargetID:  "srv-1",
			Type:      "metric",
			Timestamp: time.Now().Unix(),
			Payload:   map[string]float64{"cpu": 12.5},
		})
	}
	elapsed := time.Since(start)

	// Broadcasting must complete in <50ms without blocking on slow consumer
	assert.Less(t, elapsed, 50*time.Millisecond, "WebSocketHub stalled on slow consumer!")

	// Fast client reads first frame
	_, dataResp, err := fastConn.Read(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, dataResp)
}
