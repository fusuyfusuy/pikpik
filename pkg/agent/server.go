package agent

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/fusuycorp/pikpik/pkg/store"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"nhooyr.io/websocket"
)

// TokenValidatorFunc verifies whether an incoming enrollment token is authorized.
type TokenValidatorFunc func(token string) bool

// AgentServerOptions configures the Control Plane agent server handler.
type AgentServerOptions struct {
	EnrollmentToken    string
	TokenValidator     TokenValidatorFunc
	NodeTokenValidator NodeTokenValidatorFunc
	OnTelemetry        TelemetryCallback
	OnLog              LogCallback
	RingBuffers        map[string]telemetry.RingBuffer
	WebSocketHub       telemetry.WebSocketHub
	MachineStore       store.MachineStore
}

type defaultAgentServer struct {
	mu           sync.RWMutex
	sessions     map[string]*NodeSession
	tokenVal     TokenValidatorFunc
	nodeTokenVal NodeTokenValidatorFunc
	onTelemetry  TelemetryCallback
	onLog        LogCallback
	ringBuffers  map[string]telemetry.RingBuffer
	ringMu       sync.RWMutex
	webSocketHub telemetry.WebSocketHub
	machineStore store.MachineStore
}

// NewAgentServer creates a new AgentServer for the Control Plane.
func NewAgentServer(opts AgentServerOptions) telemetry.AgentServer {
	val := opts.TokenValidator
	if val == nil && opts.EnrollmentToken != "" {
		expected := opts.EnrollmentToken
		val = func(t string) bool {
			return subtle.ConstantTimeCompare([]byte(t), []byte(expected)) == 1
		}
	}

	rb := opts.RingBuffers
	if rb == nil {
		rb = make(map[string]telemetry.RingBuffer)
	}

	return &defaultAgentServer{
		sessions:     make(map[string]*NodeSession),
		tokenVal:     val,
		nodeTokenVal: opts.NodeTokenValidator,
		onTelemetry:  opts.OnTelemetry,
		onLog:        opts.OnLog,
		ringBuffers:  rb,
		webSocketHub: opts.WebSocketHub,
		machineStore: opts.MachineStore,
	}
}

// GetRingBuffer returns a ring buffer by key in a thread-safe manner.
func (s *defaultAgentServer) GetRingBuffer(key string) telemetry.RingBuffer {
	s.ringMu.RLock()
	defer s.ringMu.RUnlock()
	return s.ringBuffers[key]
}

// RingBufferSnapshot returns a thread-safe copy of the ring buffer registry.
// Callers must use this instead of accessing the map passed into
// AgentServerOptions directly, since that map is mutated concurrently
// (under ringMu) by incoming telemetry writes.
func (s *defaultAgentServer) RingBufferSnapshot() map[string]telemetry.RingBuffer {
	s.ringMu.RLock()
	defer s.ringMu.RUnlock()
	snapshot := make(map[string]telemetry.RingBuffer, len(s.ringBuffers))
	for key, buf := range s.ringBuffers {
		snapshot[key] = buf
	}
	return snapshot
}

// RegisterNode registers a newly authenticated worker node session.
func (s *defaultAgentServer) RegisterNode(nodeID string, session interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if sess, ok := session.(*NodeSession); ok {
		if oldSess, exists := s.sessions[nodeID]; exists && oldSess != nil && oldSess != sess {
			if oldSess.Conn != nil {
				_ = oldSess.Conn.Close(websocket.StatusPolicyViolation, "displaced by new session")
			}
			for id, ch := range oldSess.PendingCommands {
				select {
				case ch <- &CommandResult{
					ID:      id,
					Success: false,
					Error:   "node re-enrolled with new session",
				}:
				default:
				}
			}
		}
		s.sessions[nodeID] = sess
	}
}

// UnregisterNode removes disconnected worker node sessions.
func (s *defaultAgentServer) UnregisterNode(nodeID string) {
	s.mu.Lock()
	sess, ok := s.sessions[nodeID]
	delete(s.sessions, nodeID)
	s.mu.Unlock()

	if s.machineStore != nil {
		_ = s.machineStore.UpdateStatus(context.Background(), nodeID, "offline", time.Now().UTC())
	}

	if ok && sess != nil {
		s.mu.Lock()
		for id, ch := range sess.PendingCommands {
			select {
			case ch <- &CommandResult{
				ID:      id,
				Success: false,
				Error:   "agent disconnected",
			}:
			default:
			}
		}
		s.mu.Unlock()
	}
}

// GetNodeSession retrieves an active session for nodeID.
func (s *defaultAgentServer) GetNodeSession(nodeID string) (*NodeSession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[nodeID]
	return sess, ok
}

// ListNodes returns all currently connected worker node sessions.
func (s *defaultAgentServer) ListNodes() []*NodeSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]*NodeSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		list = append(list, sess)
	}
	return list
}

// ServeHTTP implements http.Handler for mounting on standard HTTP routers.
func (s *defaultAgentServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.HandleHTTP(w, r)
}

