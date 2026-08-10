package httpd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/srvconn"
)

const (
	defaultMetricsInterval = 2 * time.Second
	minMetricsInterval     = 2 * time.Second
	maxMetricsInterval     = 10 * time.Second
	metricsProbeTimeout    = 5 * time.Second
	metricsSampleMarker    = "__JMS_METRICS_SAMPLE__"
)

const metricsProbeScript = `
read _ cpu_user cpu_nice cpu_system cpu_idle cpu_iowait cpu_irq cpu_softirq cpu_steal _ < /proc/stat
echo cpu_user=$cpu_user
echo cpu_nice=$cpu_nice
echo cpu_system=$cpu_system
echo cpu_idle=$cpu_idle
echo cpu_iowait=$cpu_iowait
echo cpu_irq=$cpu_irq
echo cpu_softirq=$cpu_softirq
echo cpu_steal=$cpu_steal
while read key value _; do
  case "$key" in
    MemTotal:) echo mem_total_kb=$value ;;
    MemAvailable:) echo mem_available_kb=$value ;;
    SwapTotal:) echo swap_total_kb=$value ;;
    SwapFree:) echo swap_free_kb=$value ;;
  esac
done < /proc/meminfo
disk_read_sectors=0
disk_write_sectors=0
for stat in /sys/block/*/stat; do
  [ -r "$stat" ] || continue
  read _ _ read_sectors _ _ _ write_sectors _ < "$stat"
  disk_read_sectors=$((disk_read_sectors + read_sectors))
  disk_write_sectors=$((disk_write_sectors + write_sectors))
done
echo disk_read_sectors=$disk_read_sectors
echo disk_write_sectors=$disk_write_sectors
set -- $(df -Pk / 2>/dev/null | tail -n 1)
echo disk_total_kb=${2:-0}
echo disk_used_kb=${3:-0}
network_rx_bytes=0
network_tx_bytes=0
while read iface rx _ _ _ _ _ _ _ tx _; do
  iface=${iface%:}
  [ "$iface" = lo ] && continue
  case "$rx:$tx" in *[!0-9:]*) continue ;; esac
  network_rx_bytes=$((network_rx_bytes + rx))
  network_tx_bytes=$((network_tx_bytes + tx))
done < /proc/net/dev
echo network_rx_bytes=$network_rx_bytes
echo network_tx_bytes=$network_tx_bytes
read uptime_seconds _ < /proc/uptime
echo uptime_seconds=$uptime_seconds
echo hostname=$(uname -n 2>/dev/null)
echo kernel=$(uname -sr 2>/dev/null)
echo architecture=$(uname -m 2>/dev/null)
echo cpu_cores=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 0)
`

type clientMetrics struct {
	mu         sync.Mutex
	sshClient  *srvconn.SSHClient
	guard      func() error
	enabled    bool
	requested  bool
	interval   time.Duration
	cancel     context.CancelFunc
	previous   *metricsSnapshot
	previousAt time.Time
}

type metricsSnapshot struct {
	CPUUser, CPUNice, CPUSystem, CPUIdle    uint64
	CPUIowait, CPUIrq, CPUSoftirq, CPUSteal uint64
	MemTotalKB, MemAvailableKB              uint64
	SwapTotalKB, SwapFreeKB                 uint64
	DiskReadSectors, DiskWriteSectors       uint64
	DiskTotalKB, DiskUsedKB                 uint64
	NetworkRXBytes, NetworkTXBytes          uint64
	UptimeSeconds                           float64
	Hostname, Kernel, Architecture          string
	CPUCores                                uint64
}

type metricsUpdate struct {
	Timestamp               int64   `json:"timestamp"`
	Hostname                string  `json:"hostname,omitempty"`
	Kernel                  string  `json:"kernel,omitempty"`
	Architecture            string  `json:"architecture,omitempty"`
	CPUCores                uint64  `json:"cpuCores,omitempty"`
	UptimeSeconds           float64 `json:"uptimeSeconds"`
	CPUPercent              float64 `json:"cpuPercent"`
	MemoryUsedBytes         uint64  `json:"memoryUsedBytes"`
	MemoryTotalBytes        uint64  `json:"memoryTotalBytes"`
	MemoryPercent           float64 `json:"memoryPercent"`
	SwapUsedBytes           uint64  `json:"swapUsedBytes"`
	SwapTotalBytes          uint64  `json:"swapTotalBytes"`
	DiskUsedBytes           uint64  `json:"diskUsedBytes"`
	DiskTotalBytes          uint64  `json:"diskTotalBytes"`
	DiskPercent             float64 `json:"diskPercent"`
	DiskReadBytesPerSecond  float64 `json:"diskReadBytesPerSecond"`
	DiskWriteBytesPerSecond float64 `json:"diskWriteBytesPerSecond"`
	NetworkRXBytesPerSecond float64 `json:"networkRxBytesPerSecond"`
	NetworkTXBytesPerSecond float64 `json:"networkTxBytesPerSecond"`
}

func (c *Client) configureMetrics(enabled bool, guard func() error) {
	c.metrics.mu.Lock()
	c.metrics.enabled = enabled
	c.metrics.guard = guard
	c.metrics.mu.Unlock()
}

