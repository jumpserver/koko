package koko

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/proxy"
	"github.com/jumpserver/koko/pkg/service"
)

func Initial() {
	conf := config.GetConf()
	if conf.UploadFailedReplay {
		go uploadRemainReplay(conf.RootPath)
		go uploadRemainFtpLogFile(conf.RootPath)
	}

	go keepHeartbeat()
}

// uploadRemainReplay 上传遗留的录像
func uploadRemainReplay(rootPath string) {
	replayDir := filepath.Join(rootPath, "data", "replays")
	err := common.EnsureDirExist(replayDir)
	if err != nil {
		logger.Debugf("upload failed replay err: %s", err.Error())
		return
	}
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
			data := map[string]interface{}{"id": sid, "date_end": info.ModTime().UTC().Format(
				"2006-01-02 15:04:05 +0000")}
			service.FinishSession(data)
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
			if err := common.GzipCompressFile(path, absGzPath); err != nil {
				continue
			}
			_ = os.Remove(path)
		}
		relayRecord := &proxy.ReplyRecorder{SessionID: sid}
		relayRecord.AbsGzFilePath = absGzPath
		relayRecord.Target, _ = filepath.Rel(replayDir, absGzPath)
		relayRecord.UploadGzipFile(3)
	}
	logger.Debug("Upload remain replay done")
}

// uploadRemainReplay 上传遗留的文件记录
func uploadRemainFtpLogFile(rootPath string) {
	fileStoreDir := filepath.Join(rootPath, "data", "files")
	err := common.EnsureDirExist(fileStoreDir)
	if err != nil {
		logger.Debugf("upload failed replay err: %s", err.Error())
		return
	}
	allRemainFiles := make(map[string]string)
	_ = filepath.Walk(fileStoreDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		var sid string
		filename := info.Name()
		if len(filename) == 36 {
			sid = filename
		}
		if sid != "" {
			allRemainFiles[sid] = path
		}
		return nil
	})

	for sid, path := range allRemainFiles {
		target, _ := filepath.Rel(fileStoreDir, path)
		proxy.UploadFtpLogFile(path, target, sid)
	}
	logger.Debug("Upload remain replay done")
}

// keepHeartbeat 保持心跳
func keepHeartbeat() {
	for {
		time.Sleep(30 * time.Second)
		data := proxy.GetAliveSessions()
		tasks := service.TerminalHeartBeat(data)
		if len(tasks) != 0 {
			for _, task := range tasks {
				proxy.HandleSessionTask(task)
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
