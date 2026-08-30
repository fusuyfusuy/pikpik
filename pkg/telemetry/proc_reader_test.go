package telemetry_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcStatCPUDeltaMath validates non-negative CPU percentages across ticks on live host if Linux.
func TestProcStatCPUDeltaMath(t *testing.T) {
	reader := telemetry.NewProcReader()
	metrics, err := reader.ReadHostMetrics(context.Background())
	if err != nil {
		t.Skip("Skipping live /proc test in non-Linux environment")
	}

	assert.GreaterOrEqual(t, metrics.CPUPercent, 0.0)
	assert.LessOrEqual(t, metrics.CPUPercent, 100.0)
	assert.Greater(t, metrics.CPUCores, 0)
	assert.Greater(t, metrics.MemTotalBytes, uint64(0))
}

// TestProcReaderMockFileSystem tests exact mathematical parsing against synthetic /proc files.
func TestProcReaderMockFileSystem(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Create mock /proc/stat
	statTick1 := "cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 50 0 50 400 0 0 0 0 0 0\ncpu1 50 0 50 400 0 0 0 0 0 0\n"
	err := os.WriteFile(filepath.Join(tmpDir, "stat"), []byte(statTick1), 0644)
	require.NoError(t, err)

	// 2. Create mock /proc/meminfo
	meminfo := `MemTotal:       16384000 kB
MemAvailable:    8192000 kB
MemFree:         4096000 kB
Buffers:          512000 kB
Cached:          3584000 kB
SwapTotal:       2048000 kB
SwapFree:        1024000 kB
`
	err = os.WriteFile(filepath.Join(tmpDir, "meminfo"), []byte(meminfo), 0644)
	require.NoError(t, err)

	// 3. Create mock /proc/diskstats
	diskstats := `   8       0 sda 100 0 2000 50 50 0 1000 25 0 0 0
   7       0 loop0 10 0 20 5 0 0 0 0 0 0 0
`
	err = os.WriteFile(filepath.Join(tmpDir, "diskstats"), []byte(diskstats), 0644)
	require.NoError(t, err)

	// 4. Create mock /proc/net/dev
	err = os.MkdirAll(filepath.Join(tmpDir, "net"), 0755)
	require.NoError(t, err)

	netdev := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 1000000     100    0    0    0     0          0         0  1000000     100    0    0    0     0       0          0
  eth0: 2048000     200    2    0    0     0          0         0  1024000     100    1    0    0     0       0          0
`
	err = os.WriteFile(filepath.Join(tmpDir, "net", "dev"), []byte(netdev), 0644)
	require.NoError(t, err)

	// 5. Create mock /proc/loadavg
	loadavg := "0.42 0.75 1.10 2/250 12345\n"
	err = os.WriteFile(filepath.Join(tmpDir, "loadavg"), []byte(loadavg), 0644)
	require.NoError(t, err)

	// 6. Create mock /proc/uptime
	uptime := "12345.67 98765.43\n"
	err = os.WriteFile(filepath.Join(tmpDir, "uptime"), []byte(uptime), 0644)
	require.NoError(t, err)

	reader := telemetry.NewProcReaderWithRoot(tmpDir)

	// First tick (initializes baseline)
	m1, err := reader.ReadHostMetrics(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, m1.CPUCores)
	assert.Equal(t, uint64(16384000*1024), m1.MemTotalBytes)
	assert.Equal(t, uint64(8192000*1024), m1.MemAvailBytes)
	assert.Equal(t, uint64(8192000*1024), m1.MemUsedBytes)
	assert.InDelta(t, 50.0, m1.MemPercent, 0.01)
	assert.Equal(t, uint64(2048000*1024), m1.SwapTotalBytes)
	assert.Equal(t, uint64(1024000*1024), m1.SwapUsedBytes)
	assert.InDelta(t, 0.42, m1.LoadAvg1m, 0.001)
	assert.InDelta(t, 0.75, m1.LoadAvg5m, 0.001)
	assert.InDelta(t, 1.10, m1.LoadAvg15m, 0.001)
	assert.Equal(t, uint64(12345), m1.UptimeSeconds)
	assert.Equal(t, uint64(2), m1.NetRxErrors)
	assert.Equal(t, uint64(1), m1.NetTxErrors)

	// Second tick: simulate CPU activity and I/O traffic
	time.Sleep(50 * time.Millisecond)

	// In tick 2: total +1000, idle +500 => CPU is 50%
	statTick2 := "cpu  300 0 300 1200 0 0 0 0 0 0\ncpu0 150 0 150 600 0 0 0 0 0 0\ncpu1 150 0 150 600 0 0 0 0 0 0\n"
	err = os.WriteFile(filepath.Join(tmpDir, "stat"), []byte(statTick2), 0644)
	require.NoError(t, err)

	// Disk: sda reads +2000 sectors (1MB), writes +1000 sectors (512KB)
	diskstatsTick2 := `   8       0 sda 150 0 4000 50 75 0 2000 25 0 0 0
`
	err = os.WriteFile(filepath.Join(tmpDir, "diskstats"), []byte(diskstatsTick2), 0644)
	require.NoError(t, err)

	// Net: eth0 rx +102400 bytes, tx +51200 bytes
	netdevTick2 := `Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
  eth0: 2150400     210    2    0    0     0          0         0  1075200     110    1    0    0     0       0          0
`
	err = os.WriteFile(filepath.Join(tmpDir, "net", "dev"), []byte(netdevTick2), 0644)
	require.NoError(t, err)

	m2, err := reader.ReadHostMetrics(context.Background())
	require.NoError(t, err)

	// CPU delta: (800 - 400) / 800 * 100 = 50%
	assert.InDelta(t, 50.0, m2.CPUPercent, 1.0)
	assert.Greater(t, m2.DiskReadBps, uint64(0))
	assert.Greater(t, m2.DiskWriteBps, uint64(0))
	assert.Greater(t, m2.DiskReadIOPS, uint64(0))
	assert.Greater(t, m2.DiskWriteIOPS, uint64(0))
	assert.Greater(t, m2.NetRxBps, uint64(0))
	assert.Greater(t, m2.NetTxBps, uint64(0))

	// MetricPoint conversion
	pt := m2.ToMetricPoint()
	assert.Equal(t, m2.Timestamp.Unix(), pt.Timestamp)
	assert.InDelta(t, float32(50.0), pt.CPUPercent, 1.0)
	assert.Equal(t, m2.MemUsedBytes, pt.MemoryBytes)
}

// TestProcReaderMissingFiles verifies graceful error returns when /proc files do not exist.
func TestProcReaderMissingFiles(t *testing.T) {
	tmpDir := t.TempDir()
	reader := telemetry.NewProcReaderWithRoot(tmpDir)

	_, err := reader.ReadHostMetrics(context.Background())
	assert.Error(t, err)
}
