package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	storage "github.com/jumpserver/koko/pkg/proxy/recorderstorage"

	"github.com/jumpserver/koko/pkg/asciinema"
	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/model"
	"github.com/jumpserver/koko/pkg/jms-sdk-go/service"
	"github.com/jumpserver/koko/pkg/logger"
)

type CommandRecorder struct {
	sessionID string
	storage   CommandStorage

	queue  chan *model.Command
	closed chan struct{}

	jmsService *service.JMService
}

func (c *CommandRecorder) Record(command *model.Command) {
	c.queue <- command
}

func (c *CommandRecorder) End() {
	select {
	case <-c.closed:
		return
	default:
	}
	close(c.closed)
}

func (c *CommandRecorder) record() {
	cmdList := make([]*model.Command, 0, 10)
	notificationList := make([]*model.Command, 0, 10)
	maxRetry := 0
	logger.Infof("Session %s: Command recorder start", c.sessionID)
	defer logger.Infof("Session %s: Command recorder close", c.sessionID)
	tick := time.NewTicker(time.Second * 10)
	defer tick.Stop()
	for {
		select {
		case <-c.closed:
			if len(cmdList) == 0 {
				return
			}
		case p, ok := <-c.queue:
			if !ok {
				return
			}
			if p.RiskLevel == model.DangerLevel {
				notificationList = append(notificationList, p)
			}
			cmdList = append(cmdList, p)
			if len(cmdList) < 5 {
				continue
			}
		case <-tick.C:
			if len(cmdList) == 0 {
				continue
			}
		}
		if len(notificationList) > 0 {
			if err := c.jmsService.NotifyCommand(notificationList); err == nil {
				notificationList = notificationList[:0]
			} else {
				logger.Errorf("Session %s: command notify err: %s", c.sessionID, err)
			}
		}
		err := c.storage.BulkSave(cmdList)
		if err == nil {
			cmdList = cmdList[:0]
			maxRetry = 0
			continue
		}
		if err != nil {
			logger.Errorf("Session %s: command bulk save err: %s", c.sessionID, err)
		}

		if maxRetry > 5 {
			cmdList = cmdList[1:]
		}
		maxRetry++
	}
}

/*
old file format: sessionId.replay.gz
new file format: sessionId.cast.replay.gz "application/x-asciicast"
*/

const (
	dateTimeFormat = "2006-01-02"

	replayFilenameSuffix   = ".cast"
	replayGzFilenameSuffix = ".gz"
)

func NewReplayRecord(sid string, jmsService *service.JMService,
	storage ReplayStorage, info *ReplyInfo) (*ReplyRecorder, error) {
	recorder := &ReplyRecorder{
		SessionID:  sid,
		jmsService: jmsService,
		storage:    storage,
		info:       info,
	}

	if recorder.isNullStorage() {
		return recorder, nil
	}
	today := info.TimeStamp.Format(dateTimeFormat)
	replayRootDir := config.GetConf().ReplayFolderPath
	sessionReplayDirPath := filepath.Join(replayRootDir, today)
	err := common.EnsureDirExist(sessionReplayDirPath)
	if err != nil {
		logger.Errorf("Create dir %s error: %s\n", sessionReplayDirPath, err)
		recorder.err = err
		return recorder, err
	}
	filename := sid + replayFilenameSuffix
	gzFilename := filename + replayGzFilenameSuffix
	absFilePath := filepath.Join(sessionReplayDirPath, filename)
	absGZFilePath := filepath.Join(sessionReplayDirPath, gzFilename)
	storageTargetName := strings.Join([]string{today, gzFilename}, "/")
	recorder.absGzipFilePath = absGZFilePath
	recorder.absFilePath = absFilePath
	recorder.Target = storageTargetName
	fd, err := os.Create(recorder.absFilePath)
	if err != nil {
		logger.Errorf("Create replay file %s error: %s\n", recorder.absFilePath, err)
		recorder.err = err
		return recorder, err
	}
	recorder.file = fd

	options := make([]asciinema.Option, 0, 3)
	options = append(options, asciinema.WithHeight(info.Height))
	options = append(options, asciinema.WithWidth(info.Width))
	options = append(options, asciinema.WithTimestamp(info.TimeStamp))
	recorder.Writer = asciinema.NewWriter(recorder.file, options...)
	return recorder, nil
}

type ReplyRecorder struct {
	SessionID  string
	jmsService *service.JMService
	storage    ReplayStorage
	info       *ReplyInfo

	absFilePath     string
	absGzipFilePath string
	Target          string
	Writer          *asciinema.Writer
	err             error

	file *os.File
	once sync.Once
}

func (r *ReplyRecorder) isNullStorage() bool {
	return r.storage.TypeName() == "null" || r.err != nil
}

func (r *ReplyRecorder) Record(p []byte) {
	if r.isNullStorage() {
		return
	}
	if len(p) > 0 {
		r.once.Do(func() {
			if err := r.Writer.WriteHeader(); err != nil {
				logger.Errorf("Session %s write replay header failed: %s", r.SessionID, err)
			}
		})
		if err := r.Writer.WriteRow(p); err != nil {
			logger.Errorf("Session %s write replay row failed: %s", r.SessionID, err)
		}
	}
}

func (r *ReplyRecorder) End() {
	if r.isNullStorage() {
		return
	}
	_ = r.file.Close()
	go r.uploadReplay()
}

func (r *ReplyRecorder) uploadReplay() {
	logger.Infof("Session %s: Replay recorder is uploading", r.SessionID)
	if !common.FileExists(r.absFilePath) {
		logger.Info("Replay file not found, passed: ", r.absFilePath)
		return
	}
	if stat, err := os.Stat(r.absFilePath); err == nil && stat.Size() == 0 {
		logger.Info("Replay file is empty, removed: ", r.absFilePath)
		_ = os.Remove(r.absFilePath)
		return
	}
	if !common.FileExists(r.absGzipFilePath) {
		logger.Debug("Compress replay file: ", r.absFilePath)
		_ = common.GzipCompressFile(r.absFilePath, r.absGzipFilePath)
		_ = os.Remove(r.absFilePath)
	}
	r.UploadGzipFile(3)

}

func (r *ReplyRecorder) UploadGzipFile(maxRetry int) {
	if r.isNullStorage() {
		_ = os.Remove(r.absGzipFilePath)
		return
	}
	for i := 0; i <= maxRetry; i++ {
		logger.Infof("Upload replay file: %s, type: %s", r.absGzipFilePath, r.storage.TypeName())
		err := r.storage.Upload(r.absGzipFilePath, r.Target)
		if err == nil {
			_ = os.Remove(r.absGzipFilePath)
			if err = r.jmsService.FinishReply(r.SessionID); err != nil {
				logger.Errorf("Session[%s] finish replay err: %s", r.SessionID, err)
			}
			break
		}
		logger.Errorf("Upload replay file err: %s",err)
		// 如果还是失败，上传 server 再传一次
		if i == maxRetry {
			if r.storage.TypeName() == "server" {
				break
			}
			logger.Errorf("Session[%s] using server storage retry upload", r.SessionID)
			r.storage = storage.ServerStorage{StorageType: "server", JmsService: r.jmsService}
			r.UploadGzipFile(3)
			break
		}
	}
}

type ReplyInfo struct {
	Width     int
	Height    int
	TimeStamp time.Time
}
