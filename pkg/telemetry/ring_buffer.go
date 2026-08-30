package telemetry

import (
	"math"
	"sort"
	"sync"
	"time"
)

// DefaultRingBufferCapacity represents 24 hours of 10-second resolution telemetry (24 * 60 * 6 = 8640).
const DefaultRingBufferCapacity = 8640

type circularRingBuffer struct {
	mu       sync.RWMutex
	capacity int
	data     []MetricPoint
	head     int
	size     int
}

// NewRingBuffer allocates an in-memory ring buffer with fixed capacity.
// 24 hours @ 10s resolution = 8640 capacity.
func NewRingBuffer(capacity int) RingBuffer {
	if capacity <= 0 {
		capacity = DefaultRingBufferCapacity
	}
	return &circularRingBuffer{
		capacity: capacity,
		data:     make([]MetricPoint, capacity),
		head:     0,
		size:     0,
	}
}

// Push appends a new metric point, overwriting the oldest point when capacity is reached.
func (r *circularRingBuffer) Push(point MetricPoint) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.size < r.capacity {
		r.data[r.size] = point
		r.size++
		r.head = r.size - 1
	} else {
		r.head = (r.head + 1) % r.capacity
		r.data[r.head] = point
	}
}

// GetLastN returns the most recent N points in chronological order (oldest to newest).
func (r *circularRingBuffer) GetLastN(n int) []MetricPoint {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.size == 0 || n <= 0 {
		return nil
	}

	if n > r.size {
		n = r.size
	}

	result := make([]MetricPoint, n)
	for i := 0; i < n; i++ {
		idx := (r.head - n + 1 + i + r.capacity) % r.capacity
		result[i] = r.data[idx]
	}
	return result
}

// GetRange returns all points within [from, to] timestamps (inclusive) in chronological order.
// ponytail: basic linear search on GetRange <- 8,640 points per container -> binary search when capacity > 50,000 points
func (r *circularRingBuffer) GetRange(from, to int64) []MetricPoint {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.size == 0 || from > to {
		return nil
	}

	result := make([]MetricPoint, 0, min(r.size, 512))
	for i := 0; i < r.size; i++ {
		idx := (r.head - r.size + 1 + i + r.capacity) % r.capacity
		pt := r.data[idx]
		if pt.Timestamp >= from && pt.Timestamp <= to {
			result = append(result, pt)
		}
	}
	return result
}

// DownsampleHour computes aggregates (Min, Max, Avg, P95) for the specified 1-hour window.
func (r *circularRingBuffer) DownsampleHour(hourStart int64) (*DownsampleAggregate, error) {
	hourEnd := hourStart + 3600
	points := r.GetRange(hourStart, hourEnd)
	if len(points) == 0 {
		return nil, nil
	}

	var sumCPU float64
	minCPU := float64(math.MaxFloat32)
	maxCPU := float64(-1.0)
	cpuValues := make([]float64, len(points))

	var sumMem uint64
	var maxMem uint64
	var totalNetRx uint64
	var totalNetTx uint64
	var totalDiskRead uint64
	var totalDiskWrite uint64

	for i, pt := range points {
		cpu := float64(pt.CPUPercent)
		sumCPU += cpu
		cpuValues[i] = cpu
		if cpu < minCPU {
			minCPU = cpu
		}
		if cpu > maxCPU {
			maxCPU = cpu
		}

		sumMem += pt.MemoryBytes
		if pt.MemoryBytes > maxMem {
			maxMem = pt.MemoryBytes
		}

		// Rates multiplied by 10s interval to estimate absolute byte volume
		totalNetRx += uint64(pt.NetRxRate) * 10
		totalNetTx += uint64(pt.NetTxRate) * 10
		totalDiskRead += uint64(pt.DiskReadRate) * 10
		totalDiskWrite += uint64(pt.DiskWriteRate) * 10
	}

	// Calculate P95 CPU
	sort.Float64s(cpuValues)
	p95Idx := int(float64(len(cpuValues)-1) * 0.95)
	if p95Idx >= len(cpuValues) {
		p95Idx = len(cpuValues) - 1
	}

	return &DownsampleAggregate{
		BucketStart:    time.Unix(hourStart, 0).UTC(),
		CPUAvg:         sumCPU / float64(len(points)),
		CPUMin:         minCPU,
		CPUMax:         maxCPU,
		CPUP95:         cpuValues[p95Idx],
		MemAvg:         sumMem / uint64(len(points)),
		MemMax:         maxMem,
		NetRxTotal:     totalNetRx,
		NetTxTotal:     totalNetTx,
		DiskReadTotal:  totalDiskRead,
		DiskWriteTotal: totalDiskWrite,
		SampleCount:    len(points),
	}, nil
}

// Clear resets the buffer state.
func (r *circularRingBuffer) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.head = 0
	r.size = 0
}

// Len returns the number of points currently in the buffer.
func (r *circularRingBuffer) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.size
}

// Capacity returns the maximum buffer capacity.
func (r *circularRingBuffer) Capacity() int {
	return r.capacity
}
