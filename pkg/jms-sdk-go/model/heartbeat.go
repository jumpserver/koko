package model

type HeartbeatData struct {
	SessionOnlineIds []string `json:"sessions"`
	SessionOnline    int      `json:"session_online"`
	CpuUsed          float64  `json:"cpu_load"`
	MemoryUsed       float64  `json:"memory_used"`
	DiskUsed         float64  `json:"disk_used"`
}
