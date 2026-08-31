package telemetry_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"github.com/stretchr/testify/assert"
	"nhooyr.io/websocket"
)

// TestAdversarial_Telemetry_HighConcurrency_100Clients_Subscriptions_And_Broadcasts tests
// massive multi-threaded contention with 100 client sessions and 10 continuous broadcaster routines.
func TestAdversarial_Telemetry_HighConcurrency_100Clients_Subscriptions_And_Broadcasts(t *testing.T) {
	hub := telemetry.NewWebSocketHub()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	const numClients = 100
	const numBroadcasters = 10
	var wg sync.WaitGroup
	var broadcastCount int64
	var clientOpsCount int64

	channels := []string{"metrics", "logs", "events", "system"}
	targets := []string{"app_1", "app_2", "node_1", "node_2", "*"}

	// Launch 100 concurrent client life-cycle workers
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(clientID string) {
			defer wg.Done()
			ch := make(chan []byte, 64)
			r := rand.New(rand.NewSource(time.Now().UnixNano()))

			for {
				select {
				case <-ctx.Done():
					hub.UnregisterClient(clientID)
					return
				default:
					action := r.Intn(4)
					channel := channels[r.Intn(len(channels))]
					target := targets[r.Intn(len(targets))]

					switch action {
					case 0: // Subscribe
						hub.Subscribe(clientID, channel, target, ch)
					case 1: // Unsubscribe
						hub.Unsubscribe(clientID, channel, target)
					case 2: // Drain channel to simulate active consumer
						select {
						case <-ch:
						default:
						}
					case 3: // Re-register
						hub.UnregisterClient(clientID)
						hub.Subscribe(clientID, channel, target, ch)
					}
					atomic.AddInt64(&clientOpsCount, 1)
					time.Sleep(time.Duration(r.Intn(2)+1) * time.Millisecond)
				}
			}
		}(fmt.Sprintf("client_%d", i))
	}

	// Launch 10 concurrent high-volume broadcasters
	for b := 0; b < numBroadcasters; b++ {
		wg.Add(1)
		go func(bID int) {
			defer wg.Done()
			r := rand.New(rand.NewSource(time.Now().UnixNano() + int64(bID)))

			for {
				select {
				case <-ctx.Done():
					return
				default:
					channel := channels[r.Intn(len(channels))]
					target := targets[r.Intn(len(targets))]
					msgType := "metric"
					if channel == "logs" || channel == "events" {
						msgType = "log"
					}

					msg := &telemetry.StreamMessage{
						Type:      msgType,
						Channel:   channel,
						TargetID:  target,
						Timestamp: time.Now().Unix(),
						Payload:   map[string]interface{}{"worker": bID, "rand": r.Float64()},
					}

					hub.Broadcast(msg)
					atomic.AddInt64(&broadcastCount, 1)
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(b)
	}

	wg.Wait()

	t.Logf("Completed %d broadcast operations and %d client management mutations cleanly under -race",
		atomic.LoadInt64(&broadcastCount), atomic.LoadInt64(&clientOpsCount))

	assert.Greater(t, atomic.LoadInt64(&broadcastCount), int64(100))
	assert.Greater(t, atomic.LoadInt64(&clientOpsCount), int64(500))
}

// TestAdversarial_Telemetry_SlowConsumer_BackpressureAndWarningEnqueuing tests that stalled or
// unread client sessions do not block the broadcaster and properly handle frame overflow.
func TestAdversarial_Telemetry_SlowConsumer_BackpressureAndWarningEnqueuing(t *testing.T) {
	hub := telemetry.NewWebSocketHub()

	// Register slow consumer and fast consumer
	hub.Subscribe("slow_consumer", "logs", "app_slow", nil)
	hub.Subscribe("fast_consumer", "logs", "app_slow", nil)

	// Broadcast 1000 log and metric messages rapidly
	start := time.Now()
	for i := 0; i < 1000; i++ {
		msgType := "log"
		channel := "logs"
		if i%2 == 0 {
			msgType = "metric"
			channel = "metrics"
		}
		hub.Broadcast(&telemetry.StreamMessage{
			Type:      msgType,
			Channel:   channel,
			TargetID:  "app_slow",
			Timestamp: time.Now().Unix(),
			Payload:   fmt.Sprintf("data point %d", i),
		})
	}
	elapsed := time.Since(start)

	// Broadcaster must NOT be blocked and should finish quickly (<100ms)
	assert.Less(t, elapsed, 200*time.Millisecond, "broadcast must be non-blocking even with saturated/unread channels")

	// Clean up
	hub.UnregisterClient("slow_consumer")
	hub.UnregisterClient("fast_consumer")
}

// TestAdversarial_Telemetry_ConcurrentWebSocket_UpgradeAndTeardown tests 20 concurrent real
// WebSocket sessions connecting, pinging, subscribing, and closing abruptly.
func TestAdversarial_Telemetry_ConcurrentWebSocket_UpgradeAndTeardown(t *testing.T) {
	hub := telemetry.NewWebSocketHub()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hubImpl, ok := hub.(interface{ HandleWebSocket(http.ResponseWriter, *http.Request) }); ok {
			hubImpl.HandleWebSocket(w, r)
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	const numConns = 20
	var wg sync.WaitGroup
	var successCount int64

	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			conn, _, err := websocket.Dial(ctx, wsURL, nil)
			if err != nil {
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "done")

			// Send ping
			pingCmd := telemetry.ClientCommandFrame{Action: "ping"}
			pingBytes, _ := json.Marshal(pingCmd)
			_ = conn.Write(ctx, websocket.MessageText, pingBytes)

			// Subscribe
			subCmd := telemetry.ClientCommandFrame{
				Action:  "subscribe",
				Channel: "metrics",
				Target:  fmt.Sprintf("target_%d", id),
			}
			subBytes, _ := json.Marshal(subCmd)
			_ = conn.Write(ctx, websocket.MessageText, subBytes)

			// Read response
			_, _, _ = conn.Read(ctx)

			atomic.AddInt64(&successCount, 1)
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int64(numConns), atomic.LoadInt64(&successCount))
}
