package record

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"cocogo/pkg/config"
	"cocogo/pkg/storage"
)

var conf = config.Conf

func NewReplyRecord(sessionID string) *Reply {
	rootPath := conf.RootPath
	currentData := time.Now().UTC().Format("2006-01-02")
	gzFileName := sessionID + ".replay.gz"
	absFilePath := filepath.Join(rootPath, "data", "replays", currentData, sessionID)
	absGzFilePath := filepath.Join(rootPath, "data", "replays", currentData, gzFileName)

	target := strings.Join([]string{currentData, gzFileName}, "/")
	return &Reply{
		SessionID:     sessionID,
		FileName:      sessionID,
		absFilePath:   absFilePath,
		gzFileName:    gzFileName,
		absGzFilePath: absGzFilePath,
		StartTime:     time.Now().UTC(),
		target:        target,
	}
}

type Reply struct {
	SessionID     string
	FileName      string
	gzFileName    string
	absFilePath   string
	absGzFilePath string
	target        string
	WriteF        *os.File
	StartTime     time.Time
}

func (r *Reply) Record(b []byte) {
	interval := time.Now().UTC().Sub(r.StartTime).Seconds()
	data, _ := json.Marshal(string(b))
	_, _ = r.WriteF.WriteString(fmt.Sprintf("\"%0.6f\":%s,", interval, data))
}

func (r *Reply) StartRecord() {
	//auth.MakeSureDirExit(r.absFilePath)
	//r.WriteF, _ = os.Create(r.absFilePath)
	//_, _ = r.WriteF.Write([]byte("{"))
}

func (r *Reply) EndRecord(ctx context.Context) {
	select {
	case <-ctx.Done():
		_, _ = r.WriteF.WriteString(`"0":""}`)
		_ = r.WriteF.Close()
	}
	r.uploadReplay()
}

func (r *Reply) uploadReplay() {
	_ = GzipCompressFile(r.absFilePath, r.absGzFilePath)
	if sto := storage.NewStorageServer(); sto != nil {
		sto.Upload(r.absGzFilePath, r.target)
	}
	_ = os.Remove(r.absFilePath)
	_ = os.Remove(r.absGzFilePath)

}

func GzipCompressFile(srcPath, dstPath string) error {
	srcf, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	dstf, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(dstf)
	zw.Name = dstPath
	zw.ModTime = time.Now().UTC()
	_, err = io.Copy(zw, srcf)
	if err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}

	return nil

}
