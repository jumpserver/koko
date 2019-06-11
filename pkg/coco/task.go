package coco

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"koko/pkg/common"
	"koko/pkg/config"
	"koko/pkg/logger"
	"koko/pkg/proxy"
	"koko/pkg/service"
)

func Initial() {
	conf := config.GetConf()
	if conf.UploadFailedReplay {
		go uploadRemainReplay(conf.RootPath)
	}

	go keepHeartbeat(conf.HeartbeatDuration)
}

// uploadRemainReplay 上传遗留的录像
func uploadRemainReplay(rootPath string) {
	replayDir := filepath.Join(rootPath, "data", "replays")
	err := common.EnsureDirExist(replayDir)
	if err != nil {
		logger.Debugf("upload failed replay err: %s", err.Error())
		return
	}

	_ = filepath.Walk(replayDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		filename := info.Name()
		if strings.HasSuffix(filename, ".replay.gz") {
			sid := strings.Split(filename, ".")[0]
			if len(sid) == 36 {
				relayRecord := proxy.NewReplyRecord(sid)
				relayRecord.AbsGzFilePath = path
				relayRecord.Target, _ = filepath.Rel(path, rootPath)
				go relayRecord.UploadGzipFile(3)
			}
		}
		return nil
	})
	logger.Debug("Upload remain replay done")
}

// keepHeartbeat 保持心跳
func keepHeartbeat(interval time.Duration) {
	tick := time.Tick(interval * time.Second)
	for {
		select {
		case <-tick:
			data := proxy.GetAliveSessions()
			tasks := service.TerminalHeartBeat(data)
			if len(tasks) != 0 {
				for _, task := range tasks {
					proxy.HandleSessionTask(task)
				}
			}
		}
	}
}
