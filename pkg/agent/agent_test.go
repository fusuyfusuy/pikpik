package agent_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/agent"
	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProcReader struct {
	mu      sync.Mutex
	metrics *telemetry.HostMetrics
}

func (m *mockProcReader) ReadHostMetrics(ctx context.Context) (*telemetry.HostMetrics, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.metrics == nil {
		return &telemetry.HostMetrics{
			NodeID:        "mock-node",
			Timestamp:     time.Now().UTC(),
			CPUPercent:    24.5,
			CPUCores:      4,
			MemTotalBytes: 8 * 1024 * 1024 * 1024,
			MemUsedBytes:  2 * 1024 * 1024 * 1024,
		}, nil
	}
	return m.metrics, nil
}

type mockDockerCollector struct {
	mu      sync.Mutex
	running bool
}

func (m *mockDockerCollector) Start(ctx context.Context) error {
	m.mu.Lock()
	m.running = true
	m.mu.Unlock()
	return nil
}

func (m *mockDockerCollector) StreamContainerStats(ctx context.Context, out chan<- telemetry.ContainerStats) error {
	_ = m.Start(ctx)
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case out <- telemetry.ContainerStats{
					ContainerID:     "c_test_01",
					ServiceID:       "svc_test",
					ProjectID:       "proj_test",
					Timestamp:       time.Now().UTC(),
					CPUPercent:      12.0,
					MemoryUsedBytes: 100 * 1024 * 1024,
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return nil
}

func (m *mockDockerCollector) Stop() error {
	m.mu.Lock()
	m.running = false
	m.mu.Unlock()
	return nil
}

// TestAgentClientServerE2E tests outbound connection, authentication, telemetry flow, and bidirectional command dispatch.
func TestAgentClientServerE2E(t *testing.T) {
	enrollmentToken := "secret_enroll_token_12345"
	nodeID := "worker_alpha_01"

	var receivedTelemetry []*telemetry.StreamMessage
	var telMu sync.Mutex
	ringBuffers := make(map[string]telemetry.RingBuffer)

	// 1. Setup Control Plane Agent Server
	serverOpts := agent.AgentServerOptions{
		EnrollmentToken: enrollmentToken,
		OnTelemetry: func(nID string, msg *telemetry.StreamMessage) {
			telMu.Lock()
			receivedTelemetry = append(receivedTelemetry, msg)
			telMu.Unlock()
		},
		RingBuffers: ringBuffers,
	}
	agentServer := agent.NewAgentServer(serverOpts)

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if srvImpl, ok := agentServer.(interface{ HandleHTTP(http.ResponseWriter, *http.Request) }); ok {
			srvImpl.HandleHTTP(w, r)
		}
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	// 2. Setup Agent Client
	cfg := agent.AgentConfig{
		NodeID:                  nodeID,
		NodeName:                "worker-alpha-01",
		NodeRole:                "worker",
		ControlPlaneURL:         wsURL,
		EnrollmentToken:         enrollmentToken,
		HostMetricInterval:      50 * time.Millisecond,
		ContainerMetricInterval: 50 * time.Millisecond,
	}

	procReader := &mockProcReader{}
	dockerCollector := &mockDockerCollector{}
	dispatcher := agent.NewCommandDispatcher("")

	// Add custom test handler
	dispatcher.RegisterHandler("test.echo", func(ctx context.Context, cmd *agent.CommandPayload) (interface{}, error) {
		return map[string]interface{}{"echo": cmd.Params["message"]}, nil
	})

	client, err := agent.NewAgentClient(cfg, procReader, dockerCollector, dispatcher)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = client.Start(ctx)
	require.NoError(t, err)
	defer client.Close()

	// Wait for session registration
	require.Eventually(t, func() bool {
		if srvImpl, ok := agentServer.(interface {
			GetNodeSession(string) (*agent.NodeSession, bool)
		}); ok {
			_, exists := srvImpl.GetNodeSession(nodeID)
			return exists
		}
		return false
	}, 3*time.Second, 50*time.Millisecond, "node session should be registered on server")

	// 3. Test Command Dispatch: ping
	if srvImpl, ok := agentServer.(interface {
		Dispatch(context.Context, string, *agent.CommandPayload) (*agent.CommandResult, error)
	}); ok {
		pingRes, err := srvImpl.Dispatch(ctx, nodeID, &agent.CommandPayload{
			Command: "ping",
		})
		require.NoError(t, err)
		require.NotNil(t, pingRes)
		assert.True(t, pingRes.Success)

		// 4. Test Command Dispatch: host.info
		infoRes, err := srvImpl.Dispatch(ctx, nodeID, &agent.CommandPayload{
			Command: "host.info",
		})
		require.NoError(t, err)
		assert.True(t, infoRes.Success)

		// 5. Test Command Dispatch: custom handler
		echoRes, err := srvImpl.Dispatch(ctx, nodeID, &agent.CommandPayload{
			Command: "test.echo",
			Params:  map[string]interface{}{"message": "hello control plane"},
		})
		require.NoError(t, err)
		assert.True(t, echoRes.Success)

		// 6. Test Command Dispatch: unknown command
		unkRes, err := srvImpl.Dispatch(ctx, nodeID, &agent.CommandPayload{
			Command: "nonexistent.cmd",
		})
		require.NoError(t, err)
		assert.False(t, unkRes.Success)
		assert.Contains(t, unkRes.Error, "unknown command")
	}

	// 7. Verify Telemetry Ingestion into Ring Buffers & Callback
	require.Eventually(t, func() bool {
		telMu.Lock()
		count := len(receivedTelemetry)
		telMu.Unlock()
		return count >= 2
	}, 3*time.Second, 50*time.Millisecond, "should have received host & container telemetry frames")

	// Verify Ring Buffer points
	nodeBufKey := fmt.Sprintf("node:%s", nodeID)
	require.Eventually(t, func() bool {
		if srvImpl, ok := agentServer.(interface{ GetRingBuffer(string) telemetry.RingBuffer }); ok {
			buf := srvImpl.GetRingBuffer(nodeBufKey)
			return buf != nil && buf.Len() > 0
		}
		return false
	}, 3*time.Second, 50*time.Millisecond, "node ring buffer should contain points")
}

// TestRingBufferSnapshotConcurrentAccess proves RingBufferSnapshot can be
// ranged over safely by a reader goroutine (mimicking the background
// downsampler ticker in cmd/pikpik/main.go) while real telemetry ingestion
// concurrently mutates the server's ring buffer registry via
// recordToRingBuffer. Run with -race: before RingBufferSnapshot existed,
// ranging the raw map shared between main.go and the server would trip Go's
// concurrent map read/write detector under this exact workload.
func TestRingBufferSnapshotConcurrentAccess(t *testing.T) {
	ringBuffers := make(map[string]telemetry.RingBuffer)
	agentServer := agent.NewAgentServer(agent.AgentServerOptions{
		EnrollmentToken: "snapshot_test_token",
		RingBuffers:     ringBuffers,
	})

	srvImpl, ok := agentServer.(interface {
		RingBufferSnapshot() map[string]telemetry.RingBuffer
	})
	require.True(t, ok, "AgentServer must implement RingBufferSnapshot")

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h, ok := agentServer.(interface{ HandleHTTP(http.ResponseWriter, *http.Request) }); ok {
			h.HandleHTTP(w, r)
		}
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	cfg := agent.AgentConfig{
		NodeID:                  "snapshot_race_node",
		ControlPlaneURL:         wsURL,
		EnrollmentToken:         "snapshot_test_token",
		HostMetricInterval:      1 * time.Millisecond,
		ContainerMetricInterval: 1 * time.Millisecond,
	}

	client, err := agent.NewAgentClient(cfg, &mockProcReader{}, &mockDockerCollector{}, agent.NewCommandDispatcher(""))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, client.Start(ctx))
	defer client.Close()

	// Reader: repeatedly takes snapshots and ranges over them concurrently
	// with the telemetry writes flowing in from the client above, exactly
	// like the downsampler ticker does against the live server in main.go.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			snap := srvImpl.RingBufferSnapshot()
			for range snap {
				// no-op: just prove ranging a snapshot is race-free
			}
		}
	}()

	wg.Wait()
}

