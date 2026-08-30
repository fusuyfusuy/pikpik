package telemetry_test

import (
	"sync"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRingBufferWrapAround validates circular modulo indexing and oldest overwrite.
func TestRingBufferWrapAround(t *testing.T) {
	buf := telemetry.NewRingBuffer(5)

	// Insert 5 points
	for i := 1; i <= 5; i++ {
		buf.Push(telemetry.MetricPoint{
			Timestamp:  int64(i * 10),
			CPUPercent: float32(i * 10),
		})
	}

	pts := buf.GetLastN(5)
	require.Len(t, pts, 5)
	assert.Equal(t, int64(10), pts[0].Timestamp)
	assert.Equal(t, int64(50), pts[4].Timestamp)

	// Push 6th point (overwrites index 0)
	buf.Push(telemetry.MetricPoint{
		Timestamp:  60,
		CPUPercent: 60.0,
	})

	ptsAfter := buf.GetLastN(5)
	require.Len(t, ptsAfter, 5)
	assert.Equal(t, int64(20), ptsAfter[0].Timestamp) // 10 was evicted
	assert.Equal(t, int64(60), ptsAfter[4].Timestamp)
	assert.Equal(t, 5, buf.Len())
	assert.Equal(t, 5, buf.Capacity())
}

// TestRingBufferDownsamplerMath tests Min, Max, Avg, P95 calculations.
func TestRingBufferDownsamplerMath(t *testing.T) {
	buf := telemetry.NewRingBuffer(100)
	now := time.Now().Truncate(time.Hour).Unix()

	// Insert 100 points with CPU ranging from 1% to 100%
	for i := 1; i <= 100; i++ {
		buf.Push(telemetry.MetricPoint{
			Timestamp:     now + int64(i*10),
			CPUPercent:    float32(i),
			MemoryBytes:   1024 * 1024 * 100, // 100 MB
			NetRxRate:     1000,
			NetTxRate:     2000,
			DiskReadRate:  500,
			DiskWriteRate: 1500,
		})
	}

	agg, err := buf.DownsampleHour(now)
	require.NoError(t, err)
	require.NotNil(t, agg)

	assert.Equal(t, 100, agg.SampleCount)
	assert.InDelta(t, 50.5, agg.CPUAvg, 0.01)
	assert.Equal(t, 1.0, agg.CPUMin)
	assert.Equal(t, 100.0, agg.CPUMax)
	assert.Equal(t, 95.0, agg.CPUP95)
	assert.Equal(t, uint64(1024*1024*100), agg.MemAvg)
	assert.Equal(t, uint64(1024*1024*100), agg.MemMax)
	assert.Equal(t, uint64(1000*10*100), agg.NetRxTotal)
	assert.Equal(t, uint64(2000*10*100), agg.NetTxTotal)
	assert.Equal(t, uint64(500*10*100), agg.DiskReadTotal)
	assert.Equal(t, uint64(1500*10*100), agg.DiskWriteTotal)
}

// TestRingBufferGetRange verifies range filtering and edge conditions.
func TestRingBufferGetRange(t *testing.T) {
	buf := telemetry.NewRingBuffer(10)

	for i := 1; i <= 10; i++ {
		buf.Push(telemetry.MetricPoint{
			Timestamp:  int64(i * 100),
			CPUPercent: float32(i),
		})
	}

	// Range within boundaries
	pts := buf.GetRange(300, 700)
	require.Len(t, pts, 5)
	assert.Equal(t, int64(300), pts[0].Timestamp)
	assert.Equal(t, int64(700), pts[4].Timestamp)

	// Range out of bounds
	ptsEmpty := buf.GetRange(1500, 2000)
	assert.Empty(t, ptsEmpty)

	// Inverted range
	ptsInverted := buf.GetRange(700, 300)
	assert.Nil(t, ptsInverted)

	// GetLastN with N > size
	lastLarge := buf.GetLastN(50)
	assert.Len(t, lastLarge, 10)
}

// TestRingBufferClear verifies buffer reset.
func TestRingBufferClear(t *testing.T) {
	buf := telemetry.NewRingBuffer(10)
	buf.Push(telemetry.MetricPoint{Timestamp: 100, CPUPercent: 5.0})
	assert.Equal(t, 1, buf.Len())

	buf.Clear()
	assert.Equal(t, 0, buf.Len())
	assert.Empty(t, buf.GetLastN(5))
	assert.Empty(t, buf.GetRange(0, 200))
}

// TestRingBufferConcurrentAccess validates thread safety under high concurrency.
func TestRingBufferConcurrentAccess(t *testing.T) {
	buf := telemetry.NewRingBuffer(500)
	var wg sync.WaitGroup

	// 10 concurrent writers
	for w := 0; w < 10; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				buf.Push(telemetry.MetricPoint{
					Timestamp:  int64(workerID*1000 + i),
					CPUPercent: float32(i % 100),
				})
			}
		}(w)
	}

	// 5 concurrent readers
	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = buf.GetLastN(50)
				_ = buf.GetRange(0, 100000)
				time.Sleep(1 * time.Millisecond)
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, 500, buf.Len())
}
