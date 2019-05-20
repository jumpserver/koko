package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cocogo/pkg/common"
	"cocogo/pkg/config"
	"cocogo/pkg/logger"
	"cocogo/pkg/model"
	"cocogo/pkg/service"
)

var sessionMap = make(map[string]*SwitchSession)
var lock = new(sync.RWMutex)

func Initial() {
	conf := config.GetConf()
	if conf.UploadFailedReplay {
		go uploadFailedReplay(conf.RootPath)
	}

	go KeepHeartbeat(conf.HeartbeatDuration)
}

func uploadFailedReplay(rootPath string) {
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
				relayRecord := NewReplyRecord(sid)
				go relayRecord.uploadGzipFile(3)
			}
		}
		return nil
	})
	logger.Debug("upload Failed Replay Done")
}

func KeepHeartbeat(interval int) {
	tick := time.Tick(time.Duration(interval) * time.Second)
	for {
		select {
		case <-tick:
			data := GetAliveSessions()
			tasks := service.TerminalHeartBeat(data)
			if len(tasks) != 0 {
				for _, task := range tasks {
					HandlerSessionTask(task)
				}
			}
		}

	}
}

func HandlerSessionTask(task model.TerminalTask) {
	switch task.Name {
	case "kill_session":
		KillSession(task.Args)
		service.FinishTask(task.Id)
	default:

	}

}

func KillSession(sessionID string) {
	lock.RLock()
	defer lock.RUnlock()
	if sw, ok := sessionMap[sessionID]; ok {
		sw.Terminate()
	}

}

func GetAliveSessions() []string {
	lock.RLock()
	defer lock.RUnlock()
	sids := make([]string, 0, len(sessionMap))
	for sid := range sessionMap {
		sids = append(sids, sid)
	}
	return sids
}

func RemoveSession(sw *SwitchSession) {
	lock.Lock()
	defer lock.Unlock()
	delete(sessionMap, sw.Id)
}

func AddSession(sw *SwitchSession) {
	lock.Lock()
	defer lock.Unlock()
	sessionMap[sw.Id] = sw
}