// HandleHTTP handles incoming WebSocket upgrade requests from remote agent clients.
func (s *defaultAgentServer) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	var token string
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		token = authHeader[7:]
	} else {
		token = r.URL.Query().Get("token")
	}

	if s.tokenVal != nil && !s.tokenVal(token) {
		http.Error(w, "Unauthorized agent token", http.StatusUnauthorized)
		return
	}

	nodeID := r.Header.Get("X-Node-ID")
	if nodeID == "" {
		nodeID = r.URL.Query().Get("node_id")
	}

	// If mTLS client certificates are present, enforce CommonName identity binding
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		certCN := r.TLS.PeerCertificates[0].Subject.CommonName
		if certCN != "" {
			if nodeID != "" && nodeID != certCN {
				http.Error(w, fmt.Sprintf("Forbidden: claimed node ID %q does not match client certificate CommonName %q", nodeID, certCN), http.StatusForbidden)
				return
			}
			nodeID = certCN
		}
	}

	if nodeID == "" {
		nodeID = "node_" + r.RemoteAddr
	}

	// Validate node-specific token binding if configured
	if s.nodeTokenVal != nil && !s.nodeTokenVal(nodeID, token) {
		http.Error(w, fmt.Sprintf("Unauthorized: node %q not authorized with provided token", nodeID), http.StatusUnauthorized)
		return
	}

	nodeName := r.Header.Get("X-Node-Name")
	if nodeName == "" {
		nodeName = r.URL.Query().Get("node_name")
	}

	nodeRole := r.Header.Get("X-Node-Role")
	if nodeRole == "" {
		nodeRole = r.URL.Query().Get("node_role")
	}
	if nodeRole == "" {
		nodeRole = "worker"
	}

	osKernel := r.Header.Get("X-OS-Kernel")
	cpuArch := r.Header.Get("X-CPU-Arch")
	agentVer := r.Header.Get("X-Agent-Version")
	dockerVer := r.Header.Get("X-Docker-Version")
	publicIP := r.Header.Get("X-Public-IP")
	privateIP := r.Header.Get("X-Private-IP")
	if publicIP == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err == nil {
			publicIP = host
		} else {
			publicIP = r.RemoteAddr
		}
	}
	if nodeName == "" {
		nodeName = nodeID
	}

	opts := &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	}

	conn, err := websocket.Accept(w, r, opts)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusInternalError, "session closed")

	session := &NodeSession{
		NodeID:          nodeID,
		NodeName:        nodeName,
		NodeRole:        nodeRole,
		RemoteAddr:      r.RemoteAddr,
		ConnectedAt:     time.Now().UTC(),
		Conn:            conn,
		PendingCommands: make(map[string]chan *CommandResult),
	}
	session.UpdateHeartbeat()

	if s.machineStore != nil {
		now := time.Now().UTC()
		_ = s.machineStore.Upsert(r.Context(), &store.ManagedMachine{
			ID:            nodeID,
			Hostname:      nodeName,
			Role:          nodeRole,
			PublicIP:      publicIP,
			PrivateIP:     privateIP,
			OSKernel:      osKernel,
			CPUArch:       cpuArch,
			DockerVersion: dockerVer,
			AgentVersion:  agentVer,
			Status:        "online",
			LastSeen:      &now,
		})
	}

	s.RegisterNode(nodeID, session)
	defer s.UnregisterNode(nodeID)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Ping goroutine (every 10s)
	go s.heartbeatLoop(ctx, session)

	// Ingestion and command response read loop
	s.readLoop(ctx, session)
}

func (s *defaultAgentServer) heartbeatLoop(ctx context.Context, session *NodeSession) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check dead connection deadline (25s)
			if time.Since(session.GetLastHeartbeat()) > 25*time.Second {
				session.Conn.Close(websocket.StatusPolicyViolation, "heartbeat timeout")
				return
			}

			pingMsg := telemetry.StreamMessage{
				Type:      "ping",
				Channel:   "heartbeat",
				TargetID:  session.NodeID,
				Timestamp: time.Now().UTC().Unix(),
			}

			data, err := json.Marshal(pingMsg)
			if err != nil {
				continue
			}

			writeCtx, writeCancel := context.WithTimeout(ctx, 5*time.Second)
			err = session.SafeWrite(writeCtx, websocket.MessageText, data)
			writeCancel()
			if err != nil {
				return
			}
		}
	}
}

func (s *defaultAgentServer) readLoop(ctx context.Context, session *NodeSession) {
	for {
		readCtx, readCancel := context.WithTimeout(ctx, 30*time.Second)
		msgType, data, err := session.Conn.Read(readCtx)
		readCancel()

		if err != nil {
			return
		}

		session.UpdateHeartbeat()

		if msgType != websocket.MessageText {
			continue
		}

		var msg telemetry.StreamMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "ack":
			// Heartbeat pong received

		case "metric":
			if s.machineStore != nil {
				now := time.Now().UTC()
				_ = s.machineStore.UpdateStatus(ctx, session.NodeID, "online", now)
			}
			// Forward to callback
			if s.onTelemetry != nil {
				s.onTelemetry(session.NodeID, &msg)
			}
			// Forward to WebSocket Hub for UI clients
			if s.webSocketHub != nil {
				s.webSocketHub.Broadcast(&msg)
			}
			// Store in Ring Buffer
			s.recordToRingBuffer(&msg)

		case "log":
			// Forward to log callback if configured, otherwise fallback to onTelemetry
			if s.onLog != nil {
				s.onLog(session.NodeID, &msg)
			} else if s.onTelemetry != nil {
				s.onTelemetry(session.NodeID, &msg)
			}
			// Forward to WebSocket Hub for UI clients
			if s.webSocketHub != nil {
				s.webSocketHub.Broadcast(&msg)
			}

		case "command_response":
			s.handleCommandResponse(session, &msg)
		}
	}
}

