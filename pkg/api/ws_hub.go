package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	Subprotocols: []string{"pikpik-auth"},
}

type WSClient struct {
	hub      *WebSocketHub
	conn     *websocket.Conn
	send     chan []byte
	channels map[string]bool // "channel:target_id"
	mu       sync.RWMutex
	userID   string
}

type WebSocketHub struct {
	clients    map[*WSClient]bool
	broadcast  chan WSMessage
	register   chan *WSClient
	unregister chan *WSClient
	mu         sync.RWMutex
}

// NewWebSocketHub creates a new WebSocketHub instance for real-time multiplexing.
func NewWebSocketHub() *WebSocketHub {
	return &WebSocketHub{
		clients:    make(map[*WSClient]bool),
		broadcast:  make(chan WSMessage, 1024),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
	}
}

func (h *WebSocketHub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				_ = client.conn.Close()
				delete(h.clients, client)
			}
			h.mu.Unlock()
			return
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				_ = client.conn.Close()
			}
			h.mu.Unlock()
		case msg := <-h.broadcast:
			h.broadcastMessage(msg)
		}
	}
}

func (h *WebSocketHub) Broadcast(msg WSMessage) {
	if msg.Time.IsZero() {
		msg.Time = time.Now().UTC()
	}
	select {
	case h.broadcast <- msg:
	default:
		// Drop message if broadcast channel is congested
	}
}

func (h *WebSocketHub) broadcastMessage(msg WSMessage) {
	topicKey := msg.Channel + ":" + msg.TargetID
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		client.mu.RLock()
		subscribed := client.channels[topicKey] || client.channels[msg.Channel+":*"] || client.channels["*:*"]
		client.mu.RUnlock()

		if subscribed {
			select {
			case client.send <- payload:
			default:
				// Slow consumer, skip frame
			}
		}
	}
}

// ServeWebSocket handles an incoming WebSocket upgrade and routes subscription messages.
func (h *WebSocketHub) ServeWebSocket(w http.ResponseWriter, r *http.Request, defaultChannel string) {
	responseHeader := make(http.Header)
	// Echo back subprotocol if requested
	for _, p := range websocket.Subprotocols(r) {
		if strings.HasPrefix(p, "pikpik-auth") {
			responseHeader.Set("Sec-WebSocket-Protocol", "pikpik-auth")
			break
		}
	}

	conn, err := upgrader.Upgrade(w, r, responseHeader)
	if err != nil {
		return
	}

	client := &WSClient{
		hub:      h,
		conn:     conn,
		send:     make(chan []byte, 256),
		channels: make(map[string]bool),
	}

	// Auto-subscribe to default channel if specified via path or query (e.g. /ws/logs?target_id=app_123)
	targetID := r.URL.Query().Get("target_id")
	if targetID == "" {
		targetID = "*"
	}
	if defaultChannel != "" && defaultChannel != "multiplex" {
		client.channels[defaultChannel+":"+targetID] = true
	}

	h.register <- client

	// Writer goroutine
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer func() {
			ticker.Stop()
			_ = conn.Close()
		}()

		for {
			select {
			case msg, ok := <-client.send:
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if !ok {
					_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
					return
				}
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	// Reader goroutine
	defer func() {
		h.unregister <- client
	}()

	conn.SetReadLimit(64 * 1024)
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var action ClientAction
		if err := json.Unmarshal(payload, &action); err == nil {
			switch strings.ToLower(action.Action) {
			case "subscribe":
				if action.TargetID == "" {
					action.TargetID = "*"
				}
				client.mu.Lock()
				client.channels[action.Channel+":"+action.TargetID] = true
				client.mu.Unlock()
			case "unsubscribe":
				if action.TargetID == "" {
					action.TargetID = "*"
				}
				client.mu.Lock()
				delete(client.channels, action.Channel+":"+action.TargetID)
				client.mu.Unlock()
			case "ping":
				pong := WSMessage{
					Channel:  "system",
					TargetID: "client",
					Event:    "pong",
					Data:     map[string]string{"time": time.Now().UTC().Format(time.RFC3339)},
					Time:     time.Now().UTC(),
				}
				if pongData, err := json.Marshal(pong); err == nil {
					select {
					case client.send <- pongData:
					default:
					}
				}
			}
		}
	}
}
