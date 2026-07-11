package monitoring

import (
	"bufio"
	"errors"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/proxy"
)

type GoMemory struct {
	HeapAllocBytes   uint64 `json:"heap_alloc_bytes"`
	HeapInuseBytes   uint64 `json:"heap_inuse_bytes"`
	HeapSysBytes     uint64 `json:"heap_sys_bytes"`
	StackInuseBytes  uint64 `json:"stack_inuse_bytes"`
	SysBytes         uint64 `json:"sys_bytes"`
	NextGCBytes      uint64 `json:"next_gc_bytes"`
	MemoryLimitBytes int64  `json:"memory_limit_bytes"`
	NumGC            uint32 `json:"num_gc"`
	Goroutines       int    `json:"goroutines"`
	CgoCalls         int64  `json:"cgo_calls"`
}

type ProcessMemory struct {
	RSSBytes     uint64 `json:"rss_bytes"`
	PeakRSSBytes uint64 `json:"peak_rss_bytes"`
}

type CgroupMemory struct {
	Available    bool   `json:"available"`
	Version      int    `json:"version,omitempty"`
	CurrentBytes uint64 `json:"current_bytes,omitempty"`
	LimitBytes   uint64 `json:"limit_bytes,omitempty"`
	Unlimited    bool   `json:"unlimited,omitempty"`
}

type MemorySnapshot struct {
	Timestamp time.Time                   `json:"timestamp"`
	Go        GoMemory                    `json:"go"`
	Process   ProcessMemory               `json:"process"`
	Cgroup    CgroupMemory                `json:"cgroup"`
	Terminal  proxy.TerminalParserMetrics `json:"terminal_parser"`
}

func Snapshot() MemorySnapshot {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return MemorySnapshot{
		Timestamp: time.Now().UTC(),
		Go: GoMemory{
			HeapAllocBytes:   stats.HeapAlloc,
			HeapInuseBytes:   stats.HeapInuse,
			HeapSysBytes:     stats.HeapSys,
			StackInuseBytes:  stats.StackInuse,
			SysBytes:         stats.Sys,
			NextGCBytes:      stats.NextGC,
			MemoryLimitBytes: debug.SetMemoryLimit(-1),
			NumGC:            stats.NumGC,
			Goroutines:       runtime.NumGoroutine(),
			CgoCalls:         runtime.NumCgoCall(),
		},
		Process:  readProcessMemory(),
		Cgroup:   readCgroupMemory(),
		Terminal: proxy.GetTerminalParserMetrics(),
	}
}

func readProcessMemory() ProcessMemory {
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return ProcessMemory{}
	}
	defer file.Close()

	var result ProcessMemory
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSuffix(fields[0], ":") {
		case "VmRSS":
			result.RSSBytes = value * 1024
		case "VmHWM":
			result.PeakRSSBytes = value * 1024
		}
	}
	return result
}

func readCgroupMemory() CgroupMemory {
	if current, err := readUintFile("/sys/fs/cgroup/memory.current"); err == nil {
		result := CgroupMemory{Available: true, Version: 2, CurrentBytes: current}
		limit, unlimited, err := readLimitFile("/sys/fs/cgroup/memory.max")
		if err == nil {
			result.LimitBytes, result.Unlimited = limit, unlimited
		}
		return result
	}
	if current, err := readUintFile("/sys/fs/cgroup/memory/memory.usage_in_bytes"); err == nil {
		result := CgroupMemory{Available: true, Version: 1, CurrentBytes: current}
		limit, unlimited, err := readLimitFile("/sys/fs/cgroup/memory/memory.limit_in_bytes")
		if err == nil {
			result.LimitBytes, result.Unlimited = limit, unlimited || limit >= 1<<60
		}
		return result
	}
	return CgroupMemory{}
}

func readUintFile(path string) (uint64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}

func readLimitFile(path string) (uint64, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false, err
	}
	value := strings.TrimSpace(string(data))
	if value == "max" {
		return 0, true, nil
	}
	limit, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, false, errors.New("invalid cgroup memory limit")
	}
	return limit, false, nil
}
