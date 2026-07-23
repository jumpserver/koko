package terminalai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/config"
	"github.com/jumpserver/koko/pkg/logger"
)

type auditWriter struct {
	mu         sync.Mutex
	file       *os.File
	sessionID  string
	terminalID uint32
}

type auditEvent struct {
	name    string
	payload any
}

func newAuditWriter(sessionID string, terminalID uint32) *auditWriter {
	sessionID = safeAuditName(sessionID)
	if sessionID == "" {
		return nil
	}
	root := filepath.Join(config.GetConf().DataFolderPath, "terminal_ai", time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(root, 0700); err != nil {
		logger.Errorf("Create terminal AI audit directory failed: %s", err)
		return nil
	}
	_ = os.Chmod(root, 0700)
	path := filepath.Join(root, sessionID+".jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		logger.Errorf("Open terminal AI audit failed: %s", err)
		return nil
	}
	_ = file.Chmod(0600)
	return &auditWriter{
		file: file, sessionID: sessionID, terminalID: terminalID,
	}
}

func safeAuditName(value string) string {
	value = strings.TrimSpace(value)
	var result strings.Builder
	for _, char := range value {
		switch {
		case char >= 'a' && char <= 'z':
			result.WriteRune(char)
		case char >= 'A' && char <= 'Z':
			result.WriteRune(char)
		case char >= '0' && char <= '9':
			result.WriteRune(char)
		case char == '-', char == '_':
			result.WriteRune(char)
		}
	}
	return result.String()
}

func (r *Runtime) writeAudit(event string, payload any) {
	r.mu.Lock()
	writer := r.audit
	if writer == nil {
		if len(r.auditPending) < 1000 {
			r.auditPending = append(r.auditPending, auditEvent{name: event, payload: payload})
		}
	}
	r.mu.Unlock()
	if writer == nil {
		return
	}
	writer.Write(event, payload)
}

func (w *auditWriter) Write(event string, payload any) {
	if w == nil || w.file == nil {
		return
	}
	record := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"sessionId": w.sessionID, "terminalId": w.terminalID,
		"event": event, "payload": payload,
	}
	value, err := json.Marshal(record)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_, _ = w.file.Write(append(value, '\n'))
}

func (w *auditWriter) Close() {
	if w == nil || w.file == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	_ = w.file.Close()
	w.file = nil
}
