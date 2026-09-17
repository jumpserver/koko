package tunnel

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/lion/session"
	"github.com/jumpserver/koko/pkg/logger"
)

type ReplayRecorder struct {
	tunnelSession *session.TunnelSession
	SessionId     string
	guacdAddr     string
	conf          guacd.Configuration
	info          guacd.ClientInformation
	newPartChan   chan struct{}
	currentIndex  int
	MaxSize       int
	apiClient     *service.JMService

	RootPath string
	wg       sync.WaitGroup
}

func (r *ReplayRecorder) run(ctx context.Context) {
	r.startRecordPartReplay(ctx)
	for {
		select {
		case <-ctx.Done():
			logger.Infof("ReplayRecorder %s done", r.SessionId)
			return
		case <-r.newPartChan:
			r.currentIndex++
			r.startRecordPartReplay(ctx)
		}
	}
}

func (r *ReplayRecorder) startRecordPartReplay(ctx context.Context) {
	r.wg.Add(1)
	go r.recordReplay(ctx, &r.wg)
}

func (r *ReplayRecorder) Start(ctx context.Context) {
	if r.tunnelSession.TerminalConfig.ReplayStorage.TypeName == "null" {
		logger.Warnf("ReplayRecorder %s storage is null, not record", r.SessionId)
		return
	}
	rootPath := filepath.Join(config.GetConf().SessionFolderPath, r.SessionId)
	_ = os.MkdirAll(rootPath, os.ModePerm)
	r.RootPath = rootPath
	r.WriteSessionMeta(r.tunnelSession.Created)
	go r.run(ctx)
}

func (r *ReplayRecorder) WriteSessionMeta(t common.UTCTime) {
	var sessionData struct {
		model.Session
		DateEnd common.UTCTime `json:"date_end"`
	}
	sessionData.Session = *r.tunnelSession.ModelSession
	sessionData.DateEnd = t
	metaFilename := r.SessionId + ".json"
	metaFilePath := filepath.Join(r.RootPath, metaFilename)
	metaBuf, _ := json.Marshal(sessionData)
	if err := os.WriteFile(metaFilePath, metaBuf, os.ModePerm); err != nil {
		logger.Errorf("ReplayRecorder(%s) Write session meta file %s failed: %v", r.SessionId, metaFilename, err)
		return
	}
	logger.Infof("ReplayRecorder(%s) Write session meta file %s success", r.SessionId, metaFilename)
}

func (r *ReplayRecorder) IsConnectFailed() bool {
	// 检测录像文件是否存在，且大小大于 5KB 只检测第一个录像文件大小
	minSize := int64(1024) * 5
	partFilename := r.GetPartFilenameByIndex(0)
	partFilePath := filepath.Join(r.RootPath, partFilename)
	fi, err := os.Stat(partFilePath)
	if err != nil {
		logger.Errorf("ReplayRecorder %s get part file %s error: %v", r.SessionId, partFilename, err)
		return true
	}
	if fi.IsDir() {
		logger.Warnf("ReplayRecorder %s part file %s is a directory, not connect failed", r.SessionId, partFilename)
		return true
	}
	if fi.Size() <= minSize {
		logger.Infof("ReplayRecorder %s part file %s size %d < 5KB, not connect failed", r.SessionId, partFilename, fi.Size())
		return true
	}
	return false
}

func (r *ReplayRecorder) CleanFailedPartFileReplay() {
	// 删除后续异常的文件
	minSize := int64(1024) * 5
	for i := 0; i < r.currentIndex; i++ {
		partFilename := r.GetPartFilenameByIndex(i)
		partFilePath := filepath.Join(r.RootPath, partFilename)
		fi, err := os.Stat(partFilePath)
		if err != nil {
			logger.Errorf("ReplayRecorder %s get part file %s error: %v", r.SessionId, partFilename, err)
			continue
		}
		if fi.IsDir() {
			continue
		}
		if fi.Size() <= minSize {
			logger.Infof("ReplayRecorder %s part file %s size < 5KB, remove it and its meta file",
				r.SessionId, partFilename)
			partMeatFilePath := partFilePath + MetaSuffix
			_ = os.Remove(partFilePath)
			_ = os.Remove(partMeatFilePath)
		}
	}
}

func (r *ReplayRecorder) Stop() {
	r.wg.Wait()
	r.WriteSessionMeta(common.NewNowUTCTime())
	uploader := PartUploader{
		RootPath:  r.RootPath,
		SessionId: r.SessionId,
		ApiClient: r.apiClient,
		TermCfg:   r.tunnelSession.TerminalConfig,
		Info:      r.info,
	}

	// 检测会话文件大小是否满足录像要求，否则判断连接失败，不上传录像文件。
	if r.IsConnectFailed() {
		logger.Warnf("ReplayRecorder %s connect failed, not upload replay parts", r.SessionId)
		if err := os.RemoveAll(r.RootPath); err != nil {
			logger.Errorf("ReplayRecorder %s remove root path %s error: %v", r.SessionId, r.RootPath, err)
		}
		return
	}
	r.CleanFailedPartFileReplay()
	go uploader.Start()
	logger.Infof("Replay recorder %s stop and uploading replay parts", r.SessionId)
}

func (r *ReplayRecorder) GetPartFilename() string {
	return fmt.Sprintf("%s.%d.part", r.SessionId, r.currentIndex)
}

func (r *ReplayRecorder) GetPartFilenameByIndex(index int) string {
	return fmt.Sprintf("%s.%d.part", r.SessionId, index)
}

