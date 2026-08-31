package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"nhooyr.io/websocket"
)

// AgentConfig encapsulates all runtime parameters for the remote worker agent binary.
type AgentConfig struct {
	NodeID                   string        `json:"node_id"`
	NodeName                 string        `json:"node_name"`
	NodeRole                 string        `json:"node_role"`
	ControlPlaneURL          string        `json:"control_plane_url"`
	EnrollmentToken          string        `json:"enrollment_token"`
	TLSCertFile              string        `json:"tls_cert_file"`
	TLSKeyFile               string        `json:"tls_key_file"`
	TLSCAFile                string        `json:"tls_ca_file"`
	InsecureSkipVerify       bool          `json:"insecure_skip_verify"`
	HostMetricInterval       time.Duration `json:"host_metric_interval"`
	ContainerMetricInterval  time.Duration `json:"container_metric_interval"`
	DockerSocket             string        `json:"docker_socket"`
	ProcRoot                 string        `json:"proc_root"`
}

// DefaultAgentConfig returns standard production defaults for agent.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		NodeRole:                "worker",
		HostMetricInterval:      5 * time.Second,
		ContainerMetricInterval: 10 * time.Second,
		DockerSocket:            "/var/run/docker.sock",
		ProcRoot:                "/proc",
	}
}

// BuildTLSConfig constructs a hardened *tls.Config from configured cert paths.
func (c *AgentConfig) BuildTLSConfig() (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: c.InsecureSkipVerify,
		MinVersion:         tls.VersionTLS13,
	}

	if c.TLSCAFile != "" {
		caData, err := os.ReadFile(c.TLSCAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read TLS CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to parse TLS CA PEM")
		}
		tlsConfig.RootCAs = pool
	}

	if c.TLSCertFile != "" && c.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.TLSCertFile, c.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client keypair: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// CommandPayload represents an action request sent from the control plane to a remote worker.
type CommandPayload struct {
	ID      string                 `json:"id"`
	Command string                 `json:"command"` // "ping" | "host.info" | "docker.ps" | "docker.inspect" | "docker.logs" | "docker.exec"
	Args    []string               `json:"args,omitempty"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

// CommandResult represents the outcome of an executed worker command.
type CommandResult struct {
	ID      string      `json:"id"`
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// NodeTokenValidatorFunc verifies whether an incoming enrollment token is authorized for a specific node ID.
type NodeTokenValidatorFunc func(nodeID, token string) bool

// NodeSession tracks an authenticated active worker node connection on the control plane.
type NodeSession struct {
	NodeID                string
	NodeName              string
	NodeRole              string
	RemoteAddr            string
	ConnectedAt           time.Time
	LastHeartbeat         time.Time
	lastHeartbeatUnixNano atomic.Int64
	Conn                  *websocket.Conn
	PendingCommands       map[string]chan *CommandResult
	writeMu               sync.Mutex
}

// UpdateHeartbeat sets the last heartbeat timestamp atomically.
func (ns *NodeSession) UpdateHeartbeat() {
	now := time.Now().UTC()
	ns.LastHeartbeat = now
	ns.lastHeartbeatUnixNano.Store(now.UnixNano())
}

// GetLastHeartbeat returns the last heartbeat timestamp atomically.
func (ns *NodeSession) GetLastHeartbeat() time.Time {
	nano := ns.lastHeartbeatUnixNano.Load()
	if nano > 0 {
		return time.Unix(0, nano).UTC()
	}
	return ns.LastHeartbeat
}

// SafeWrite writes data to the WebSocket connection under a mutex lock.
func (ns *NodeSession) SafeWrite(ctx context.Context, typ websocket.MessageType, p []byte) error {
	ns.writeMu.Lock()
	defer ns.writeMu.Unlock()
	if ns.Conn == nil {
		return fmt.Errorf("connection is nil")
	}
	return ns.Conn.Write(ctx, typ, p)
}

// TelemetryCallback defines the function signature for receiving telemetry frames on the control plane.
type TelemetryCallback func(nodeID string, msg *telemetry.StreamMessage)

// LogCallback defines the function signature for receiving log stream messages on the control plane.
type LogCallback func(nodeID string, msg *telemetry.StreamMessage)

// LogPayload represents a structured container or service log entry sent in a "log" StreamMessage.
type LogPayload struct {
	Source    string `json:"source,omitempty"` // "stdout" | "stderr" | "system"
	Line      string `json:"line"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

