package httpd

import (
	"testing"
	"time"
)

func TestBuildMetricsUpdate(t *testing.T) {
	previous := parseMetricsSnapshot("cpu_user=100\ncpu_idle=900\nmem_total_kb=1000\nmem_available_kb=400\ndisk_read_sectors=10\nnetwork_rx_bytes=100\n")
	current := parseMetricsSnapshot("cpu_user=200\ncpu_idle=1000\nmem_total_kb=1000\nmem_available_kb=250\ndisk_read_sectors=14\nnetwork_rx_bytes=300\n")
	update := buildMetricsUpdate(current, &previous, 2*time.Second)
	if update.CPUPercent != 50 || update.MemoryPercent != 75 || update.DiskReadBytesPerSecond != 1024 || update.NetworkRXBytesPerSecond != 100 {
		t.Fatalf("unexpected metrics update: %+v", update)
	}
}
