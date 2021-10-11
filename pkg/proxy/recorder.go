package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	storage "github.com/jumpserver/koko/pkg/proxy/recorderstorage"

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

type ReplyRecorder struct {
	SessionID string

	absFilePath   string
	AbsGzFilePath string
	Target        string
	file          *os.File
	timeStartNano int64

	storage ReplayStorage

	once       sync.Once
	jmsService *service.JMService
}

func (r *ReplyRecorder) initial() {
	r.prepare()
}

func (r *ReplyRecorder) Record(b []byte) {
	if r.isNullStorage() {
		return
	}
	if len(b) > 0 {

		r.once.Do(func() {
			_, _ = r.file.Write([]byte("{"))
		})
		delta := float64(time.Now().UnixNano()-r.timeStartNano) / 1000 / 1000 / 1000
		data, _ := json.Marshal(string(b))
		_, err := r.file.WriteString(fmt.Sprintf(`"%f":%s,`, delta, data))
		if err != nil {
			logger.Errorf("Session %s write replay to file failed: %s", r.SessionID, err)
		}
	}
}

func (r *ReplyRecorder) prepare() {
	sessionID := r.SessionID
	rootPath := config.GetConf().RootPath
	today := time.Now().UTC().Format("2006-01-02")
	gzFileName := sessionID + ".replay.gz"
	replayDir := filepath.Join(rootPath, "data", "replays", today)

	r.absFilePath = filepath.Join(replayDir, sessionID)
	r.AbsGzFilePath = filepath.Join(replayDir, gzFileName)
	r.Target = strings.Join([]string{today, gzFileName}, "/")
	r.timeStartNano = time.Now().UnixNano()

	logger.Infof("Session %s storage type is %s", r.SessionID, r.storage.TypeName())
	if r.isNullStorage() {
		return
	}

	err := common.EnsureDirExist(replayDir)
	if err != nil {
		logger.Errorf("Create dir %s error: %s\n", replayDir, err)
		return
	}

	logger.Infof("Session %s: Replay file path: %s", r.SessionID, r.absFilePath)
	r.file, err = os.Create(r.absFilePath)
	if err != nil {
		logger.Errorf("Create file %s error: %s\n", r.absFilePath, err)
	}
}

func (r *ReplyRecorder) End() {
	if r.isNullStorage() {
		return
	}
	delta := float64(time.Now().UnixNano()-r.timeStartNano) / 1000 / 1000 / 1000
	_, _ = r.file.WriteString(fmt.Sprintf(`"%f":"","%f":""}`, delta, 0.0))
	_ = r.file.Close()
	go r.uploadReplay()
}

func (r *ReplyRecorder) isNullStorage() bool {
	return r.storage.TypeName() == "null"
}

func (r *ReplyRecorder) uploadReplay() {
	logger.Infof("Session %s: Replay recorder is uploading", r.SessionID)
	defer logger.Infof("Session %s: Replay recorder has uploaded", r.SessionID)
	if !common.FileExists(r.absFilePath) {
		logger.Debug("Replay file not found, passed: ", r.absFilePath)
		return
	}
	if stat, err := os.Stat(r.absFilePath); err == nil && stat.Size() == 0 {
		logger.Debug("Replay file is empty, removed: ", r.absFilePath)
		_ = os.Remove(r.absFilePath)
		return
	}
	if !common.FileExists(r.AbsGzFilePath) {
		logger.Debug("Compress replay file: ", r.absFilePath)
		_ = common.GzipCompressFile(r.absFilePath, r.AbsGzFilePath)
		_ = os.Remove(r.absFilePath)
	}
	r.UploadGzipFile(3)

}

func (r *ReplyRecorder) UploadGzipFile(maxRetry int) {
	if r.storage.TypeName() == "null" {
		_ = r.storage.Upload(r.AbsGzFilePath, r.Target)
		_ = os.Remove(r.AbsGzFilePath)
		return
	}
	for i := 0; i <= maxRetry; i++ {
		logger.Infof("Upload replay file: %s, type: %s", r.AbsGzFilePath, r.storage.TypeName())
		err := r.storage.Upload(r.AbsGzFilePath, r.Target)
		if err == nil {
			_ = os.Remove(r.AbsGzFilePath)
			if err = r.jmsService.FinishReply(r.SessionID); err != nil {
				logger.Errorf("Session[%s] finish replay err: %s", r.SessionID, err)
			}
			break
		}
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
