package tunnel

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion/guacd"
	"github.com/jumpserver/koko/pkg/logger"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver-dev/sdk-go/service/videoworker"
	"github.com/jumpserver-dev/sdk-go/storage"
)

/*
	原始录像的 part 数据格式

data/sessions/e32248ce-2dc8-43c8-b37e-a61d5ee32176
├── e32248ce-2dc8-43c8-b37e-a61d5ee32176.0.part
├── e32248ce-2dc8-43c8-b37e-a61d5ee32176.0.part.meta
└── e32248ce-2dc8-43c8-b37e-a61d5ee32176.json

upload
├── e32248ce-2dc8-43c8-b37e-a61d5ee32176.replay.json
├── e32248ce-2dc8-43c8-b37e-a61d5ee32176.0.part.gz
*/

const ReplayType = "guacamole"

type SessionReplayMeta struct {
	model.Session
	DateEnd    common.UTCTime `json:"date_end,omitempty"`
	ReplayType string         `json:"type,omitempty"`

	PartMetas []PartFileMeta `json:"files,omitempty"`
}

type PartFileMeta struct {
	Name string `json:"name"`
	PartMeta
}

type PartUploader struct {
	SessionId string
	RootPath  string
	ApiClient *service.JMService
	TermCfg   *model.TerminalConfig

	replayMeta SessionReplayMeta
	partFiles  []os.DirEntry

	Info guacd.ClientInformation
}

func (p *PartUploader) preCheckSessionMeta() error {
	metaPath := filepath.Join(p.RootPath, p.SessionId+".json")
	if _, err := os.Stat(metaPath); err != nil {
		logger.Errorf("PartUploader %s get meta file error: %v", p.SessionId, err)
		return err
	}
	metaBuf, err := os.ReadFile(metaPath)
	if err != nil {
		logger.Errorf("PartUploader %s read meta file error: %v", p.SessionId, err)
		return err
	}
	if err1 := json.Unmarshal(metaBuf, &p.replayMeta); err1 != nil {
		logger.Errorf("PartUploader %s unmarshal meta file error: %v", p.SessionId, err)
		return err1
	}
	if p.replayMeta.DateStart == p.replayMeta.DateEnd {
		// 未结束的录像, 计算结束时间，并上传到 core api 作为会话结束时间
		endTime := GetMaxModTime(p.partFiles)
		p.replayMeta.DateEnd = common.NewUTCTime(endTime)
		// api finish time
		if _, err1 := p.ApiClient.SessionFinished(p.SessionId, p.replayMeta.DateEnd); err1 != nil {
			logger.Errorf("PartUploader %s finish session error: %v", p.SessionId, err1)
			return err1
		}
		// write meta file
		metaBuf, _ = json.Marshal(p.replayMeta)
		if err1 := os.WriteFile(metaPath, metaBuf, os.ModePerm); err1 != nil {
			logger.Errorf("PartUploader %s write meta file error: %v", p.SessionId, err1)
		}
	}
	p.replayMeta.ReplayType = ReplayType
	return nil
}

func GetMaxModTime(parts []os.DirEntry) time.Time {
	var t time.Time
	for i := range parts {
		partFile := parts[i]
		partFileInfo, err := partFile.Info()
		if err != nil {
			logger.Errorf("PartUploader get part file %s info error: %v", partFile.Name(), err)
			continue
		}
		modTime := partFileInfo.ModTime()
		if t.Before(modTime) {
			t = modTime
		}
	}
	return t
}