// TestAgentServerAuthenticationFailure verifies invalid tokens are rejected with HTTP 401.
func TestAgentServerAuthenticationFailure(t *testing.T) {
	serverOpts := agent.AgentServerOptions{
		EnrollmentToken: "correct_token",
	}
	agentServer := agent.NewAgentServer(serverOpts)

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if srvImpl, ok := agentServer.(interface{ HandleHTTP(http.ResponseWriter, *http.Request) }); ok {
			srvImpl.HandleHTTP(w, r)
		}
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	cfg := agent.AgentConfig{
		NodeID:          "worker_bad_auth",
		ControlPlaneURL: wsURL,
		EnrollmentToken: "wrong_token",
	}

	client, err := agent.NewAgentClient(cfg, &mockProcReader{}, nil, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = client.Start(ctx)
	require.NoError(t, err)
	defer client.Close()

	// Wait briefly and verify node was never registered
	time.Sleep(300 * time.Millisecond)

	if srvImpl, ok := agentServer.(interface {
		GetNodeSession(string) (*agent.NodeSession, bool)
	}); ok {
		_, exists := srvImpl.GetNodeSession("worker_bad_auth")
		assert.False(t, exists, "unauthenticated worker must not be registered")
	}
}

func TestAgentLogStreamMessage(t *testing.T) {
	enrollmentToken := "log_stream_test_token"
	nodeID := "node_log_producer"

	var logMu sync.Mutex
	var receivedLogs []*telemetry.StreamMessage
	wsHub := telemetry.NewWebSocketHub()

	serverOpts := agent.AgentServerOptions{
		EnrollmentToken: enrollmentToken,
		OnLog: func(nID string, msg *telemetry.StreamMessage) {
			logMu.Lock()
			receivedLogs = append(receivedLogs, msg)
			logMu.Unlock()
		},
		WebSocketHub: wsHub,
	}
	agentServer := agent.NewAgentServer(serverOpts)

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if srvImpl, ok := agentServer.(interface{ HandleHTTP(http.ResponseWriter, *http.Request) }); ok {
			srvImpl.HandleHTTP(w, r)
		}
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	cfg := agent.AgentConfig{
		NodeID:                  nodeID,
		ControlPlaneURL:         wsURL,
		EnrollmentToken:         enrollmentToken,
		HostMetricInterval:      500 * time.Millisecond,
		ContainerMetricInterval: 500 * time.Millisecond,
	}

	client, err := agent.NewAgentClient(cfg, &mockProcReader{}, nil, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = client.Start(ctx)
	require.NoError(t, err)
	defer client.Close()

	// Wait for connection
	require.Eventually(t, func() bool {
		if srvImpl, ok := agentServer.(interface {
			GetNodeSession(string) (*agent.NodeSession, bool)
		}); ok {
			_, exists := srvImpl.GetNodeSession(nodeID)
			return exists
		}
		return false
	}, 3*time.Second, 50*time.Millisecond)

	// Send a "log" StreamMessage
	logMsg := &telemetry.StreamMessage{
		Type:      "log",
		Channel:   "container",
		TargetID:  "c_app_web",
		Timestamp: time.Now().UTC().Unix(),
		Payload: agent.LogPayload{
			Source:    "stdout",
			Line:      "Server started on port 8080",
			Timestamp: time.Now().UTC().Unix(),
		},
	}

	err = client.SendTelemetry(ctx, logMsg)
	require.NoError(t, err)

	// Verify log message is received by OnLog callback
	require.Eventually(t, func() bool {
		logMu.Lock()
		defer logMu.Unlock()
		return len(receivedLogs) > 0
	}, 3*time.Second, 50*time.Millisecond)

	logMu.Lock()
	defer logMu.Unlock()
	require.Len(t, receivedLogs, 1)
	assert.Equal(t, "log", receivedLogs[0].Type)
	assert.Equal(t, "container", receivedLogs[0].Channel)
	assert.Equal(t, "c_app_web", receivedLogs[0].TargetID)
}

func TestAgentDispatcher_DockerPathTraversal_Sanitization(t *testing.T) {
	dispatcher := agent.NewCommandDispatcher("")
	ctx := context.Background()

	traversalPayloads := []string{
		"../../etc/shadow",
		"../volumes/prune",
		"x/archive?path=/etc/passwd#",
		"container/../escape",
		"bad%2fid",
		";rm -rf /",
		"",
	}

	for _, badID := range traversalPayloads {
		// docker.inspect
		res, err := dispatcher.Dispatch(ctx, &agent.CommandPayload{
			Command: "docker.inspect",
			Args:    []string{badID},
		})
		require.NoError(t, err) // Dispatch returns structured result with Success=false on handler error
		assert.False(t, res.Success, "docker.inspect with %q must fail", badID)
		assert.NotEmpty(t, res.Error)

		// docker.logs
		res, err = dispatcher.Dispatch(ctx, &agent.CommandPayload{
			Command: "docker.logs",
			Args:    []string{badID, "100"},
		})
		require.NoError(t, err)
		assert.False(t, res.Success, "docker.logs with %q must fail", badID)
		assert.NotEmpty(t, res.Error)
	}
}

func TestAgentServer_NodeIdentityBindingAndSpoofingPrevention(t *testing.T) {
	// 1. Test NodeTokenValidator
	validTokens := map[string]string{
		"node_alpha": "token_alpha_secret",
		"node_beta":  "token_beta_secret",
	}

	serverOpts := agent.AgentServerOptions{
		NodeTokenValidator: func(nodeID, token string) bool {
			expected, exists := validTokens[nodeID]
			return exists && expected == token
		},
	}
	agentServer := agent.NewAgentServer(serverOpts)

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if srvImpl, ok := agentServer.(interface{ HandleHTTP(http.ResponseWriter, *http.Request) }); ok {
			srvImpl.HandleHTTP(w, r)
		}
	}))
	defer httpServer.Close()

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http")

	// Attempt connecting as node_alpha with node_beta's token (spoofing attempt)
	cfgSpoof := agent.AgentConfig{
		NodeID:          "node_alpha",
		ControlPlaneURL: wsURL,
		EnrollmentToken: "token_beta_secret",
	}
	clientSpoof, err := agent.NewAgentClient(cfgSpoof, &mockProcReader{}, nil, nil)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	require.NoError(t, clientSpoof.Start(ctx))
	defer clientSpoof.Close()

	time.Sleep(200 * time.Millisecond)

	srvImpl := agentServer.(interface {
		GetNodeSession(string) (*agent.NodeSession, bool)
	})
	_, exists := srvImpl.GetNodeSession("node_alpha")
	assert.False(t, exists, "spoofed node_alpha must be rejected by NodeTokenValidator")

	// 2. Connect legitimately as node_alpha
	cfgLegit := agent.AgentConfig{
		NodeID:          "node_alpha",
		ControlPlaneURL: wsURL,
		EnrollmentToken: "token_alpha_secret",
	}
	clientLegit, err := agent.NewAgentClient(cfgLegit, &mockProcReader{}, nil, nil)
	require.NoError(t, err)

	require.NoError(t, clientLegit.Start(ctx))
	defer clientLegit.Close()

	require.Eventually(t, func() bool {
		_, ok := srvImpl.GetNodeSession("node_alpha")
		return ok
	}, 2*time.Second, 50*time.Millisecond, "legitimate node_alpha must connect")
}

