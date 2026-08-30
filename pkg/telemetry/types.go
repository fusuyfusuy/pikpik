package telemetry

import (
	"context"
	"math"
	"net"
	"time"
)

// ============================================================================
// 1. DOMAIN MODELS & TELEMETRY STRUCTS
// ============================================================================

// HostMetrics captures real-time operating system performance parsed from /proc.
type HostMetrics struct {
	NodeID        string    `json:"node_id"`
	Timestamp     time.Time `json:"timestamp"`
	UptimeSeconds uint64    `json:"uptime_seconds"`

	// CPU
	CPUPercent float64 `json:"cpu_percent"`
	CPUCores   int     `json:"cpu_cores"`
	LoadAvg1m  float64 `json:"load_avg_1m"`
	LoadAvg5m  float64 `json:"load_avg_5m"`
	LoadAvg15m float64 `json:"load_avg_15m"`

	// Memory
	MemTotalBytes  uint64  `json:"mem_total_bytes"`
	MemUsedBytes   uint64  `json:"mem_used_bytes"`
	MemAvailBytes  uint64  `json:"mem_avail_bytes"`
	MemPercent     float64 `json:"mem_percent"`
	SwapTotalBytes uint64  `json:"swap_total_bytes"`
	SwapUsedBytes  uint64  `json:"swap_used_bytes"`

	// Disk Block I/O
	DiskReadBps   uint64 `json:"disk_read_bps"`
	DiskWriteBps  uint64 `json:"disk_write_bps"`
	DiskReadIOPS  uint64 `json:"disk_read_iops"`
	DiskWriteIOPS uint64 `json:"disk_write_iops"`

	// Network I/O
	NetRxBps    uint64 `json:"net_rx_bps"`
	NetTxBps    uint64 `json:"net_tx_bps"`
	NetRxErrors uint64 `json:"net_rx_errors"`
	NetTxErrors uint64 `json:"net_tx_errors"`
}

// ToMetricPoint converts HostMetrics into a compact 32-byte MetricPoint.
func (h *HostMetrics) ToMetricPoint() MetricPoint {
	ts := h.Timestamp.Unix()
	if ts <= 0 {
		ts = time.Now().Unix()
	}
	return MetricPoint{
		Timestamp:     ts,
		CPUPercent:    float32(h.CPUPercent),
		MemoryBytes:   h.MemUsedBytes,
		NetRxRate:     uint32(min(h.NetRxBps, math.MaxUint32)),
		NetTxRate:     uint32(min(h.NetTxBps, math.MaxUint32)),
		DiskReadRate:  uint32(min(h.DiskReadBps, math.MaxUint32)),
		DiskWriteRate: uint32(min(h.DiskWriteBps, math.MaxUint32)),
	}
}

// ContainerStats represents normalized Docker container resource consumption.
type ContainerStats struct {
	NodeID      string    `json:"node_id"`
	ContainerID string    `json:"container_id"`
	ServiceID   string    `json:"service_id"`
	ProjectID   string    `json:"project_id"`
	Timestamp   time.Time `json:"timestamp"`

	CPUPercent       float64 `json:"cpu_percent"`
	MemoryUsedBytes  uint64  `json:"memory_used_bytes"`
	MemoryLimitBytes uint64  `json:"memory_limit_bytes"`
	MemoryPercent    float64 `json:"memory_percent"`

	NetRxBytesRate uint64 `json:"net_rx_bytes_rate"`
	NetTxBytesRate uint64 `json:"net_tx_bytes_rate"`
	BlockReadRate  uint64 `json:"block_read_rate"`
	BlockWriteRate uint64 `json:"block_write_rate"`

	PIDs   uint32 `json:"pids"`
	Status string `json:"status"` // "running" | "restarting" | "dead"
}

// ToMetricPoint converts ContainerStats into a compact 32-byte MetricPoint.
func (c *ContainerStats) ToMetricPoint() MetricPoint {
	ts := c.Timestamp.Unix()
	if ts <= 0 {
		ts = time.Now().Unix()
	}
	return MetricPoint{
		Timestamp:     ts,
		CPUPercent:    float32(c.CPUPercent),
		MemoryBytes:   c.MemoryUsedBytes,
		NetRxRate:     uint32(min(c.NetRxBytesRate, math.MaxUint32)),
		NetTxRate:     uint32(min(c.NetTxBytesRate, math.MaxUint32)),
		DiskReadRate:  uint32(min(c.BlockReadRate, math.MaxUint32)),
		DiskWriteRate: uint32(min(c.BlockWriteRate, math.MaxUint32)),
	}
}