func (p *PartUploader) Start() {
	/*
		1、创建 upload 目录
		2、将所有的 part 文件压缩成gz文件，并移动到 upload 目录
		3、生成新的 meta 文件
		4、上传
	*/
	if err := p.CollectionPartFiles(); err != nil {
		logger.Errorf("PartUploader %s collect part files error: %v, manual handling required", p.SessionId, err)
		return
	}
	if len(p.partFiles) == 0 {
		logger.Errorf("PartUploader %s no part file", p.SessionId)
		return
	}
	partMetas, err := p.preCheckPartFiles()
	if err != nil {
		logger.Errorf("PartUploader %s check part files error: %v, manual handling required", p.SessionId, err)
		return
	}
	if err = p.preCheckSessionMeta(); err != nil {
		return
	}
	p.replayMeta.PartMetas = partMetas

	// 1、创建 upload 目录
	uploadPath := filepath.Join(p.RootPath, "upload")
	if err = os.RemoveAll(uploadPath); err != nil {
		logger.Errorf("PartUploader %s clean upload dir error: %v", p.SessionId, err)
		return
	}
	if err := os.MkdirAll(uploadPath, os.ModePerm); err != nil {
		logger.Errorf("PartUploader %s create upload dir error: %v", p.SessionId, err)
		return
	}
	// 2、将所有的 part 文件压缩移动到 upload 目录
	for i := range p.partFiles {
		partFile := p.partFiles[i]
		partFilePath := filepath.Join(p.RootPath, partFile.Name())
		partGzFilename := partFile.Name() + ".gz"
		uploadFilePath := filepath.Join(uploadPath, partGzFilename)

		if err := common.CompressToGzipFile(partFilePath, uploadFilePath); err != nil {
			logger.Errorf("PartUploader %s compress part file %s error: %v", p.SessionId, partFile.Name(), err)
			return
		}
	}
	// 3、生成新的 meta 文件
	// upload 写入 replayMeta json
	replayMetaBuf, _ := json.Marshal(p.replayMeta)
	if err := os.WriteFile(filepath.Join(uploadPath, p.SessionId+".replay.json"), replayMetaBuf, os.ModePerm); err != nil {
		logger.Errorf("PartUploader %s write replay meta file error: %v", p.SessionId, err)
		return
	}
	// 4、上传 upload 目录下的所有文件到 存储
	p.uploadToStorage(uploadPath)
}

func (p *PartUploader) CollectionPartFiles() error {
	entries, err := os.ReadDir(p.RootPath)
	if err != nil {
		return err
	}
	p.partFiles = make([]os.DirEntry, 0, 5)
	partPrefix := p.SessionId + "."
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, partPrefix) || !strings.HasSuffix(name, PartSuffix) {
			continue
		}
		if _, err = replayPartIndex(name, partPrefix); err != nil {
			return err
		}
		p.partFiles = append(p.partFiles, entry)
	}
	sort.Slice(p.partFiles, func(i, j int) bool {
		left, _ := replayPartIndex(p.partFiles[i].Name(), partPrefix)
		right, _ := replayPartIndex(p.partFiles[j].Name(), partPrefix)
		return left < right
	})
	for i := range p.partFiles {
		index, _ := replayPartIndex(p.partFiles[i].Name(), partPrefix)
		if index != i {
			return fmt.Errorf("part sequence is not continuous: expected %d, got %d", i, index)
		}
	}
	return nil
}

func replayPartIndex(name, prefix string) (int, error) {
	indexText := strings.TrimSuffix(strings.TrimPrefix(name, prefix), PartSuffix)
	index, err := strconv.Atoi(indexText)
	if err != nil || index < 0 {
		return 0, fmt.Errorf("invalid replay part filename %q", name)
	}
	return index, nil
}