func (c *Client) setMetricsSSHClient(sshClient *srvconn.SSHClient) {
	c.metrics.mu.Lock()
	c.metrics.sshClient = sshClient
	requested := c.metrics.requested
	interval := c.metrics.interval
	c.metrics.mu.Unlock()
	if requested {
		c.startMetricsDuration(interval)
	}
}

func (c *Client) startMetrics(intervalSeconds int) {
	interval := time.Duration(intervalSeconds) * time.Second
	if interval < minMetricsInterval || interval > maxMetricsInterval {
		interval = defaultMetricsInterval
	}
	c.metrics.mu.Lock()
	c.metrics.requested = true
	c.metrics.interval = interval
	enabled := c.metrics.enabled
	ready := c.metrics.sshClient != nil
	c.metrics.mu.Unlock()
	if !enabled {
		c.sendMetricsStatus("unavailable", "Linux metrics are unavailable for this connection")
		return
	}
	if ready {
		c.startMetricsDuration(interval)
	}
}

func (c *Client) startMetricsDuration(interval time.Duration) {
	c.metrics.mu.Lock()
	if c.metrics.cancel != nil || !c.metrics.enabled || !c.metrics.requested || c.metrics.sshClient == nil {
		c.metrics.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.metrics.cancel = cancel
	c.metrics.previous = nil
	c.metrics.previousAt = time.Time{}
	c.metrics.mu.Unlock()
	c.sendMetricsStatus("collecting", "")
	go c.collectMetrics(ctx, interval)
}

func (c *Client) unsubscribeMetrics() {
	c.metrics.mu.Lock()
	c.metrics.requested = false
	c.stopMetricsLocked()
	c.metrics.mu.Unlock()
}

func (c *Client) stopMetrics() {
	c.metrics.mu.Lock()
	c.metrics.requested = false
	c.stopMetricsLocked()
	c.metrics.mu.Unlock()
}

func (c *Client) stopMetricsLocked() {
	if c.metrics.cancel != nil {
		c.metrics.cancel()
		c.metrics.cancel = nil
	}
	c.metrics.previous = nil
	c.metrics.previousAt = time.Time{}
}

func (c *Client) collectMetrics(ctx context.Context, interval time.Duration) {
	if err := c.streamMetrics(ctx, interval); err != nil && !errors.Is(err, context.Canceled) {
		c.sendMetricsStatus("unavailable", err.Error())
	}
	c.metrics.mu.Lock()
	c.stopMetricsLocked()
	c.metrics.mu.Unlock()
}

func (c *Client) streamMetrics(ctx context.Context, interval time.Duration) error {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	c.metrics.mu.Lock()
	sshClient := c.metrics.sshClient
	guard := c.metrics.guard
	c.metrics.mu.Unlock()
	if sshClient == nil {
		return errors.New("SSH metrics channel is unavailable")
	}
	if guard != nil {
		if err := guard(); err != nil {
			return err
		}
	}
	session, err := sshClient.AcquireSession()
	if err != nil {
		return err
	}
	defer func() {
		_ = session.Close()
		sshClient.ReleaseSession(session)
	}()
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return err
	}
	command := fmt.Sprintf(
		"LC_ALL=C sh -c 'while :; do\n%s\necho %s\nsleep %d\ndone'",
		metricsProbeScript,
		metricsSampleMarker,
		int(interval/time.Second),
	)
	if err = session.Start(command); err != nil {
		return err
	}
	go func() { _, _ = io.Copy(io.Discard, stderr) }()

	samples := make(chan metricsSnapshot, 1)
	done := make(chan error, 1)
	go func() {
		var sample strings.Builder
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if line != metricsSampleMarker {
				sample.WriteString(line)
				sample.WriteByte('\n')
				continue
			}
			value := parseMetricsSnapshot(sample.String())
			sample.Reset()
			select {
			case samples <- value:
			case <-streamCtx.Done():
				return
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			done <- scanErr
			return
		}
		done <- session.Wait()
	}()

	timeout := time.NewTimer(metricsProbeTimeout)
	defer timeout.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = session.Close()
			return ctx.Err()
		case err = <-done:
			if err == nil {
				return errors.New("Linux metrics stream closed")
			}
			return err
		case <-timeout.C:
			_ = session.Close()
			return errors.New("Linux metrics probe timed out")
		case snapshot := <-samples:
			if snapshot.MemTotalKB == 0 {
				return errors.New("remote host did not return Linux metrics")
			}
			if guard != nil {
				if guardErr := guard(); guardErr != nil {
					return guardErr
				}
			}
			now := time.Now()
			c.metrics.mu.Lock()
			update := buildMetricsUpdate(snapshot, c.metrics.previous, now.Sub(c.metrics.previousAt))
			c.metrics.previous = &snapshot
			c.metrics.previousAt = now
			c.metrics.mu.Unlock()
			c.sendMetricsUpdate(update)
			if !timeout.Stop() {
				select {
				case <-timeout.C:
				default:
				}
			}
			timeout.Reset(interval + metricsProbeTimeout)
		}
	}
}

