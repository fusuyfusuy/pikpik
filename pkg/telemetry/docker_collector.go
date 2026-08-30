package telemetry

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DockerStatsJSON models the raw JSON chunk streamed by Docker daemon /containers/{id}/stats?stream=true.
type DockerStatsJSON struct {
	Read      time.Time `json:"read"`
	Preread   time.Time `json:"preread"`
	CPUStats  RawCPUStats `json:"cpu_stats"`
	PreCPUStats RawCPUStats `json:"precpu_stats"`
	MemoryStats RawMemoryStats `json:"memory_stats"`
	Networks  map[string]RawNetStats `json:"networks"`
	BlkioStats RawBlkioStats `json:"blkio_stats"`
	PidsStats struct {
		Current uint32 `json:"current"`
	} `json:"pids_stats"`
}

type RawCPUStats struct {
	CPUUsage struct {
		TotalUsage  uint64   `json:"total_usage"`
		PercpuUsage []uint64 `json:"percpu_usage"`
	} `json:"cpu_usage"`
	SystemCPUUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs     uint32 `json:"online_cpus"`
}

type RawMemoryStats struct {
	Usage uint64            `json:"usage"`
	Limit uint64            `json:"limit"`
	Stats map[string]uint64 `json:"stats"`
}

type RawNetStats struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

type RawBlkioStats struct {
	IOServiceBytesRecursive []struct {
		Op    string `json:"op"`
		Value uint64 `json:"value"`
	} `json:"io_service_bytes_recursive"`
}

// DockerContainerSummary models the JSON returned by GET /containers/json.
type DockerContainerSummary struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

// DockerEvent models a single container event from GET /events.
type DockerEvent struct {
	Type   string `json:"Type"`
	Action string `json:"Action"`
	Actor  struct {
		ID         string            `json:"ID"`
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
	Time int64 `json:"time"`
}

// DockerSocketCollector implements DockerCollector using direct HTTP over the Docker Unix domain socket.
type DockerSocketCollector struct {
	socketPath string
	nodeID     string
	client     *http.Client

	mu          sync.Mutex
	running     bool
	ctx         context.Context
	cancel      context.CancelFunc
	streams     map[string]context.CancelFunc
	lastAttempt map[string]time.Time
	outChan     chan<- ContainerStats
}

// NewDockerCollector creates a new DockerSocketCollector for the given Unix domain socket path.
func NewDockerCollector(socketPath, nodeID string) DockerCollector {
	if socketPath == "" {
		socketPath = "/var/run/docker.sock"
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, proto, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
		DisableKeepAlives:     false,
		MaxIdleConns:          50,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}

	return &DockerSocketCollector{
		socketPath:  socketPath,
		nodeID:      nodeID,
		client:      &http.Client{Transport: tr},
		streams:     make(map[string]context.CancelFunc),
		lastAttempt: make(map[string]time.Time),
	}
}

// Start begins background discovery and event monitoring.
func (d *DockerSocketCollector) Start(ctx context.Context) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return nil
	}
	d.ctx, d.cancel = context.WithCancel(ctx)
	d.running = true
	d.mu.Unlock()

	return nil
}

// StreamContainerStats launches the container discovery and stats streaming loop into out.
func (d *DockerSocketCollector) StreamContainerStats(ctx context.Context, out chan<- ContainerStats) error {
	d.mu.Lock()
	d.outChan = out
	d.mu.Unlock()

	if err := d.Start(ctx); err != nil {
		return err
	}

	// 1. Initial container discovery
	go d.discoverAndStreamActiveContainers()

	// 2. Events listener loop (auto reconnects if dropped)
	go d.eventsLoop()

	return nil
}

// Stop terminates all running container stats streams.
func (d *DockerSocketCollector) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return nil
	}
	d.running = false
	if d.cancel != nil {
		d.cancel()
	}
	for id, cancelFn := range d.streams {
		cancelFn()
		delete(d.streams, id)
	}
	return nil
}

func (d *DockerSocketCollector) discoverAndStreamActiveContainers() {
	containers, err := d.listRunningContainers()
	if err != nil {
		return
	}

	for _, c := range containers {
		d.spawnContainerStream(c.ID, c.Labels)
	}
}

