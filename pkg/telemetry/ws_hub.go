package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

// ClientCommandFrame represents an incoming command from a WebSocket client.
type ClientCommandFrame struct {
	Action  string                 `json:"action"`  // "subscribe" | "unsubscribe" | "ping"
	Channel string                 `json:"channel"` // "metrics" | "logs" | "events"
	Target  string                 `json:"target"`  // Target entity identifier or "*"
	Params  map[string]interface{} `json:"params,omitempty"`
}

// ServerDataFrame represents an outgoing multiplexed telemetry data frame.
type ServerDataFrame struct {
	Channel   string      `json:"channel"`
	Target    string      `json:"target"`
	Timestamp int64       `json:"timestamp"`
	Data      interface{} `json:"data"`
	Warning   string      `json:"warning,omitempty"`
}

type subscriptionKey struct {
	channel string
	target  string
}

type clientSession struct {
	id       string
	sendChan chan []byte
	subs     map[subscriptionKey]struct{}
	mu       sync.Mutex
}

type wsHub struct {
	mu      sync.RWMutex
	clients map[string]*clientSession
	// channel -> target -> set of clientIDs
	topics map[string]map[string]map[string]struct{}
}

// NewWebSocketHub creates a new WebSocketHub instance for multiplexing streams to UI/CLI clients.
func NewWebSocketHub() WebSocketHub {
	return &wsHub{
		clients: make(map[string]*clientSession),
		topics:  make(map[string]map[string]map[string]struct{}),
	}
}

// Subscribe registers a client connection to a specific channel and target.
func (h *wsHub) Subscribe(clientID string, channel, targetID string, sendChan chan<- []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()

	session, exists := h.clients[clientID]
	if !exists {
		session = &clientSession{
			id:       clientID,
			sendChan: make(chan []byte, 256),
			subs:     make(map[subscriptionKey]struct{}),
		}
		h.clients[clientID] = session
	}

	key := subscriptionKey{channel: channel, target: targetID}
	session.mu.Lock()
	session.subs[key] = struct{}{}
	session.mu.Unlock()

	if h.topics[channel] == nil {
		h.topics[channel] = make(map[string]map[string]struct{})
	}
	if h.topics[channel][targetID] == nil {
		h.topics[channel][targetID] = make(map[string]struct{})
	}
	h.topics[channel][targetID][clientID] = struct{}{}
}

// Unsubscribe removes a client subscription.
func (h *wsHub) Unsubscribe(clientID string, channel, targetID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	session, exists := h.clients[clientID]
	if exists {
		key := subscriptionKey{channel: channel, target: targetID}
		session.mu.Lock()
		delete(session.subs, key)
		session.mu.Unlock()
	}

	if targets, ok := h.topics[channel]; ok {
		if clients, ok := targets[targetID]; ok {
			delete(clients, clientID)
			if len(clients) == 0 {
				delete(targets, targetID)
			}
		}
		if len(targets) == 0 {
			delete(h.topics, channel)
		}
	}
}

// UnregisterClient cleans up all subscriptions and deletes client session.
func (h *wsHub) UnregisterClient(clientID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	session, exists := h.clients[clientID]
	if !exists {
		return
	}

	session.mu.Lock()
	for key := range session.subs {
		if targets, ok := h.topics[key.channel]; ok {
			if clients, ok := targets[key.target]; ok {
				delete(clients, clientID)
				if len(clients) == 0 {
					delete(targets, key.target)
				}
			}
			if len(targets) == 0 {
				delete(h.topics, key.channel)
			}
		}
	}
	session.mu.Unlock()

	delete(h.clients, clientID)
}

