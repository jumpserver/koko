package utils

import (
	"testing"
)

func TestBasic(t *testing.T) {
	t.Log("load cpu==> ",CpuLoad1Usage())
	t.Log("men==> ", MemoryUsagePercent())
	t.Log("disk==> ", DiskUsagePercent())
	t.Log("local ip ==> ", CurrentLocalIP())
}
