package agent

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"nhooyr.io/websocket"
)

type defaultAgentClient struct {
	config          AgentConfig
	procReader      telemetry.ProcReader
	dockerCollector telemetry.DockerCollector
	dispatcher      CommandDispatcher

	mu       sync.Mutex
	conn     *websocket.Conn
	writeMu  sync.Mutex
	running  bool
	ctx      context.Context
	cancel   context.CancelFunc
	tlsConf  *tls.Config
	httpCl   *http.Client
	doneChan chan struct{}
}

// NewAgentClient creates a new AgentClient instance with given configuration and subsystems.
func NewAgentClient(cfg AgentConfig, procReader telemetry.ProcReader, dockerCollector telemetry.DockerCollector, dispatcher CommandDispatcher) (telemetry.AgentClient, error) {
	if cfg.NodeID == "" {
		cfg.NodeID = "node_" + fmt.Sprintf("%x", time.Now().UnixNano())
	}
	if cfg.HostMetricInterval <= 0 {
		cfg.HostMetricInterval = 5 * time.Second
	}
	if cfg.ContainerMetricInterval <= 0 {
		cfg.ContainerMetricInterval = 10 * time.Second
	}

	tlsConf, err := cfg.BuildTLSConfig()
	if err != nil {
		return nil, fmt.Errorf("agent: invalid TLS configuration: %w", err)
	}

	if procReader == nil {
		procReader = telemetry.NewProcReaderWithRoot(cfg.ProcRoot)
	}
	if dispatcher == nil {
		dispatcher = NewCommandDispatcher(cfg.DockerSocket)
	}

	return &defaultAgentClient{
		config:          cfg,
		procReader:      procReader,
		dockerCollector: dockerCollector,
		dispatcher:      dispatcher,
		tlsConf:         tlsConf,
		httpCl: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConf,
			},
		},
		doneChan: make(chan struct{}),
	}, nil
}

// Start initiates the outbound connection loop to the Control Plane.
func (c *defaultAgentClient) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil
	}
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.running = true
	c.mu.Unlock()

	go c.connectionLoop()
	return nil
}

func (c *defaultAgentClient) connectionLoop() {
	defer close(c.doneChan)
	attempt := 0

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		err := c.runSession()
		if err != nil && c.ctx.Err() == nil {
			// Calculate jittered exponential backoff
			// T_base = 1.0s, T_max = 30.0s, Jitter = +/-20%
			delaySec := math.Min(30.0, 1.0*math.Pow(2.0, float64(attempt)))
			jitter := (rand.Float64()*0.4 - 0.2) * delaySec
			totalDelay := time.Duration((delaySec + jitter) * float64(time.Second))
			if totalDelay < 500*time.Millisecond {
				totalDelay = 500 * time.Millisecond
			}

			attempt++

			select {
			case <-c.ctx.Done():
				return
			case <-time.After(totalDelay):
			}
		} else {
			attempt = 0
		}
	}
}

func (c *defaultAgentClient) runSession() error {
	headers := http.Header{}
	if c.config.EnrollmentToken != "" {
		headers.Set("Authorization", "Bearer "+c.config.EnrollmentToken)
	}
	headers.Set("X-Node-ID", c.config.NodeID)
	headers.Set("X-Node-Name", c.config.NodeName)
	headers.Set("X-Node-Role", c.config.NodeRole)

	dialOpts := &websocket.DialOptions{
		HTTPClient: c.httpCl,
		HTTPHeader: headers,
	}

	dialCtx, dialCancel := context.WithTimeout(c.ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dialCtx, c.config.ControlPlaneURL, dialOpts)
	dialCancel()
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
		conn.Close(websocket.StatusNormalClosure, "session ended")
	}()

	sessionCtx, sessionCancel := context.WithCancel(c.ctx)
	defer sessionCancel()

	// Launch Host Metrics Poller
	go c.hostMetricsLoop(sessionCtx, conn)

	// Launch Container Stats Streamer
	if c.dockerCollector != nil {
		go c.containerStatsLoop(sessionCtx, conn)
	}

	// Read & Command Dispatcher Loop (with 25s read deadline)
	return c.readLoop(sessionCtx, conn)
}