type PartMeta struct {
	StartTime int64 `json:"start,omitempty"`
	EndTime   int64 `json:"end,omitempty"`
	Duration  int64 `json:"duration,omitempty"`
	Size      int64 `json:"size,omitempty"`
}

const (
	PartSuffix = ".part"
	MetaSuffix = ".meta"
)

func (r *ReplayRecorder) recordReplay(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	joinTunnel, err1 := guacd.NewTunnel(r.guacdAddr, r.conf, r.info)
	if err1 != nil {
		logger.Errorf("Join replay tunnel %s failed: %v", r.SessionId, err1)
		return
	}
	defer joinTunnel.Close()
	partFilename := r.GetPartFilename()
	partMetaFilename := partFilename + MetaSuffix
	partFilePath := filepath.Join(r.RootPath, partFilename)
	partMetaFilePath := filepath.Join(r.RootPath, partMetaFilename)
	partRecorder := PartRecorder{
		Id:           r.SessionId,
		MetaFilename: partMetaFilename,
		MetaFilePath: partMetaFilePath,
		PartFilename: partFilename,
		PartFilePath: partFilePath,
		MaxSize:      r.MaxSize,
		currentIndex: r.currentIndex,
		ExitSignal: func() {
			r.newPartChan <- struct{}{}
		},
	}
	partRecorder.Start(ctx, joinTunnel)
}

func NewReplayConfiguration(conf *guacd.Configuration, connectionId string) guacd.Configuration {
	newCfg := conf.Clone()
	newCfg.ConnectionID = connectionId
	newCfg.SetParameter(guacd.READONLY, guacd.BoolTrue)
	return newCfg
}

type PartRecorder struct {
	Id           string
	MetaFilename string
	MetaFilePath string

	PartFilename string
	PartFilePath string

	MaxSize      int
	currentIndex int
	ExitSignal   func()

	StartTime int64
	EndTime   int64
}

func (p *PartRecorder) String() string {
	return fmt.Sprintf("%s, part %d", p.Id, p.currentIndex)
}

func (p *PartRecorder) Start(ctx context.Context, joinTunnel *guacd.Tunnel) {
	fd, err := os.Create(p.PartFilePath)
	if err != nil {
		logger.Errorf("PartRecorder create replay file %s failed: %v", p.PartFilePath, err)
		return
	}
	defer fd.Close()
	writer := bufio.NewWriter(fd)
	defer writer.Flush()
	totalWrittenSize := 0
	disconnectInst := guacd.NewInstruction(guacd.InstructionClientDisconnect)
	var (
		waitExit bool
	)
	for {
		inst, err2 := joinTunnel.ReadInstruction()
		if err2 != nil {
			if waitExit && (err2 == io.EOF) {
				logger.Infof("PartRecorder(%s) tunnel EOF", p)
				break
			}
			logger.Warnf("PartRecorder(%s) read failed: %v", p, err2)
			break
		}
		if inst.Opcode == INTERNALDATAOPCODE && len(inst.Args) >= 2 && inst.Args[0] == PINGOPCODE {
			if err3 := joinTunnel.WriteInstruction(guacd.NewInstruction(INTERNALDATAOPCODE, PINGOPCODE)); err3 != nil {
				logger.Warnf("Join tunnel %s write ping failed: %v", p.Id, err3)
			}
			continue
		}
		select {
		case <-ctx.Done():
			if !waitExit {
				_ = joinTunnel.WriteInstructionAndFlush(disconnectInst)
				waitExit = true
				logger.Infof("PartRecorder(%s) ctx done and sned disconnect to guacd", p)
			} else {
				logger.Infof("PartRecorder(%s) ctx done and wait exit", p)
			}
		default:

		}
		switch inst.Opcode {
		case guacd.InstructionClientSync:
			_ = joinTunnel.WriteInstructionAndFlush(inst)
			if syncTime, err3 := strconv.ParseInt(inst.Args[0], 10, 64); err3 == nil {
				p.EndTime = syncTime
				if p.StartTime == 0 {
					p.StartTime = syncTime
				}
			}
		case guacd.InstructionClientNop:
			logger.Debugf("PartRecorder(%s) receive nop", p)
			_ = joinTunnel.WriteInstructionAndFlush(inst)
			continue
		default:
		}
		wr, err3 := writer.WriteString(inst.String())
		if err3 != nil {
			logger.Errorf("PartRecorder(%s) write failed: %v", p, err3)
		}
		totalWrittenSize += wr
		if totalWrittenSize > p.MaxSize && !waitExit {
			_ = joinTunnel.WriteInstructionAndFlush(disconnectInst)
			waitExit = true
			logger.Infof("PartRecorder(%s) finish, start new part", p)
			if p.ExitSignal != nil {
				p.ExitSignal()
			}
		}
		if inst.Opcode == guacd.InstructionClientDisconnect {
			logger.Infof("PartRecorder(%s) receive disconnect", p)
			break
		}
	}
	p.WritePartMeta(totalWrittenSize)
}

func (p *PartRecorder) WritePartMeta(size int) {
	meta := PartMeta{
		StartTime: p.StartTime,
		EndTime:   p.EndTime,
		Duration:  p.EndTime - p.StartTime,
		Size:      int64(size),
	}
	metaBuf, _ := json.Marshal(meta)
	if err := os.WriteFile(p.MetaFilePath, metaBuf, os.ModePerm); err != nil {
		logger.Errorf("Write replay meta file %s failed: %v", p.MetaFilename, err)
	}
}
