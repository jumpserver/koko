package storage

import (
	"cocogo/pkg/auth"
	"path/filepath"
	"strings"
)

func NewJmsStorage() Storage {
	appService := auth.GetGlobalService()
	return &Server{
		StorageType: "jms",
		service:     appService,
	}
}

type Server struct {
	StorageType string
	service     *auth.Service
}

func (s *Server) Upload(gZipFilePath, target string) {
	sessionID := strings.Split(filepath.Base(gZipFilePath), ".")[0]
	_ = s.service.PushSessionReplay(gZipFilePath, sessionID)
}
