package telemetry

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// LinuxProcReader implements ProcReader by scraping the Linux /proc filesystem directly.
type LinuxProcReader struct {
	mu               sync.Mutex
	procRoot         string
	prevCPUTotal     uint64
	prevCPUIdle      uint64
	prevDiskTime     time.Time
	prevDiskRead     uint64
	prevDiskWrite    uint64
	prevDiskReadOps  uint64
	prevDiskWriteOps uint64
	prevNetTime      time.Time
	prevNetRx        uint64
	prevNetTx        uint64
}

// NewProcReader creates a new ProcReader pointing to the standard /proc filesystem.
func NewProcReader() ProcReader {
	return NewProcReaderWithRoot("/proc")
}

// NewProcReaderWithRoot creates a ProcReader pointing to a custom /proc root path (useful for testing).
func NewProcReaderWithRoot(procRoot string) ProcReader {
	return &LinuxProcReader{
		procRoot: procRoot,
	}
}

// ReadHostMetrics reads and calculates real-time operating system metrics.
func (p *LinuxProcReader) ReadHostMetrics(ctx context.Context) (*HostMetrics, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	metrics := &HostMetrics{
		Timestamp: time.Now().UTC(),
	}

	if err := p.readCPU(metrics); err != nil {
		return nil, fmt.Errorf("failed to parse /proc/stat: %w", err)
	}

	if err := p.readMem(metrics); err != nil {
		return nil, fmt.Errorf("failed to parse /proc/meminfo: %w", err)
	}

	if err := p.readDisk(metrics); err != nil {
		return nil, fmt.Errorf("failed to parse /proc/diskstats: %w", err)
	}

	if err := p.readNet(metrics); err != nil {
		return nil, fmt.Errorf("failed to parse /proc/net/dev: %w", err)
	}

	if err := p.readLoadAvg(metrics); err != nil {
		return nil, fmt.Errorf("failed to parse /proc/loadavg: %w", err)
	}

	if err := p.readUptime(metrics); err != nil {
		// Non-fatal if uptime is unavailable
		metrics.UptimeSeconds = 0
	}

	return metrics, nil
}

func (p *LinuxProcReader) readCPU(m *HostMetrics) error {
	path := filepath.Join(p.procRoot, "stat")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	coreCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "cpu ") {
			fields := strings.Fields(line)
			if len(fields) < 9 {
				continue
			}

			user, _ := strconv.ParseUint(fields[1], 10, 64)
			nice, _ := strconv.ParseUint(fields[2], 10, 64)
			system, _ := strconv.ParseUint(fields[3], 10, 64)
			idle, _ := strconv.ParseUint(fields[4], 10, 64)
			iowait, _ := strconv.ParseUint(fields[5], 10, 64)
			irq, _ := strconv.ParseUint(fields[6], 10, 64)
			softirq, _ := strconv.ParseUint(fields[7], 10, 64)
			steal, _ := strconv.ParseUint(fields[8], 10, 64)

			idleTime := idle + iowait
			nonIdleTime := user + nice + system + irq + softirq + steal
			totalTime := idleTime + nonIdleTime

			if p.prevCPUTotal > 0 && totalTime > p.prevCPUTotal {
				totalDelta := totalTime - p.prevCPUTotal
				idleDelta := idleTime - p.prevCPUIdle
				if totalDelta > 0 && idleDelta <= totalDelta {
					m.CPUPercent = float64(totalDelta-idleDelta) / float64(totalDelta) * 100.0
					if m.CPUPercent < 0 {
						m.CPUPercent = 0
					} else if m.CPUPercent > 100.0 {
						m.CPUPercent = 100.0
					}
				}
			}
			p.prevCPUTotal = totalTime
			p.prevCPUIdle = idleTime
		} else if strings.HasPrefix(line, "cpu") && len(line) > 3 && line[3] >= '0' && line[3] <= '9' {
			coreCount++
		}
	}
	if coreCount == 0 {
		coreCount = 1
	}
	m.CPUCores = coreCount
	return nil
}

func (p *LinuxProcReader) readMem(m *HostMetrics) error {
	path := filepath.Join(p.procRoot, "meminfo")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(bytes.NewReader(data))
	var memTotal, memAvail, memFree, buffers, cached, swapTotal, swapFree uint64

	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(fields[1], 10, 64)
		valBytes := val * 1024 // /proc/meminfo values are in kB

		switch fields[0] {
		case "MemTotal:":
			memTotal = valBytes
		case "MemAvailable:":
			memAvail = valBytes
		case "MemFree:":
			memFree = valBytes
		case "Buffers:":
			buffers = valBytes
		case "Cached:":
			cached = valBytes
		case "SwapTotal:":
			swapTotal = valBytes
		case "SwapFree:":
			swapFree = valBytes
		}
	}

	// If MemAvailable was missing (Linux < 3.14), estimate it
	if memAvail == 0 && (memFree > 0 || buffers > 0 || cached > 0) {
		memAvail = memFree + buffers + cached
	}

	m.MemTotalBytes = memTotal
	m.MemAvailBytes = memAvail
	if memTotal >= memAvail {
		m.MemUsedBytes = memTotal - memAvail
	}
	if memTotal > 0 {
		m.MemPercent = float64(m.MemUsedBytes) / float64(memTotal) * 100.0
	}
	m.SwapTotalBytes = swapTotal
	if swapTotal >= swapFree {
		m.SwapUsedBytes = swapTotal - swapFree
	}
	return nil
}

