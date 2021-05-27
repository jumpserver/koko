package common

import (
	"fmt"
	"os"
	"strconv"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

func CpuLoad1Usage() float64 {
	var (
		err         error
		cpuCount    int
		avgLoadStat *load.AvgStat
	)
	cpuCount, err = cpu.Counts(true)
	if err != nil {
		return -1
	}
	avgLoadStat, err = load.Avg()
	if err != nil {
		return -1
	}
	return convertFloatDecimal(avgLoadStat.Load1 / float64(cpuCount))
}

func DiskUsagePercent() float64 {
	dir, _ := os.Getwd()
	usage, err := disk.Usage(dir)
	if err != nil {
		return -1
	}
	return convertFloatDecimal(usage.UsedPercent)
}

func MemoryUsagePercent() float64 {
	vmStatus, err := mem.VirtualMemory()
	if err != nil {
		return -1
	}
	return convertFloatDecimal(vmStatus.UsedPercent)
}

func convertFloatDecimal(value float64) float64 {
	result, _ := strconv.ParseFloat(fmt.Sprintf("%.2f", value), 64)
	return result
}
