package koko

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/common"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
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
	allRemainFiles := make(map[string]RemainReplay)
	_ = filepath.Walk(replayDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if replayInfo, ok := parseReplayFilename(info.Name()); ok {
			finishedTime := common.NewUTCTime(info.ModTime())
			if err2 := jmsService.SessionFinished(replayInfo.Id, finishedTime); err2 != nil {
				logger.Error(err2)
				return nil
			}
			allRemainFiles[path] = replayInfo
		}
		return nil
	})

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
		Target, _ := filepath.Rel(replayDir, absGzPath)
		logger.Infof("Upload replay file: %s, type: %s", absGzPath, replayStorage.TypeName())
		if err2 := replayStorage.Upload(absGzPath, Target); err2 != nil {
			logger.Errorf("Upload remain replay file %s failed: %s", absGzPath, err2)
			continue
		}
		if err := jmsService.FinishReply(remainReplay.Id); err != nil {
			logger.Errorf("Notify session %s upload failed: %s", remainReplay.Id, err)
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
				var terminalFlag bool
				switch task.Name {
				case model.TaskKillSession:
					if sw, ok := proxy.GetSessionById(task.Args); ok {
						sw.Terminate(task.Kwargs.TerminatedBy)
						terminalFlag = true
					}
					if cmdCancel, ok := proxy.GetCommandSession(task.Args); ok {
						cmdCancel()
						terminalFlag = true

					}
					if !terminalFlag {
						continue
					}
					if err = jmsService.FinishTask(task.ID); err != nil {
						logger.Error(err)
					}
				default:

				}
			}
		}
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
