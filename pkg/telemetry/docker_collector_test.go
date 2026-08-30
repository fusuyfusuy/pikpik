package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalculateCPUPercent validates CPU percentage computation across sample ticks.
func TestCalculateCPUPercent(t *testing.T) {
	// Baseline: 4 cores, system delta 1000, cpu delta 250 -> 25% of 4 cores = 100% total cpu
	prev := &DockerStatsJSON{
		CPUStats: RawCPUStats{
			CPUUsage: struct {
				TotalUsage  uint64   `json:"total_usage"`
				PercpuUsage []uint64 `json:"percpu_usage"`
			}{
				TotalUsage: 1000,
			},
			SystemCPUUsage: 10000,
			OnlineCPUs:     4,
		},
	}

	curr := &DockerStatsJSON{
		CPUStats: RawCPUStats{
			CPUUsage: struct {
				TotalUsage  uint64   `json:"total_usage"`
				PercpuUsage []uint64 `json:"percpu_usage"`
			}{
				TotalUsage: 1250,
			},
			SystemCPUUsage: 11000,
			OnlineCPUs:     4,
		},
	}

	pct := calculateCPUPercent(curr, prev)
	// (250 / 1000) * 4 * 100 = 100.0%
	assert.InDelta(t, 100.0, pct, 0.01)

	// Fallback to percpu_usage if OnlineCPUs == 0
	curr.CPUStats.OnlineCPUs = 0
	curr.CPUStats.CPUUsage.PercpuUsage = []uint64{300, 300, 300, 350}
	pctPercpu := calculateCPUPercent(curr, prev)
	assert.InDelta(t, 100.0, pctPercpu, 0.01)

	// Single tick with PreCPUStats embedded
	single := &DockerStatsJSON{
		CPUStats: curr.CPUStats,
		PreCPUStats: RawCPUStats{
			CPUUsage: struct {
				TotalUsage  uint64   `json:"total_usage"`
				PercpuUsage []uint64 `json:"percpu_usage"`
			}{
				TotalUsage: 1000,
			},
			SystemCPUUsage: 10000,
			OnlineCPUs:     4,
		},
	}
	pctSingle := calculateCPUPercent(single, nil)
	assert.InDelta(t, 100.0, pctSingle, 0.01)
}

// TestCalculateMemory validates cgroups v1 and v2 memory subtraction.
func TestCalculateMemory(t *testing.T) {
	// cgroups v2 inactive_file
	statsV2 := &DockerStatsJSON{
		MemoryStats: RawMemoryStats{
			Usage: 200 * 1024 * 1024,
			Limit: 1024 * 1024 * 1024,
			Stats: map[string]uint64{
				"inactive_file": 50 * 1024 * 1024,
			},
		},
	}

	used, limit, pct := calculateMemory(statsV2)
	assert.Equal(t, uint64(150*1024*1024), used)
	assert.Equal(t, uint64(1024*1024*1024), limit)
	assert.InDelta(t, (150.0/1024.0)*100.0, pct, 0.01)

	// cgroups v1 cache / total_inactive_file
	statsV1 := &DockerStatsJSON{
		MemoryStats: RawMemoryStats{
			Usage: 300 * 1024 * 1024,
			Limit: 1024 * 1024 * 1024,
			Stats: map[string]uint64{
				"total_inactive_file": 100 * 1024 * 1024,
			},
		},
	}

	usedV1, _, pctV1 := calculateMemory(statsV1)
	assert.Equal(t, uint64(200*1024*1024), usedV1)
	assert.InDelta(t, (200.0/1024.0)*100.0, pctV1, 0.01)
}

// TestDockerSocketCollectorMockServer tests full stream ingestion against a mock Unix socket Docker daemon.
func TestDockerSocketCollectorMockServer(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "docker.sock")

	listener, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	defer listener.Close()

	mux := http.NewServeMux()

	// 1. GET /containers/json
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		containers := []DockerContainerSummary{
			{
				ID:    "c_web_01",
				Names: []string{"/web-service"},
				State: "running",
				Labels: map[string]string{
					"pikpik.service.id": "svc_web_01",
					"pikpik.project.id": "proj_main",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(containers)
	})

	// 2. GET /events
	mux.HandleFunc("/events", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		// Keep connection open during test
		<-r.Context().Done()
	})

	// 3. GET /containers/{id}/stats
	mux.HandleFunc("/containers/c_web_01/stats", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		stat := DockerStatsJSON{
			Read: time.Now().UTC(),
			CPUStats: RawCPUStats{
				CPUUsage: struct {
					TotalUsage  uint64   `json:"total_usage"`
					PercpuUsage []uint64 `json:"percpu_usage"`
				}{TotalUsage: 5000},
				SystemCPUUsage: 50000,
				OnlineCPUs:     2,
			},
			PreCPUStats: RawCPUStats{
				CPUUsage: struct {
					TotalUsage  uint64   `json:"total_usage"`
					PercpuUsage []uint64 `json:"percpu_usage"`
				}{TotalUsage: 4000},
				SystemCPUUsage: 40000,
				OnlineCPUs:     2,
			},
			MemoryStats: RawMemoryStats{
				Usage: 50 * 1024 * 1024,
				Limit: 512 * 1024 * 1024,
			},
		}

		data, _ := json.Marshal(stat)
		_, _ = fmt.Fprintf(w, "%s\n", data)
		flusher.Flush()

		time.Sleep(100 * time.Millisecond)

		stat.Read = time.Now().UTC()
		stat.CPUStats.CPUUsage.TotalUsage = 6000
		stat.CPUStats.SystemCPUUsage = 60000
		data2, _ := json.Marshal(stat)
		_, _ = fmt.Fprintf(w, "%s\n", data2)
		flusher.Flush()

		<-r.Context().Done()
	})

	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Close()

	collector := NewDockerCollector(sockPath, "node_test_01")
	outChan := make(chan ContainerStats, 10)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = collector.StreamContainerStats(ctx, outChan)
	require.NoError(t, err)

	select {
	case stat := <-outChan:
		assert.Equal(t, "node_test_01", stat.NodeID)
		assert.Equal(t, "c_web_01", stat.ContainerID)
		assert.Equal(t, "svc_web_01", stat.ServiceID)
		assert.Equal(t, "proj_main", stat.ProjectID)
		assert.Equal(t, uint64(50*1024*1024), stat.MemoryUsedBytes)
		assert.Equal(t, uint64(512*1024*1024), stat.MemoryLimitBytes)

		pt := stat.ToMetricPoint()
		assert.Equal(t, stat.Timestamp.Unix(), pt.Timestamp)
		assert.Equal(t, stat.MemoryUsedBytes, pt.MemoryBytes)
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for container stats from mock docker socket")
	}

	err = collector.Stop()
	assert.NoError(t, err)
}
