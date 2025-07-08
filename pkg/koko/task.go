package koko

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/session"
)

// uploadRemainReplay 上传遗留的录像
func uploadRemainReplay(jmsService *service.JMService) {
	replayDir := config.GetConf().ReplayFolderPath
	conf, err := jmsService.GetTerminalConfig()
	if err != nil {
		logger.Error(err)
		return
	}
	replayStorage := proxy.NewReplayStorage(jmsService, &conf)
	allRemainFiles := make(map[string]RemainReplay)
	_ = filepath.Walk(replayDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if replayInfo, ok := parseReplayFilename(info.Name()); ok {
			finishedTime := common.NewUTCTime(info.ModTime())
			if _, err2 := jmsService.SessionFinished(replayInfo.Id, finishedTime); err2 != nil {
				logger.Error(err2)
				return nil
			}
			allRemainFiles[path] = replayInfo
		}
		return nil
	})

	recordLifecycleLog := func(id string, event model.LifecycleEvent, reason string) {
		logObj := model.SessionLifecycleLog{Reason: reason}
		if err1 := jmsService.RecordSessionLifecycleLog(id, event, logObj); err1 != nil {
			logger.Errorf("Update session %s activity log failed: %s", id, err1)
		}
	}
	if len(allRemainFiles) == 0 {
		logger.Info("No remain replay file to upload")
		return
	}

	logger.Infof("Start upload remain %d replay files 10 min later ", len(allRemainFiles))
	time.Sleep(10 * time.Minute)

	for absPath, remainReplay := range allRemainFiles {
		absGzPath := absPath
		if !remainReplay.IsGzip {
			switch remainReplay.Version {
			case model.Version2:
				if err := ValidateRemainReplayFile(absPath); err != nil {
					continue
				}
				absGzPath = absPath + model.SuffixReplayGz
			case model.Version3:
				absGzPath = absPath + model.SuffixGz
			default:
				absGzPath = absPath + model.SuffixGz
			}

			if err = common.CompressToGzipFile(absPath, absGzPath); err != nil {
				logger.Error(err)
				continue
			}
			_ = os.Remove(absPath)
		}
		absFileInfo, err := os.Stat(absGzPath)
		if err != nil {
			logger.Errorf("Session %s: Replay file %s stat error: %s", remainReplay.Id, absGzPath, err)
			continue
		}
		target, _ := filepath.Rel(replayDir, absGzPath)

		recordLifecycleLog(remainReplay.Id, model.ReplayUploadStart, "")
		logger.Infof("Upload replay file: %s, type: %s", absGzPath, replayStorage.TypeName())
		if err2 := replayStorage.Upload(absGzPath, target); err2 != nil {
			logger.Errorf("Upload remain replay file %s failed: %s", absGzPath, err2)
			reason := model.SessionReplayErrUploadFailed
			if _, err3 := jmsService.SessionReplayFailed(remainReplay.Id, reason); err3 != nil {
				logger.Errorf("Update session %s status %s failed: %s", remainReplay.Id, reason, err3)
			}
			failureMsg := strings.ReplaceAll(err2.Error(), ",", " ")
			recordLifecycleLog(remainReplay.Id, model.ReplayUploadFailure, failureMsg)
			continue
		}
		replaySize := absFileInfo.Size()
		recordLifecycleLog(remainReplay.Id, model.ReplayUploadSuccess, "")
		if _, err1 := jmsService.FinishReplyWithSize(remainReplay.Id, replaySize); err1 != nil {
			logger.Errorf("Notify session %s upload failed: %s", remainReplay.Id, err1)
			continue
		}
		_ = os.Remove(absGzPath)
		logger.Infof("Upload remain replay file %s success", absGzPath)
	}
	logger.Info("Upload remain replay done")
}

// uploadRemainFTPFile 上传遗留的上传下载文件
func uploadRemainFTPFile(jmsService *service.JMService) {
	ftpFileDir := config.GetConf().FTPFileFolderPath
	conf, err := jmsService.GetTerminalConfig()
	if err != nil {
		logger.Error(err)
		return
	}
	ftpFileStorage := proxy.NewFTPFileStorage(jmsService, &conf)
	allRemainFiles := make(map[string]RemainFTPFile)
	_ = filepath.Walk(ftpFileDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if ftpFileInfo, ok := parseFTPFilename(info.Name()); ok {
			allRemainFiles[path] = ftpFileInfo
		}
		return nil
	})
	if len(allRemainFiles) == 0 {
		logger.Info("No remain ftp file to upload")
		return
	}
	logger.Infof("Start upload remain %d ftp files 10 min later ", len(allRemainFiles))
	time.Sleep(10 * time.Minute)

	for absPath, remainFTPFile := range allRemainFiles {
		absGzPath := absPath
		dateTarget, _ := filepath.Rel(ftpFileDir, absGzPath)
		targetName := strings.Join([]string{proxy.FtpTargetPrefix, dateTarget}, "/")
		logger.Infof("Upload FTP file: %s, target: %s, type: %s", absGzPath,
			targetName, ftpFileStorage.TypeName())
		if err = ftpFileStorage.Upload(absGzPath, targetName); err != nil {
			logger.Errorf("Upload remain FTP file %s failed: %s", absGzPath, err)
			continue
		}
		if err := jmsService.FinishFTPFile(remainFTPFile.Id); err != nil {
			logger.Errorf("Notify FTP file %s upload failed: %s", remainFTPFile.Id, err)
			continue
		}
		_ = os.Remove(absGzPath)
		logger.Infof("Upload remain FTP file %s success", absGzPath)
	}
	logger.Info("Upload remain FTP file done")
}