func (d *DockerSocketCollector) listRunningContainers() ([]DockerContainerSummary, error) {
	req, err := http.NewRequestWithContext(d.ctx, "GET", "http://localhost/containers/json", nil)
	if err != nil {
		return nil, err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status from docker: %d", resp.StatusCode)
	}

	var containers []DockerContainerSummary
	if err := json.NewDecoder(resp.Body).Decode(&containers); err != nil {
		return nil, err
	}
	return containers, nil
}

func (d *DockerSocketCollector) eventsLoop() {
	for {
		select {
		case <-d.ctx.Done():
			return
		default:
		}

		err := d.listenEvents()
		if err != nil {
			select {
			case <-d.ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}
}

func (d *DockerSocketCollector) listenEvents() error {
	req, err := http.NewRequestWithContext(d.ctx, "GET", "http://localhost/events?filters=%7B%22type%22%3A%5B%22container%22%5D%7D", nil)
	if err != nil {
		return err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	decoder := json.NewDecoder(resp.Body)
	for {
		var ev DockerEvent
		if err := decoder.Decode(&ev); err != nil {
			return err
		}

		switch ev.Action {
		case "start", "unpause":
			d.spawnContainerStream(ev.Actor.ID, ev.Actor.Attributes)
		case "die", "destroy", "stop", "pause", "kill":
			d.stopContainerStream(ev.Actor.ID)
		}
	}
}

func (d *DockerSocketCollector) spawnContainerStream(containerID string, labels map[string]string) {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return
	}

	// Check if already active
	if _, exists := d.streams[containerID]; exists {
		d.mu.Unlock()
		return
	}

	// Rate limiting: max 1 stream per 5 seconds per container
	if last, ok := d.lastAttempt[containerID]; ok && time.Since(last) < 5*time.Second {
		d.mu.Unlock()
		return
	}
	d.lastAttempt[containerID] = time.Now()

	streamCtx, cancelFn := context.WithCancel(d.ctx)
	d.streams[containerID] = cancelFn
	out := d.outChan
	d.mu.Unlock()

	go d.streamSingleContainer(streamCtx, containerID, labels, out)
}

func (d *DockerSocketCollector) stopContainerStream(containerID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if cancelFn, exists := d.streams[containerID]; exists {
		cancelFn()
		delete(d.streams, containerID)
	}
}

func (d *DockerSocketCollector) streamSingleContainer(ctx context.Context, containerID string, labels map[string]string, out chan<- ContainerStats) {
	defer d.stopContainerStream(containerID)

	serviceID := labels["pikpik.service.id"]
	if serviceID == "" {
		serviceID = labels["com.docker.compose.service"]
	}
	projectID := labels["pikpik.project.id"]
	if projectID == "" {
		projectID = labels["com.docker.compose.project"]
	}

	url := fmt.Sprintf("http://localhost/containers/%s/stats?stream=true", containerID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	// Inactivity watchdog to close body on dead/hung streams
	streamCtx, cancelStream := context.WithCancel(ctx)
	defer cancelStream()

	watchdog := time.AfterFunc(30*time.Second, func() {
		_ = resp.Body.Close()
	})
	defer watchdog.Stop()

	scanner := bufio.NewScanner(resp.Body)

	// Buffer can grow up to 64KB for large container stats
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 256*1024)

	var prevStats *DockerStatsJSON
	var prevTime time.Time
	var prevNetRx, prevNetTx uint64
	var prevBlkRead, prevBlkWrite uint64

	for scanner.Scan() {
		watchdog.Reset(30 * time.Second)
		select {
		case <-streamCtx.Done():
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var stats DockerStatsJSON
		if err := json.Unmarshal(line, &stats); err != nil {
			continue
		}

		now := stats.Read
		if now.IsZero() {
			now = time.Now().UTC()
		}

		// Calculate CPU %
		cpuPercent := calculateCPUPercent(&stats, prevStats)

		// Calculate Memory % (cgroups v1 & v2 compatible)
		usedMem, memLimit, memPercent := calculateMemory(&stats)

		// Calculate Network & Block I/O rates
		var curNetRx, curNetTx uint64
		for _, netStats := range stats.Networks {
			curNetRx += netStats.RxBytes
			curNetTx += netStats.TxBytes
		}

		var curBlkRead, curBlkWrite uint64
		for _, blk := range stats.BlkioStats.IOServiceBytesRecursive {
			switch strings.ToLower(blk.Op) {
			case "read":
				curBlkRead += blk.Value
			case "write":
				curBlkWrite += blk.Value
			}
		}

		var netRxRate, netTxRate, blkReadRate, blkWriteRate uint64
		if !prevTime.IsZero() {
			dt := now.Sub(prevTime).Seconds()
			if dt > 0 {
				if curNetRx >= prevNetRx {
					netRxRate = uint64(float64(curNetRx-prevNetRx) / dt)
				}
				if curNetTx >= prevNetTx {
					netTxRate = uint64(float64(curNetTx-prevNetTx) / dt)
				}
				if curBlkRead >= prevBlkRead {
					blkReadRate = uint64(float64(curBlkRead-prevBlkRead) / dt)
				}
				if curBlkWrite >= prevBlkWrite {
					blkWriteRate = uint64(float64(curBlkWrite-prevBlkWrite) / dt)
				}
			}
		}

		containerStat := ContainerStats{
			NodeID:           d.nodeID,
			ContainerID:      containerID,
			ServiceID:        serviceID,
			ProjectID:        projectID,
			Timestamp:        now,
			CPUPercent:       cpuPercent,
			MemoryUsedBytes:  usedMem,
			MemoryLimitBytes: memLimit,
			MemoryPercent:    memPercent,
			NetRxBytesRate:   netRxRate,
			NetTxBytesRate:   netTxRate,
			BlockReadRate:    blkReadRate,
			BlockWriteRate:   blkWriteRate,
			PIDs:             stats.PidsStats.Current,
			Status:           "running",
		}

		if out != nil {
			select {
			case out <- containerStat:
			case <-ctx.Done():
				return
			default:
				// Non-blocking drop if channel buffer full
			}
		}

		prevStatsCopy := stats
		prevStats = &prevStatsCopy
		prevTime = now
		prevNetRx = curNetRx
		prevNetTx = curNetTx
		prevBlkRead = curBlkRead
		prevBlkWrite = curBlkWrite
	}
}

// CalculateCPUPercent computes CPU utilization percentage from Docker stats JSON.
func calculateCPUPercent(stats *DockerStatsJSON, prevStats *DockerStatsJSON) float64 {
	if prevStats == nil {
		// Use PreCPUStats if provided in the JSON chunk itself
		if stats.PreCPUStats.CPUUsage.TotalUsage > 0 && stats.PreCPUStats.SystemCPUUsage > 0 {
			prevStats = &DockerStatsJSON{
				CPUStats: stats.PreCPUStats,
			}
		} else {
			return 0.0
		}
	}

	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(prevStats.CPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemCPUUsage) - float64(prevStats.CPUStats.SystemCPUUsage)

	onlineCPUs := float64(stats.CPUStats.OnlineCPUs)
	if onlineCPUs == 0 {
		onlineCPUs = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if onlineCPUs == 0 {
		onlineCPUs = 1.0
	}

	if systemDelta > 0.0 && cpuDelta > 0.0 {
		pct := (cpuDelta / systemDelta) * onlineCPUs * 100.0
		if pct < 0.0 {
			return 0.0
		}
		return pct
	}
	return 0.0
}

// CalculateMemory computes used memory and percentage across cgroups v1 and v2.
func calculateMemory(stats *DockerStatsJSON) (used uint64, limit uint64, percent float64) {
	limit = stats.MemoryStats.Limit

	var inactiveFile uint64
	if val, ok := stats.MemoryStats.Stats["inactive_file"]; ok {
		inactiveFile = val
	} else if val, ok := stats.MemoryStats.Stats["total_inactive_file"]; ok {
		inactiveFile = val
	} else if val, ok := stats.MemoryStats.Stats["cache"]; ok {
		inactiveFile = val
	}

	used = stats.MemoryStats.Usage
	if used > inactiveFile {
		used -= inactiveFile
	}

	if limit > 0 {
		percent = (float64(used) / float64(limit)) * 100.0
		if percent > 100.0 {
			percent = 100.0
		}
	}
	return used, limit, percent
}