func (p *PartUploader) preCheckPartFiles() ([]PartFileMeta, error) {
	partMetas := make([]PartFileMeta, 0, len(p.partFiles))
	lastPart := len(p.partFiles) - 1
	for i := range p.partFiles {
		partFile := p.partFiles[i]
		partFilePath := filepath.Join(p.RootPath, partFile.Name())
		scan, err := scanPartReplay(partFilePath)
		if err != nil {
			if i != lastPart || !errors.Is(err, io.ErrUnexpectedEOF) || scan.lastSyncOffset == 0 {
				return nil, fmt.Errorf("part %s is invalid: %w", partFile.Name(), err)
			}
			if err = os.Truncate(partFilePath, scan.lastSyncOffset); err != nil {
				return nil, fmt.Errorf("truncate last part %s: %w", partFile.Name(), err)
			}
			logger.Warnf("PartUploader %s truncated incomplete tail of last part %s to %d bytes",
				p.SessionId, partFile.Name(), scan.lastSyncOffset)
			scan, err = scanPartReplay(partFilePath)
			if err != nil {
				return nil, fmt.Errorf("check repaired last part %s: %w", partFile.Name(), err)
			}
		}
		partMetas = append(partMetas, PartFileMeta{
			Name:     partFile.Name() + ".gz",
			PartMeta: scan.meta,
		})
	}

	for i := range p.partFiles {
		metaBuf, err := json.Marshal(partMetas[i].PartMeta)
		if err != nil {
			return nil, fmt.Errorf("marshal part %s meta: %w", p.partFiles[i].Name(), err)
		}
		metaPath := filepath.Join(p.RootPath, p.partFiles[i].Name()+MetaSuffix)
		if err = os.WriteFile(metaPath, metaBuf, os.ModePerm); err != nil {
			return nil, fmt.Errorf("write part %s meta: %w", p.partFiles[i].Name(), err)
		}
	}
	return partMetas, nil
}

func (p *PartUploader) GetStorage() storage.ReplayStorage {
	return storage.NewReplayStorage(p.ApiClient, p.TermCfg.ReplayStorage)
}

const recordDirTimeFormat = "2006-01-02"

func (p *PartUploader) uploadToStorage(uploadPath string) {
	// check whether to use ENABLE_VIDEO_WORKER
	if videoWorkerClient := NewWorkerClient(config.GetConf()); videoWorkerClient != nil {
		taskCfg := videoworker.TaskConfig{
			Width:   p.Info.OptimalScreenWidth,
			Height:  p.Info.OptimalScreenHeight,
			Bitrate: 1,
		}
		taskId, err := videoWorkerClient.CreateReplaySessionTask(p.SessionId, uploadPath, &taskCfg)
		if err == nil {
			logger.Infof("Create replay session VideoWorker task success, task id: %s", taskId)
			if err = os.RemoveAll(p.RootPath); err != nil {
				logger.Errorf("PartUploader %s remove root path %s error: %v", p.SessionId, p.RootPath, err)
			}
			return
		}
		// videoWorkerClient failed then try to use self storage to upload
		logger.Errorf("Create replay session task error: %v, try to use self storage", err)
	}

	// 上传到存储
	uploadFiles, err := os.ReadDir(uploadPath)
	if err != nil {
		logger.Errorf("PartUploader %s read upload dir %s error: %v", p.SessionId, uploadPath, err)
		return
	}
	//defaultStorage := storage.ServerStorage{StorageType: "server", JmsService: p.apiClient}
	p.RecordLifecycleLog(model.ReplayUploadStart, model.EmptyLifecycleLog)
	replayStorage := p.GetStorage()
	storageType := replayStorage.TypeName()
	dateRoot := p.replayMeta.DateStart.Format(recordDirTimeFormat)
	targetRoot := strings.Join([]string{dateRoot, p.SessionId}, "/")
	logger.Infof("PartUploader %s upload replay files: %v, type: %s", p.SessionId, uploadFiles, storageType)
	totalSize := int64(0)
	for _, uploadFile := range uploadFiles {
		if uploadFile.IsDir() {
			continue
		}
		fileInfo, err := uploadFile.Info()
		if err != nil {
			logger.Errorf("PartUploader %s get file info %s error: %v", p.SessionId, uploadFile.Name(), err)
			continue
		}
		totalSize += fileInfo.Size()
		uploadFilePath := filepath.Join(uploadPath, uploadFile.Name())
		targetFile := strings.Join([]string{targetRoot, uploadFile.Name()}, "/")
		if err1 := replayStorage.Upload(uploadFilePath, targetFile); err1 != nil {
			logger.Errorf("PartUploader %s upload file %s error: %v", p.SessionId, uploadFilePath, err1)
			reason := model.SessionLifecycleLog{Reason: err1.Error()}
			p.RecordLifecycleLog(model.ReplayUploadFailure, reason)
			return
		}
		logger.Debugf("PartUploader %s upload file %s success", p.SessionId, uploadFilePath)
	}
	if _, err = p.ApiClient.FinishReplyWithSize(p.SessionId, totalSize); err != nil {
		logger.Errorf("PartUploader %s finish replay error: %v", p.SessionId, err)
		return
	}

	p.RecordLifecycleLog(model.ReplayUploadSuccess, model.EmptyLifecycleLog)
	logger.Infof("PartUploader %s upload replay success", p.SessionId)
	if err = os.RemoveAll(p.RootPath); err != nil {
		logger.Errorf("PartUploader %s remove root path %s error: %v", p.SessionId, p.RootPath, err)
		return
	}
	logger.Infof("PartUploader %s remove root path %s success", p.SessionId, p.RootPath)

}

