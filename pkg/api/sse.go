package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SSEClient represents an individual connected SSE HTTP streaming client.
type SSEClient struct {
	channel  string
	targetID string
	send     chan []byte
	closed   bool
	closeMu  sync.Mutex
}

func newSSEClient(channel, targetID string, bufferSize int) *SSEClient {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	if channel == "" {
		channel = "*"
	}
	if targetID == "" {
		targetID = "*"
	}
	return &SSEClient{
		channel:  channel,
		targetID: targetID,
		send:     make(chan []byte, bufferSize),
	}
}

// SendChan returns a read-only channel for client frames.
func (c *SSEClient) SendChan() <-chan []byte {
	return c.send
}

// close safely closes the client's send channel once.
func (c *SSEClient) close() {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if !c.closed {
		c.closed = true
		close(c.send)
	}
}

// SSEBroadcaster coordinates real-time Server-Sent Events broadcasting to HTTP clients.
type SSEBroadcaster struct {
	mu        sync.RWMutex
	clients   map[*SSEClient]struct{}
	heartbeat time.Duration
}

// NewSSEBroadcaster creates and initializes a new SSEBroadcaster.
func NewSSEBroadcaster() *SSEBroadcaster {
	return &SSEBroadcaster{
		clients:   make(map[*SSEClient]struct{}),
		heartbeat: 15 * time.Second,
	}
}

// SetHeartbeat updates the keep-alive ping interval.
func (b *SSEBroadcaster) SetHeartbeat(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.heartbeat = d
}

// Register adds a new client subscriber to the broadcaster.
func (b *SSEBroadcaster) Register(channel, targetID string) *SSEClient {
	client := newSSEClient(channel, targetID, 256)
	b.mu.Lock()
	b.clients[client] = struct{}{}
	b.mu.Unlock()
	return client
}

// Unregister removes and closes a client subscriber.
func (b *SSEBroadcaster) Unregister(client *SSEClient) {
	if client == nil {
		return
	}
	b.mu.Lock()
	delete(b.clients, client)
	b.mu.Unlock()
	client.close()
}

// ClientCount returns the current number of active SSE subscribers.
func (b *SSEBroadcaster) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// Close disconnects and cleans up all connected SSE subscribers.
func (b *SSEBroadcaster) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for client := range b.clients {
		delete(b.clients, client)
		client.close()
	}
}

// FormatSSE constructs a standards-compliant Server-Sent Event byte frame.
func FormatSSE(id, event string, data any) []byte {
	var b strings.Builder
	if id != "" {
		b.WriteString("id: ")
		b.WriteString(id)
		b.WriteByte('\n')
	}
	if event != "" {
		b.WriteString("event: ")
		b.WriteString(event)
		b.WriteByte('\n')
	}

	var payload string
	switch v := data.(type) {
	case string:
		payload = v
	case []byte:
		payload = string(v)
	default:
		if jsonBytes, err := json.Marshal(v); err == nil {
			payload = string(jsonBytes)
		} else {
			payload = fmt.Sprintf("%v", v)
		}
	}

	lines := strings.Split(payload, "\n")
	for _, line := range lines {
		b.WriteString("data: ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	return []byte(b.String())
}

// Broadcast serializes and pushes an event to all matching subscribers.
func (b *SSEBroadcaster) Broadcast(channel, targetID, event string, data any) {
	frame := FormatSSE(fmt.Sprintf("%d", time.Now().UnixNano()), event, data)
	b.BroadcastRaw(channel, targetID, frame)
}

// BroadcastRaw pushes a pre-formatted SSE frame to all matching subscribers.
func (b *SSEBroadcaster) BroadcastRaw(channel, targetID string, frame []byte) {
	if len(frame) == 0 {
		return
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for client := range b.clients {
		channelMatch := client.channel == "*" || client.channel == channel
		targetMatch := client.targetID == "*" || client.targetID == "" || client.targetID == targetID

		if channelMatch && targetMatch {
			select {
			case client.send <- frame:
			default:
				// Slow consumer: drop frame to prevent starvation and deadlock
			}
		}
	}
}

// ServeStream handles standard SSE client streaming over HTTP.
func (b *SSEBroadcaster) ServeStream(w http.ResponseWriter, r *http.Request, channel, targetID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	client := b.Register(channel, targetID)
	defer b.Unregister(client)

	// Flush headers immediately
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	b.mu.RLock()
	heartbeatInterval := b.heartbeat
	b.mu.RUnlock()

	if heartbeatInterval <= 0 {
		heartbeatInterval = 15 * time.Second
	}
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-client.send:
			if !ok {
				return
			}
			if _, err := w.Write(frame); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ServeEventsStream handles GET /api/v1/events/stream.
func (b *SSEBroadcaster) ServeEventsStream(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		targetID = "*"
	}
	b.ServeStream(w, r, "events", targetID)
}

// ServeLogsStream handles GET /api/v1/logs/stream?target_id={id}&tail={n}&follow={bool}.
func (b *SSEBroadcaster) ServeLogsStream(w http.ResponseWriter, r *http.Request, targetID string) {
	if targetID == "" {
		targetID = r.URL.Query().Get("target_id")
	}
	if targetID == "" {
		targetID = r.URL.Query().Get("service_id")
	}
	if targetID == "" {
		targetID = r.URL.Query().Get("app_id")
	}
	if targetID == "" {
		targetID = r.URL.Query().Get("container_id")
	}
	if targetID == "" {
		targetID = "*"
	}

	followStr := r.URL.Query().Get("follow")
	follow := true
	if followStr != "" {
		if parsed, err := strconv.ParseBool(followStr); err == nil {
			follow = parsed
		}
	}

	if !follow {
		// Non-following snapshot request: return empty or tail buffer then close
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}

	b.ServeStream(w, r, "logs", targetID)
}

// ServeStatsStream handles GET /api/v1/stats/stream?target_id={id}.
func (b *SSEBroadcaster) ServeStatsStream(w http.ResponseWriter, r *http.Request) {
	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		targetID = r.URL.Query().Get("service_id")
	}
	if targetID == "" {
		targetID = "*"
	}
	b.ServeStream(w, r, "stats", targetID)
}

// ServeHTTP implements http.Handler for generic SSE routing.
func (b *SSEBroadcaster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
		parts := strings.Split(path, "/")
		if len(parts) > 0 && parts[0] != "" {
			channel = parts[0]
		}
	}

	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		targetID = r.URL.Query().Get("service_id")
	}
	if targetID == "" {
		targetID = "*"
	}

	switch channel {
	case "events":
		b.ServeEventsStream(w, r)
	case "logs":
		b.ServeLogsStream(w, r, targetID)
	case "stats":
		b.ServeStatsStream(w, r)
	default:
		b.ServeStream(w, r, channel, targetID)
	}
}
