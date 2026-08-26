package lion

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/jumpserver-dev/sdk-go/common"
	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/lion/tunnel"
	"github.com/jumpserver/koko/pkg/logger"
)

func (r *Runtime) recoverRemainFiles(ctx context.Context) {
	go r.uploadRemainSessionPartReplay(ctx)
}

func (r *Runtime) uploadRemainSessionPartReplay(ctx context.Context) {
	sessionDir := config.GetConf().SessionFolderPath
	sessions, err := os.ReadDir(sessionDir)
	if err != nil {
		logger.Errorf("Read Lion session replay dir failed: %s", err)
		return
	}
	if len(sessions) == 0 {
		logger.Info("No remain Lion replay parts")
		return
	}

	logger.Infof("Start upload remain %d Lion session replay folders 10 min later", len(sessions))
	timer := time.NewTimer(10 * time.Minute)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}

	terminalConfig, err := r.jmsService.GetTerminalConfig()
	if err != nil {
		logger.Errorf("Get terminal config for Lion replay recovery failed: %s", err)
		return
	}
	for _, entry := range sessions {
		if ctx.Err() != nil {
			return
		}
		sessionID := entry.Name()
		if !entry.IsDir() || !common.IsUUID(sessionID) {
			continue
		}
		uploader := tunnel.PartUploader{
			RootPath:  filepath.Join(sessionDir, sessionID),
			SessionId: sessionID,
			ApiClient: r.jmsService,
			TermCfg:   &terminalConfig,
		}
		uploader.Start()
		logger.Infof("Upload remain Lion session replay %s finished", sessionID)
	}
}