func (p *PartUploader) RecordLifecycleLog(event model.LifecycleEvent, logObj model.SessionLifecycleLog) {
	if err := p.ApiClient.RecordSessionLifecycleLog(p.SessionId, event, logObj); err != nil {
		logger.Errorf("Record session %s lifecycle %s log err: %s", p.SessionId, event, err)
	}
}

func ReadInstruction(r *bufio.Reader) (guacd.Instruction, error) {
	return guacd.NewInstructionDecoder(r).ReadInstruction()
}

func LoadPartMetaByFile(partFile string) (PartMeta, error) {
	scan, err := scanPartReplay(partFile)
	if err != nil {
		logger.Errorf("LoadPartMetaByFile %s load replay time error: %v", partFile, err)
		return PartMeta{}, err
	}
	return scan.meta, nil
}

func LoadPartReplayTime(partFile string) (startTime int64, endTime int64, err error) {
	scan, err := scanPartReplay(partFile)
	return scan.meta.StartTime, scan.meta.EndTime, err
}

type partReplayScan struct {
	meta           PartMeta
	lastSyncOffset int64
}

type countingReader struct {
	reader    io.Reader
	bytesRead int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += int64(n)
	return n, err
}

func scanPartReplay(partFile string) (partReplayScan, error) {
	var scan partReplayScan
	fd, err := os.Open(partFile)
	if err != nil {
		return scan, err
	}
	defer fd.Close()
	info, err := fd.Stat()
	if err != nil {
		return scan, err
	}
	scan.meta.Size = info.Size()

	source := &countingReader{reader: fd}
	reader := bufio.NewReader(source)
	decoder := guacd.NewInstructionDecoder(reader)
	hasSync := false
	for {
		inst, err1 := decoder.ReadInstruction()
		if err1 != nil {
			if errors.Is(err1, io.EOF) {
				break
			}
			return scan, err1
		}
		if inst.Opcode != guacd.InstructionClientSync {
			continue
		}
		if len(inst.Args) == 0 {
			return scan, errors.New("sync instruction has no timestamp")
		}
		syncMill, err2 := strconv.ParseInt(inst.Args[0], 10, 64)
		if err2 != nil {
			return scan, fmt.Errorf("invalid sync timestamp %q: %w", inst.Args[0], err2)
		}
		if !hasSync {
			scan.meta.StartTime = syncMill
			hasSync = true
		}
		scan.meta.EndTime = syncMill
		scan.lastSyncOffset = source.bytesRead - int64(reader.Buffered())
	}
	if !hasSync {
		return scan, errors.New("replay part has no valid sync instruction")
	}
	scan.meta.Duration = scan.meta.EndTime - scan.meta.StartTime
	return scan, nil
}

func NewWorkerClient(cfg config.Config) *videoworker.WorkClient {
	if !cfg.EnableVideoWorker {
		return nil
	}
	workerURL := cfg.VideoWorkerHost
	var key model.AccessKey
	if err := key.LoadFromFile(cfg.AccessKeyFilePath); err != nil {
		logger.Errorf("Create video worker client failed: loading access key err %s", err)
		return nil
	}
	workClient := videoworker.NewClient(workerURL, key, cfg.IgnoreVerifyCerts)
	if workClient == nil {
		logger.Errorf("Create video worker client failed: worker url %s", workerURL)
		return nil
	}
	return workClient
}
