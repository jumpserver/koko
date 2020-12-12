package service

import (
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/state"
)

type StatOption func(states map[string]interface{})

func SessionActiveCount(count int) StatOption {
	return func(states map[string]interface{}) {
		states[model.StateKeyActiveSessions] = count
	}
}

func ReportStat(opts ...StatOption) {
	stateData := make(map[string]interface{})
	if cpuLoad := state.CpuLoad1Usage(); cpuLoad > 0 {
		stateData[model.StateKeyCpuLoad1] = cpuLoad
	}
	if diskUsagePercent := state.DiskUsagePercent(); diskUsagePercent > 0 {
		stateData[model.StateKeyDiskUsed] = diskUsagePercent
	}
	if memUsagePercent := state.MemoryUsagePercent(); memUsagePercent > 0 {
		stateData[model.StateKeyMemoryUsed] = memUsagePercent
	}
	for _, setter := range opts {
		setter(stateData)
	}
	var resp interface{}
	_, err := authClient.Post(StatURL, stateData, &resp)
	if err != nil {
		logger.Errorf("Report stat err: %s", err)
		return
	}
	logger.Debugf("Report stat success %v",resp)
}