func (p *LinuxProcReader) readDisk(m *HostMetrics) error {
	path := filepath.Join(p.procRoot, "diskstats")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	now := time.Now()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var totalReadSectors, totalWriteSectors uint64
	var totalReadOps, totalWriteOps uint64

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 14 {
			continue
		}
		devName := fields[2]
		// Filter pseudo, virtual, and partition noise
		if strings.HasPrefix(devName, "loop") || strings.HasPrefix(devName, "ram") || strings.HasPrefix(devName, "zram") {
			continue
		}

		readsCompleted, _ := strconv.ParseUint(fields[3], 10, 64)
		readsSectors, _ := strconv.ParseUint(fields[5], 10, 64)    // 1 sector = 512 bytes
		writesCompleted, _ := strconv.ParseUint(fields[7], 10, 64)
		writesSectors, _ := strconv.ParseUint(fields[9], 10, 64)   // 1 sector = 512 bytes

		totalReadOps += readsCompleted
		totalReadSectors += readsSectors
		totalWriteOps += writesCompleted
		totalWriteSectors += writesSectors
	}

	readBytes := totalReadSectors * 512
	writeBytes := totalWriteSectors * 512

	if !p.prevDiskTime.IsZero() {
		dt := now.Sub(p.prevDiskTime).Seconds()
		if dt > 0 {
			if readBytes >= p.prevDiskRead {
				m.DiskReadBps = uint64(float64(readBytes-p.prevDiskRead) / dt)
			}
			if writeBytes >= p.prevDiskWrite {
				m.DiskWriteBps = uint64(float64(writeBytes-p.prevDiskWrite) / dt)
			}
			if totalReadOps >= p.prevDiskReadOps {
				m.DiskReadIOPS = uint64(float64(totalReadOps-p.prevDiskReadOps) / dt)
			}
			if totalWriteOps >= p.prevDiskWriteOps {
				m.DiskWriteIOPS = uint64(float64(totalWriteOps-p.prevDiskWriteOps) / dt)
			}
		}
	}
	p.prevDiskTime = now
	p.prevDiskRead = readBytes
	p.prevDiskWrite = writeBytes
	p.prevDiskReadOps = totalReadOps
	p.prevDiskWriteOps = totalWriteOps
	return nil
}

func (p *LinuxProcReader) readNet(m *HostMetrics) error {
	path := filepath.Join(p.procRoot, "net", "dev")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	now := time.Now()
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var totalRx, totalTx uint64
	var totalRxErrors, totalTxErrors uint64

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.Split(line, ":")
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" || strings.HasPrefix(iface, "veth") || strings.HasPrefix(iface, "docker") || strings.HasPrefix(iface, "br-") || strings.HasPrefix(iface, "flannel") || strings.HasPrefix(iface, "cni") {
			continue
		}

		fields := strings.Fields(parts[1])
		if len(fields) < 16 {
			continue
		}
		rxBytes, _ := strconv.ParseUint(fields[0], 10, 64)
		rxErrors, _ := strconv.ParseUint(fields[2], 10, 64)
		txBytes, _ := strconv.ParseUint(fields[8], 10, 64)
		txErrors, _ := strconv.ParseUint(fields[10], 10, 64)

		totalRx += rxBytes
		totalTx += txBytes
		totalRxErrors += rxErrors
		totalTxErrors += txErrors
	}

	m.NetRxErrors = totalRxErrors
	m.NetTxErrors = totalTxErrors

	if !p.prevNetTime.IsZero() {
		dt := now.Sub(p.prevNetTime).Seconds()
		if dt > 0 {
			if totalRx >= p.prevNetRx {
				m.NetRxBps = uint64(float64(totalRx-p.prevNetRx) / dt)
			}
			if totalTx >= p.prevNetTx {
				m.NetTxBps = uint64(float64(totalTx-p.prevNetTx) / dt)
			}
		}
	}
	p.prevNetTime = now
	p.prevNetRx = totalRx
	p.prevNetTx = totalTx
	return nil
}

func (p *LinuxProcReader) readLoadAvg(m *HostMetrics) error {
	path := filepath.Join(p.procRoot, "loadavg")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 3 {
		m.LoadAvg1m, _ = strconv.ParseFloat(fields[0], 64)
		m.LoadAvg5m, _ = strconv.ParseFloat(fields[1], 64)
		m.LoadAvg15m, _ = strconv.ParseFloat(fields[2], 64)
	}
	return nil
}

func (p *LinuxProcReader) readUptime(m *HostMetrics) error {
	path := filepath.Join(p.procRoot, "uptime")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fields := strings.Fields(string(data))
	if len(fields) >= 1 {
		uptimeFloat, err := strconv.ParseFloat(fields[0], 64)
		if err == nil && uptimeFloat >= 0 {
			m.UptimeSeconds = uint64(uptimeFloat)
		}
	}
	return nil
}