// keepHeartbeat 保持心跳
func keepHeartbeat(jmsService *service.JMService) {
	KeepWsHeartbeat(jmsService)
}

func handleTerminalTask(jmsService *service.JMService, tasks []model.TerminalTask) {
	for _, task := range tasks {
		sess, ok := session.GetSessionById(task.Args)
		if !ok {
			logger.Infof("Task %s session %s not found", task.ID, task.Args)
			continue
		}
		logger.Infof("Handle task %s for session %s", task.Name, task.Args)
		if err := sess.HandleTask(&task); err != nil {
			logger.Errorf("Handle task %s failed: %s", task.Name, err)
			continue
		}
		if err := jmsService.FinishTask(task.ID); err != nil {
			logger.Errorf("Finish task %s failed: %s", task.ID, err)
			continue
		}
		logger.Infof("Handle task %s for session %s success", task.Name, task.Args)

	}
}

func KeepWsHeartbeat(jmsService *service.JMService) {
	ws, err := jmsService.GetWsClient()
	if err != nil {
		logger.Errorf("Start ws client failed: %s", err)
		time.Sleep(10 * time.Second)
		go KeepWsHeartbeat(jmsService)
		return
	}
	logger.Info("Start ws client success")
	done := make(chan struct{}, 2)
	go func() {
		defer close(done)
		for {
			msgType, message, err2 := ws.ReadMessage()
			if err2 != nil {
				logger.Errorf("Ws client read err: %s", err2)
				return
			}
			switch msgType {
			case websocket.PingMessage,
				websocket.PongMessage:
				logger.Debug("Ws client ping/pong Message")
				continue
			case websocket.CloseMessage:
				logger.Debug("Ws client close Message")
				return
			}
			var tasks []model.TerminalTask
			if err = json.Unmarshal(message, &tasks); err != nil {
				logger.Errorf("Ws client Unmarshal failed: %s", err)
				continue
			}
			if len(tasks) != 0 {
				handleTerminalTask(jmsService, tasks)
			}
		}
	}()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	if err1 := ws.WriteJSON(GetStatusData()); err1 != nil {
		logger.Errorf("Ws client send heartbeat data failed: %s", err1)
	}
	for {
		select {
		case <-done:
			logger.Info("Ws client closed")
			time.Sleep(10 * time.Second)
			go KeepWsHeartbeat(jmsService)
			return
		case <-ticker.C:
			if err1 := ws.WriteJSON(GetStatusData()); err1 != nil {
				logger.Errorf("Ws client write stat data failed: %s", err1)
				continue
			}
			logger.Debug("Ws client send heartbeat success")
		}
	}
}

func GetStatusData() interface{} {
	ids := session.GetAliveSessionIds()
	payload := model.HeartbeatData{
		SessionOnlineIds: ids,
		CpuUsed:          common.CpuLoad1Usage(),
		MemoryUsed:       common.MemoryUsagePercent(),
		DiskUsed:         common.DiskUsagePercent(),
		SessionOnline:    len(ids),
	}
	return map[string]interface{}{
		"type":    "status",
		"payload": payload,
	}
}

func ValidateRemainReplayFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, os.ModePerm)
	if err != nil {
		return err
	}
	defer f.Close()
	tmp := make([]byte, 1)
	_, err = f.Seek(-1, 2)
	if err != nil {
		return err
	}
	_, err = f.Read(tmp)
	if err != nil {
		return err
	}
	switch string(tmp) {
	case "}":
		return nil
	case ",":
		_, err = f.Write([]byte(`"0":""}`))
	default:
		_, err = f.Write([]byte(`}`))
	}
	return err
}

type RemainReplay struct {
	Id      string // session id
	IsGzip  bool
	Version model.ReplayVersion
}

type RemainFTPFile struct {
	Id string // FTP log id
}

func parseReplayFilename(filename string) (replay RemainReplay, ok bool) {
	// 未压缩的旧录像文件名格式是一个 UUID
	if len(filename) == 36 {
		replay.Id = filename
		replay.Version = model.Version2
		ok = true
		return
	}
	if replay.Id, replay.Version, ok = isReplayFile(filename); ok {
		replay.IsGzip = isGzipFile(filename)
	}
	return
}

func parseFTPFilename(filename string) (ftpFile RemainFTPFile, ok bool) {
	if len(filename) == 36 {
		ftpFile.Id = filename
		ok = true
		return
	}
	return
}

func isGzipFile(filename string) bool {
	return strings.HasSuffix(filename, model.SuffixGz)
}

func isReplayFile(filename string) (id string, version model.ReplayVersion, ok bool) {
	suffixesMap := map[string]model.ReplayVersion{
		model.SuffixCast:     model.Version3,
		model.SuffixCastGz:   model.Version3,
		model.SuffixReplayGz: model.Version2}
	for suffix := range suffixesMap {
		if strings.HasSuffix(filename, suffix) {
			sidName := strings.Split(filename, ".")[0]
			if len(sidName) == 36 {
				id = sidName
				version = suffixesMap[suffix]
				ok = true
				return
			}
		}
	}
	return
}