// MetricPoint is a densely packed 32-byte struct stored in the ring buffer.
type MetricPoint struct {
	Timestamp     int64   // 8 bytes (Unix epoch seconds)
	CPUPercent    float32 // 4 bytes (0.00 - 100.00)
	MemoryBytes   uint64  // 8 bytes
	NetRxRate     uint32  // 4 bytes (Bytes/sec, up to 4GB/s)
	NetTxRate     uint32  // 4 bytes
	DiskReadRate  uint32  // 4 bytes
	DiskWriteRate uint32  // 4 bytes
}

// DownsampleAggregate represents the 1-hour calculated rollups stored in SQLite.
type DownsampleAggregate struct {
	EntityType     string    `json:"entity_type"`
	EntityID       string    `json:"entity_id"`
	BucketStart    time.Time `json:"bucket_start"`
	CPUAvg         float64   `json:"cpu_avg"`
	CPUMin         float64   `json:"cpu_min"`
	CPUMax         float64   `json:"cpu_max"`
	CPUP95         float64   `json:"cpu_p95"`
	MemAvg         uint64    `json:"mem_avg"`
	MemMax         uint64    `json:"mem_max"`
	NetRxTotal     uint64    `json:"net_rx_total"`
	NetTxTotal     uint64    `json:"net_tx_total"`
	DiskReadTotal  uint64    `json:"disk_read_total"`
	DiskWriteTotal uint64    `json:"disk_write_total"`
	SampleCount    int       `json:"sample_count"`
}

// StreamMessage is the envelope used across WebSocket and Agent tunnels.
type StreamMessage struct {
	Type      string      `json:"type"`      // "metric" | "log" | "command" | "ack" | "command_response"
	Channel   string      `json:"channel"`   // "node" | "container" | "service"
	TargetID  string      `json:"target_id"` // Entity identifier
	Timestamp int64       `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

// ============================================================================
// 2. CORE INTERFACES
// ============================================================================

// ProcReader defines the interface for low-overhead Linux kernel metrics.
type ProcReader interface {
	ReadHostMetrics(ctx context.Context) (*HostMetrics, error)
}

// DockerCollector defines the interface for collecting container statistics.
type DockerCollector interface {
	Start(ctx context.Context) error
	StreamContainerStats(ctx context.Context, out chan<- ContainerStats) error
	Stop() error
}

// AgentClient represents the standalone worker agent's client engine.
type AgentClient interface {
	// Start connects outbound to the Control Plane and initiates the telemetry loop.
	Start(ctx context.Context) error
	// SendTelemetry streams a batch of host and container metrics.
	SendTelemetry(ctx context.Context, msg *StreamMessage) error
	// Close gracefully disconnects the agent session.
	Close() error
}

// AgentServer represents the Control Plane hub receiving worker agent streams.
type AgentServer interface {
	// HandleAgentConnection upgrades incoming agent HTTP request to WebSocket.
	HandleAgentConnection(ctx context.Context, conn net.Conn, nodeID string) error
	// DispatchCommand routes a command to a specific worker node.
	DispatchCommand(ctx context.Context, nodeID string, cmd *StreamMessage) (*StreamMessage, error)
	// RegisterNode registers a newly authenticated worker node.
	RegisterNode(nodeID string, session interface{})
	// UnregisterNode removes disconnected worker node sessions.
	UnregisterNode(nodeID string)
	// RingBufferSnapshot returns a thread-safe copy of the ring buffer registry,
	// safe for callers to range over without racing concurrent writers.
	RingBufferSnapshot() map[string]RingBuffer
	// GetRingBuffer returns a ring buffer by key in a thread-safe manner.
	GetRingBuffer(key string) RingBuffer
}

// RingBuffer defines the high-performance in-memory circular storage.
type RingBuffer interface {
	// Push appends a new metric point, overwriting oldest when capacity is reached.
	Push(point MetricPoint)
	// GetRange returns all points within [from, to] timestamps (inclusive).
	GetRange(from, to int64) []MetricPoint
	// GetLastN returns the most recent N points in chronological order.
	GetLastN(n int) []MetricPoint
	// DownsampleHour computes aggregates for the specified hour window.
	DownsampleHour(hourStart int64) (*DownsampleAggregate, error)
	// Clear resets the buffer.
	Clear()
	// Len returns the current number of points stored.
	Len() int
	// Capacity returns the maximum number of points the buffer can hold.
	Capacity() int
}

// WebSocketHub manages real-time broadcast to UI clients.
type WebSocketHub interface {
	// Broadcast sends a stream message to all subscribed clients.
	Broadcast(msg *StreamMessage)
	// Subscribe registers a client connection to a specific topic/target.
	Subscribe(clientID string, channel, targetID string, sendChan chan<- []byte)
	// Unsubscribe removes a client subscription.
	Unsubscribe(clientID string, channel, targetID string)
	// UnregisterClient cleans up all subscriptions for a client upon disconnect.
	UnregisterClient(clientID string)
}
