package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/model"
	"github.com/jumpserver/koko/pkg/service"
)

func NewCommandRecorder(sid string) (recorder *CommandRecorder) {
	recorder = &CommandRecorder{sessionID: sid}
	recorder.initial()
	return recorder
}

func NewReplyRecord(sid string) (recorder *ReplyRecorder) {
	recorder = &ReplyRecorder{SessionID: sid}
	recorder.initial()
	return recorder
}

type CommandRecorder struct {
	sessionID string
	storage   CommandStorage

	queue  chan *model.Command
	closed chan struct{}
}

func (c *CommandRecorder) initial() {
	c.queue = make(chan *model.Command, 10)
	c.storage = NewCommandStorage()
	c.closed = make(chan struct{})
	go c.record()
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
	maxRetry := 0
	logger.Infof("Session %s: Command recorder start", c.sessionID)
	defer logger.Infof("Session %s: Command recorder close", c.sessionID)
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
			cmdList = append(cmdList, p)
			if len(cmdList) < 5 {
				continue
			}
		case <-time.After(time.Second * 5):
			if len(cmdList) == 0 {
				continue
			}
		}
		err := c.storage.BulkSave(cmdList)
		if err == nil {
			cmdList = cmdList[:0]
			maxRetry = 0
			continue
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

	storage        ReplayStorage
	backOffStorage ReplayStorage
}

func (r *ReplyRecorder) initial() {
	r.prepare()
}

func (r *ReplyRecorder) Record(b []byte) {
	if len(b) > 0 {
		delta := float64(time.Now().UnixNano()-r.timeStartNano) / 1000 / 1000 / 1000
		data, _ := json.Marshal(string(b))
		_, _ = r.file.WriteString(fmt.Sprintf(`"%f":%s,`, delta, data))
		_ = r.file.Sync()
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

	err := common.EnsureDirExist(replayDir)
	if err != nil {
		logger.Errorf("Create dir %s error: %s\n", replayDir, err)
		return
	}

	logger.Infof("Session %s: Replay file path: %s",r.SessionID, r.absFilePath)
	r.file, err = os.Create(r.absFilePath)
	if err != nil {
		logger.Errorf("Create file %s error: %s\n", r.absFilePath, err)
	}
	_, _ = r.file.Write([]byte("{"))
}

func (r *ReplyRecorder) End() {
	_, _ = r.file.WriteString(fmt.Sprintf(`"%f":%s}`, 0.0, `""`))
	_ = r.file.Close()
	go r.uploadReplay()
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
	if r.storage == nil {
		r.backOffStorage = defaultReplayStorage
		r.storage = NewReplayStorage()
	}
	for i := 0; i <= maxRetry; i++ {
		logger.Debug("Upload replay file: ", r.AbsGzFilePath)
		err := r.storage.Upload(r.AbsGzFilePath, r.Target)
		if err == nil {
			_ = os.Remove(r.AbsGzFilePath)
			service.FinishReply(r.SessionID)
			break
		}
		// 如果还是失败，使用备用storage再传一次
		if i == maxRetry {
			if r.storage == r.backOffStorage {
				break
			}
			logger.Errorf("Using back off storage retry upload")
			r.storage = r.backOffStorage
			r.UploadGzipFile(3)
			break
		}
	}
}