// Broadcast sends a stream message to all matching subscribed clients with backpressure handling.
func (h *wsHub) Broadcast(msg *StreamMessage) {
	if msg == nil {
		return
	}

	frame := ServerDataFrame{
		Channel:   msg.Channel,
		Target:    msg.TargetID,
		Timestamp: msg.Timestamp,
		Data:      msg.Payload,
	}
	if frame.Timestamp <= 0 {
		frame.Timestamp = time.Now().UTC().Unix()
	}

	data, err := json.Marshal(frame)
	if err != nil {
		return
	}

	h.mu.RLock()
	targetClients := make(map[string]*clientSession)

	// Collect matching clients for this channel + target, as well as wildcard "*" target
	if targets, ok := h.topics[msg.Channel]; ok {
		// Specific target match
		if clients, ok := targets[msg.TargetID]; ok {
			for cid := range clients {
				if s, ok := h.clients[cid]; ok {
					targetClients[cid] = s
				}
			}
		}
		// Wildcard target match
		if clients, ok := targets["*"]; ok {
			for cid := range clients {
				if s, ok := h.clients[cid]; ok {
					targetClients[cid] = s
				}
			}
		}
	}
	h.mu.RUnlock()

	isMetric := msg.Type == "metric" || msg.Channel == "metrics"

	for _, session := range targetClients {
		h.dispatchToSession(session, data, isMetric)
	}
}

func (h *wsHub) dispatchToSession(session *clientSession, data []byte, isMetric bool) {
	select {
	case session.sendChan <- data:
		// Message delivered successfully
	default:
		if isMetric {
			// Metric frame: drop silently as newest state supersedes old points
			return
		}

		// Log / event frame: drop oldest message and enqueue dropped warning
		select {
		case <-session.sendChan:
		default:
		}

		warningFrame := ServerDataFrame{
			Channel:   "warning",
			Timestamp: time.Now().UTC().Unix(),
			Warning:   "dropped_frames",
		}
		if warnBytes, err := json.Marshal(warningFrame); err == nil {
			select {
			case session.sendChan <- warnBytes:
			default:
			}
		}

		select {
		case session.sendChan <- data:
		default:
		}
	}
}

// HandleWebSocket upgrades an incoming HTTP request to a multiplexed telemetry WebSocket connection.
func (h *wsHub) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	opts := &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Internal gateway reverse proxy termination
	}
	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusInternalError, "closing connection")

	clientID := r.RemoteAddr
	session := &clientSession{
		id:       clientID,
		sendChan: make(chan []byte, 256),
		subs:     make(map[subscriptionKey]struct{}),
	}

	h.mu.Lock()
	h.clients[clientID] = session
	h.mu.Unlock()

	defer h.UnregisterClient(clientID)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Writer goroutine: sends queued frames with a 30s timeout check
	errChan := make(chan error, 2)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-session.sendChan:
				if !ok {
					return
				}
				writeCtx, writeCancel := context.WithTimeout(ctx, 30*time.Second)
				err := conn.Write(writeCtx, websocket.MessageText, msg)
				writeCancel()
				if err != nil {
					errChan <- err
					return
				}
			}
		}
	}()

	// Reader goroutine: processes incoming subscription frames
	go func() {
		for {
			readCtx, readCancel := context.WithTimeout(ctx, 60*time.Second)
			msgType, data, err := conn.Read(readCtx)
			readCancel()
			if err != nil {
				errChan <- err
				return
			}

			if msgType == websocket.MessageText {
				var cmd ClientCommandFrame
				if err := json.Unmarshal(data, &cmd); err == nil {
					switch cmd.Action {
					case "subscribe":
						h.Subscribe(clientID, cmd.Channel, cmd.Target, session.sendChan)
					case "unsubscribe":
						h.Unsubscribe(clientID, cmd.Channel, cmd.Target)
					case "ping":
						pong := map[string]string{"action": "pong", "timestamp": time.Now().UTC().Format(time.RFC3339)}
						if pongBytes, err := json.Marshal(pong); err == nil {
							_ = conn.Write(ctx, websocket.MessageText, pongBytes)
						}
					}
				}
			}
		}
	}()

	select {
	case <-ctx.Done():
		conn.Close(websocket.StatusNormalClosure, "context cancelled")
	case <-errChan:
		conn.Close(websocket.StatusPolicyViolation, "client stalled or closed")
	}
}
