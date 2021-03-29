package utils

import (
	"fmt"
	"net"
	"strconv"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

func CpuLoad1Usage() float64 {
	var (
		err         error
		cpuCount    int
		avgLoadStat *load.AvgStat
	)
	cpuCount, err = cpu.Counts(true)
	avgLoadStat, err = load.Avg()
	if err != nil {
		logger.Errorf("Get cpu load 1min err: %s", err)
		return -1
	}
	return convertFloatDecimal(avgLoadStat.Load1 / float64(cpuCount))
}

func DiskUsagePercent() float64 {
	rootPath := config.GetConf().RootPath
	usage, err := disk.Usage(rootPath)
	if err != nil {
		logger.Errorf("Get disk usage err: %s", err)
		return -1
	}
	return convertFloatDecimal(usage.UsedPercent)
}

func MemoryUsagePercent() float64 {
	vmStatus, err := mem.VirtualMemory()
	if err != nil {
		logger.Errorf("Get memory usage err: %s", err)
		return -1
	}
	return convertFloatDecimal(vmStatus.UsedPercent)
}

func CurrentLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		logger.Errorf("Get local IP err: %s", err)
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

func convertFloatDecimal(value float64) float64 {
	result, _ := strconv.ParseFloat(fmt.Sprintf("%.2f", value), 64)
	return result
}