func (c *defaultAgentClient) readLoop(ctx context.Context, conn *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		readCtx, readCancel := context.WithTimeout(ctx, 25*time.Second)
		msgType, data, err := conn.Read(readCtx)
		readCancel()

		if err != nil {
			return err
		}

		if msgType != websocket.MessageText {
			continue
		}

		var streamMsg telemetry.StreamMessage
		if err := json.Unmarshal(data, &streamMsg); err != nil {
			continue
		}

		switch streamMsg.Type {
		case "command":
			go c.executeAndReplyCommand(ctx, conn, &streamMsg)
		case "ping":
			pongMsg := telemetry.StreamMessage{
				Type:      "ack",
				Channel:   "heartbeat",
				TargetID:  c.config.NodeID,
				Timestamp: time.Now().UTC().Unix(),
				Payload:   map[string]string{"status": "pong"},
			}
			_ = c.writeMessage(ctx, conn, &pongMsg)
		}
	}
}

func (c *defaultAgentClient) executeAndReplyCommand(ctx context.Context, conn *websocket.Conn, streamMsg *telemetry.StreamMessage) {
	payloadBytes, err := json.Marshal(streamMsg.Payload)
	if err != nil {
		return
	}

	var cmdPayload CommandPayload
	if err := json.Unmarshal(payloadBytes, &cmdPayload); err != nil {
		return
	}

	cmdResult, err := c.dispatcher.Dispatch(ctx, &cmdPayload)
	if err != nil {
		cmdResult = &CommandResult{
			ID:      cmdPayload.ID,
			Success: false,
			Error:   err.Error(),
		}
	}

	replyMsg := telemetry.StreamMessage{
		Type:      "command_response",
		Channel:   streamMsg.Channel,
		TargetID:  streamMsg.TargetID,
		Timestamp: time.Now().UTC().Unix(),
		Payload:   cmdResult,
	}

	_ = c.writeMessage(ctx, conn, &replyMsg)
}

func (c *defaultAgentClient) hostMetricsLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(c.config.HostMetricInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			metrics, err := c.procReader.ReadHostMetrics(ctx)
			if err != nil {
				continue
			}
			metrics.NodeID = c.config.NodeID

			msg := telemetry.StreamMessage{
				Type:      "metric",
				Channel:   "node",
				TargetID:  c.config.NodeID,
				Timestamp: metrics.Timestamp.Unix(),
				Payload:   metrics,
			}

			if err := c.writeMessage(ctx, conn, &msg); err != nil {
				return
			}
		}
	}
}

func (c *defaultAgentClient) containerStatsLoop(ctx context.Context, conn *websocket.Conn) {
	statsChan := make(chan telemetry.ContainerStats, 128)
	if err := c.dockerCollector.StreamContainerStats(ctx, statsChan); err != nil {
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case stat, ok := <-statsChan:
			if !ok {
				return
			}
			stat.NodeID = c.config.NodeID

			msg := telemetry.StreamMessage{
				Type:      "metric",
				Channel:   "container",
				TargetID:  stat.ContainerID,
				Timestamp: stat.Timestamp.Unix(),
				Payload:   stat,
			}

			if err := c.writeMessage(ctx, conn, &msg); err != nil {
				return
			}
		}
	}
}

func (c *defaultAgentClient) writeMessage(ctx context.Context, conn *websocket.Conn, msg *telemetry.StreamMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
	defer writeCancel()

	return conn.Write(writeCtx, websocket.MessageText, data)
}

// SendTelemetry streams a batch or custom metric/log stream frame over the active connection.
func (c *defaultAgentClient) SendTelemetry(ctx context.Context, msg *telemetry.StreamMessage) error {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("agent: connection offline")
	}

	return c.writeMessage(ctx, conn, msg)
}

// Close disconnects the remote agent and terminates running collector loops.
func (c *defaultAgentClient) Close() error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	c.running = false
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		c.conn.Close(websocket.StatusNormalClosure, "agent stopping")
	}
	c.mu.Unlock()

	if c.dockerCollector != nil {
		_ = c.dockerCollector.Stop()
	}

	<-c.doneChan
	return nil
}