func (s *defaultAgentServer) recordToRingBuffer(msg *telemetry.StreamMessage) {
	var pt telemetry.MetricPoint
	var key string

	payloadBytes, err := json.Marshal(msg.Payload)
	if err != nil {
		return
	}

	if msg.Channel == "node" {
		var hm telemetry.HostMetrics
		if err := json.Unmarshal(payloadBytes, &hm); err == nil {
			pt = hm.ToMetricPoint()
			key = "node:" + msg.TargetID
		}
	} else if msg.Channel == "container" {
		var cs telemetry.ContainerStats
		if err := json.Unmarshal(payloadBytes, &cs); err == nil {
			pt = cs.ToMetricPoint()
			key = "container:" + msg.TargetID
		}
	}

	if key != "" {
		s.ringMu.Lock()
		buf, exists := s.ringBuffers[key]
		if !exists {
			buf = telemetry.NewRingBuffer(telemetry.DefaultRingBufferCapacity)
			s.ringBuffers[key] = buf
		}
		s.ringMu.Unlock()

		buf.Push(pt)
	}
}

func (s *defaultAgentServer) handleCommandResponse(session *NodeSession, msg *telemetry.StreamMessage) {
	payloadBytes, err := json.Marshal(msg.Payload)
	if err != nil {
		return
	}

	var res CommandResult
	if err := json.Unmarshal(payloadBytes, &res); err != nil {
		return
	}

	s.mu.RLock()
	ch, exists := session.PendingCommands[res.ID]
	s.mu.RUnlock()

	if exists && ch != nil {
		select {
		case ch <- &res:
		default:
		}
	}
}

// HandleAgentConnection upgrades a raw net.Conn (or test net.Conn) to a WebSocket session.
func (s *defaultAgentServer) HandleAgentConnection(ctx context.Context, conn net.Conn, nodeID string) error {
	session := &NodeSession{
		NodeID:          nodeID,
		ConnectedAt:     time.Now().UTC(),
		LastHeartbeat:   time.Now().UTC(),
		PendingCommands: make(map[string]chan *CommandResult),
	}
	s.RegisterNode(nodeID, session)
	return nil
}

// DispatchCommand routes a command to a specific worker node and awaits response.
func (s *defaultAgentServer) DispatchCommand(ctx context.Context, nodeID string, cmd *telemetry.StreamMessage) (*telemetry.StreamMessage, error) {
	s.mu.RLock()
	session, exists := s.sessions[nodeID]
	s.mu.RUnlock()

	if !exists || session == nil || session.Conn == nil {
		return nil, fmt.Errorf("agent: node %s is offline", nodeID)
	}

	var cmdPayload CommandPayload
	payloadBytes, err := json.Marshal(cmd.Payload)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(payloadBytes, &cmdPayload); err != nil {
		return nil, err
	}

	if cmdPayload.ID == "" {
		cmdPayload.ID = fmt.Sprintf("cmd_%d", time.Now().UnixNano())
	}
	cmd.Payload = cmdPayload

	resChan := make(chan *CommandResult, 1)

	s.mu.Lock()
	session.PendingCommands[cmdPayload.ID] = resChan
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(session.PendingCommands, cmdPayload.ID)
		s.mu.Unlock()
	}()

	cmdData, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
	err = session.SafeWrite(writeCtx, websocket.MessageText, cmdData)
	writeCancel()
	if err != nil {
		return nil, fmt.Errorf("failed to send command to node %s: %w", nodeID, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-resChan:
		return &telemetry.StreamMessage{
			Type:      "command_response",
			Channel:   cmd.Channel,
			TargetID:  nodeID,
			Timestamp: time.Now().UTC().Unix(),
			Payload:   res,
		}, nil
	}
}

// Dispatch is a convenience helper to execute a typed command on a worker node.
func (s *defaultAgentServer) Dispatch(ctx context.Context, nodeID string, payload *CommandPayload) (*CommandResult, error) {
	msg := &telemetry.StreamMessage{
		Type:      "command",
		Channel:   "node",
		TargetID:  nodeID,
		Timestamp: time.Now().UTC().Unix(),
		Payload:   payload,
	}

	resp, err := s.DispatchCommand(ctx, nodeID, msg)
	if err != nil {
		return nil, err
	}

	payloadBytes, err := json.Marshal(resp.Payload)
	if err != nil {
		return nil, err
	}

	var res CommandResult
	if err := json.Unmarshal(payloadBytes, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
