package telemetry_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nhooyr.io/websocket"
)

// TestWebSocketHubBroadcastRouting tests channel and target filtering logic.
func TestWebSocketHubBroadcastRouting(t *testing.T) {
	hub := telemetry.NewWebSocketHub()

	client1Chan := make(chan []byte, 10)
	client2Chan := make(chan []byte, 10)

	// client1 subscribes to node:worker-01 metrics
	hub.Subscribe("c1", "metrics", "node:worker-01", client1Chan)
	// client2 subscribes to wildcard * metrics
	hub.Subscribe("c2", "metrics", "*", client2Chan)

	// Broadcast matching message
	msg1 := &telemetry.StreamMessage{
		Type:      "metric",
		Channel:   "metrics",
		TargetID:  "node:worker-01",
		Timestamp: time.Now().Unix(),
		Payload:   map[string]interface{}{"cpu": 15.5},
	}
	hub.Broadcast(msg1)

	// Broadcast non-matching message (node:worker-02)
	msg2 := &telemetry.StreamMessage{
		Type:      "metric",
		Channel:   "metrics",
		TargetID:  "node:worker-02",
		Timestamp: time.Now().Unix(),
		Payload:   map[string]interface{}{"cpu": 25.0},
	}
	hub.Broadcast(msg2)

	// Unsubscribe c1
	hub.Unsubscribe("c1", "metrics", "node:worker-01")

	msg3 := &telemetry.StreamMessage{
		Type:      "metric",
		Channel:   "metrics",
		TargetID:  "node:worker-01",
		Timestamp: time.Now().Unix(),
		Payload:   map[string]interface{}{"cpu": 30.0},
	}
	hub.Broadcast(msg3)

	// Clean up
	hub.UnregisterClient("c1")
	hub.UnregisterClient("c2")
}

// TestWebSocketHubE2E tests real WebSocket client connection, subscription, and frame reception.
func TestWebSocketHubE2E(t *testing.T) {
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

	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	require.NoError(t, err)
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// 1. Send Ping command
	pingCmd := telemetry.ClientCommandFrame{
		Action: "ping",
	}
	pingBytes, _ := json.Marshal(pingCmd)
	err = conn.Write(ctx, websocket.MessageText, pingBytes)
	require.NoError(t, err)

	_, resp, err := conn.Read(ctx)
	require.NoError(t, err)
	var pongResp map[string]string
	err = json.Unmarshal(resp, &pongResp)
	require.NoError(t, err)
	assert.Equal(t, "pong", pongResp["action"])

	// 2. Subscribe to metrics for container:c_1
	subCmd := telemetry.ClientCommandFrame{
		Action:  "subscribe",
		Channel: "metrics",
		Target:  "container:c_1",
	}
	subBytes, _ := json.Marshal(subCmd)
	err = conn.Write(ctx, websocket.MessageText, subBytes)
	require.NoError(t, err)

	// Short sleep for registration
	time.Sleep(50 * time.Millisecond)

	// 3. Broadcast telemetry frame from server
	hub.Broadcast(&telemetry.StreamMessage{
		Type:      "metric",
		Channel:   "metrics",
		TargetID:  "container:c_1",
		Timestamp: 1725000000,
		Payload:   map[string]float64{"cpu_percent": 42.0},
	})

	_, dataResp, err := conn.Read(ctx)
	require.NoError(t, err)

	var frame telemetry.ServerDataFrame
	err = json.Unmarshal(dataResp, &frame)
	require.NoError(t, err)
	assert.Equal(t, "metrics", frame.Channel)
	assert.Equal(t, "container:c_1", frame.Target)
	assert.Equal(t, int64(1725000000), frame.Timestamp)
}