func parseMetricsSnapshot(output string) metricsSnapshot {
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if ok && key != "" {
			values[key] = strings.TrimSpace(value)
		}
	}
	uintValue := func(key string) uint64 {
		value, _ := strconv.ParseUint(values[key], 10, 64)
		return value
	}
	floatValue := func(key string) float64 {
		value, _ := strconv.ParseFloat(values[key], 64)
		return value
	}
	return metricsSnapshot{
		CPUUser: uintValue("cpu_user"), CPUNice: uintValue("cpu_nice"),
		CPUSystem: uintValue("cpu_system"), CPUIdle: uintValue("cpu_idle"),
		CPUIowait: uintValue("cpu_iowait"), CPUIrq: uintValue("cpu_irq"),
		CPUSoftirq: uintValue("cpu_softirq"), CPUSteal: uintValue("cpu_steal"),
		MemTotalKB: uintValue("mem_total_kb"), MemAvailableKB: uintValue("mem_available_kb"),
		SwapTotalKB: uintValue("swap_total_kb"), SwapFreeKB: uintValue("swap_free_kb"),
		DiskReadSectors: uintValue("disk_read_sectors"), DiskWriteSectors: uintValue("disk_write_sectors"),
		DiskTotalKB: uintValue("disk_total_kb"), DiskUsedKB: uintValue("disk_used_kb"),
		NetworkRXBytes: uintValue("network_rx_bytes"), NetworkTXBytes: uintValue("network_tx_bytes"),
		UptimeSeconds: floatValue("uptime_seconds"), Hostname: values["hostname"],
		Kernel: values["kernel"], Architecture: values["architecture"], CPUCores: uintValue("cpu_cores"),
	}
}

func buildMetricsUpdate(current metricsSnapshot, previous *metricsSnapshot, elapsed time.Duration) metricsUpdate {
	update := metricsUpdate{
		Timestamp: time.Now().UnixMilli(), Hostname: current.Hostname, Kernel: current.Kernel,
		Architecture: current.Architecture, CPUCores: current.CPUCores, UptimeSeconds: current.UptimeSeconds,
		MemoryUsedBytes:  (current.MemTotalKB - minUint64(current.MemTotalKB, current.MemAvailableKB)) * 1024,
		MemoryTotalBytes: current.MemTotalKB * 1024,
		SwapUsedBytes:    (current.SwapTotalKB - minUint64(current.SwapTotalKB, current.SwapFreeKB)) * 1024,
		SwapTotalBytes:   current.SwapTotalKB * 1024,
		DiskUsedBytes:    current.DiskUsedKB * 1024, DiskTotalBytes: current.DiskTotalKB * 1024,
	}
	update.MemoryPercent = percent(update.MemoryUsedBytes, update.MemoryTotalBytes)
	update.DiskPercent = percent(update.DiskUsedBytes, update.DiskTotalBytes)
	if previous == nil || elapsed <= 0 {
		return update
	}
	currentIdle := current.CPUIdle + current.CPUIowait
	previousIdle := previous.CPUIdle + previous.CPUIowait
	currentTotal := current.CPUUser + current.CPUNice + current.CPUSystem + currentIdle + current.CPUIrq + current.CPUSoftirq + current.CPUSteal
	previousTotal := previous.CPUUser + previous.CPUNice + previous.CPUSystem + previousIdle + previous.CPUIrq + previous.CPUSoftirq + previous.CPUSteal
	if totalDelta := counterDelta(currentTotal, previousTotal); totalDelta > 0 {
		update.CPUPercent = float64(totalDelta-counterDelta(currentIdle, previousIdle)) * 100 / float64(totalDelta)
	}
	seconds := elapsed.Seconds()
	update.DiskReadBytesPerSecond = float64(counterDelta(current.DiskReadSectors, previous.DiskReadSectors)*512) / seconds
	update.DiskWriteBytesPerSecond = float64(counterDelta(current.DiskWriteSectors, previous.DiskWriteSectors)*512) / seconds
	update.NetworkRXBytesPerSecond = float64(counterDelta(current.NetworkRXBytes, previous.NetworkRXBytes)) / seconds
	update.NetworkTXBytesPerSecond = float64(counterDelta(current.NetworkTXBytes, previous.NetworkTXBytes)) / seconds
	return update
}

func counterDelta(current, previous uint64) uint64 {
	if current < previous {
		return 0
	}
	return current - previous
}

func minUint64(left, right uint64) uint64 {
	if left < right {
		return left
	}
	return right
}

func percent(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) * 100 / float64(total)
}

func (c *Client) sendMetricsUpdate(update metricsUpdate) {
	data, _ := json.Marshal(update)
	c.Conn.SendMessage(&Message{Type: TerminalMetricsUpdate, TerminalId: c.TerminalId, Data: string(data)})
}

func (c *Client) sendMetricsStatus(status, message string) {
	data, _ := json.Marshal(map[string]string{"status": status, "message": message})
	c.Conn.SendMessage(&Message{Type: TerminalMetricsStatus, TerminalId: c.TerminalId, Data: string(data)})
}
