package koko

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
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
	allRemainFiles := make(map[string]string)
	_ = filepath.Walk(replayDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		var sid string
		filename := info.Name()
		if len(filename) == 36 {
			sid = filename
		}
		if strings.HasSuffix(filename, ".replay.gz") {
			sidName := strings.Split(filename, ".")[0]
			if len(sidName) == 36 {
				sid = sidName
			}
		}
		if sid != "" {
			if err2 := jmsService.SessionFinished(sid, common.NewUTCTime(info.ModTime())); err2 != nil {
				logger.Error(err2)
				return nil
			}
			allRemainFiles[sid] = path
		}

		return nil
	})

	for sid, path := range allRemainFiles {
		var absGzPath string
		if strings.HasSuffix(path, ".replay.gz") {
			absGzPath = path
		} else if strings.HasSuffix(path, sid) {
			if err := ValidateRemainReplayFile(path); err != nil {
				continue
			}
			absGzPath = path + ".replay.gz"
			if err := common.CompressToGzipFile(path, absGzPath); err != nil {
				logger.Error(err)
				continue
			}
			_ = os.Remove(path)
		}
		Target, _ := filepath.Rel(replayDir, absGzPath)
		logger.Infof("Upload replay file: %s, type: %s", absGzPath, replayStorage.TypeName())
		if err2 := replayStorage.Upload(absGzPath, Target); err2 != nil {
			logger.Errorf("Upload remain replay file %s failed: %s", absGzPath, err2)
			continue
		}
		if err := jmsService.FinishReply(sid); err != nil {
			logger.Errorf("Notify session %s upload failed: %s", sid, err)
			continue
		}
		_ = os.Remove(absGzPath)
		logger.Infof("Upload remain replay file %s success", absGzPath)
	}
	logger.Info("Upload remain replay done")
}

// keepHeartbeat 保持心跳
func keepHeartbeat(jmsService *service.JMService) {
	for {
		time.Sleep(30 * time.Second)
		data := proxy.GetAliveSessions()
		tasks, err := jmsService.TerminalHeartBeat(data)
		if err != nil {
			logger.Error(err)
			continue
		}
		if len(tasks) != 0 {
			for _, task := range tasks {
				switch task.Name {
				case TaskKillSession:
					if ok := proxy.KillSession(task.Args); ok {
						if err = jmsService.FinishTask(task.ID); err != nil {
							logger.Error(err)
						}
					}
				default:

				}
			}
		}
	}
}

const (
	TaskKillSession = "kill_session"
)

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
