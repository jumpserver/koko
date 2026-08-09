package terminalai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/terminalai/provider"
)

type auditWriter struct {
	mu         sync.Mutex
	file       *os.File
	root       string
	tempPath   string
	path       string
	userID     string
	sessionID  string
	terminalID uint32
	retention  int
	metrics    auditMetrics
}

type auditMetrics struct {
	ProviderRequests   int64               `json:"providerRequests"`
	ProviderResponses  int64               `json:"providerResponses"`
	ErrorEvents        int64               `json:"errorEvents"`
	ProviderRetries    int64               `json:"providerRetries"`
	ProviderFallbacks  int64               `json:"providerFallbacks"`
	ContextFallbacks   int64               `json:"contextFallbacks"`
	HistoryCheckpoints int64               `json:"historyCheckpoints"`
	Usage              provider.TokenUsage `json:"usage"`
}

var activeAuditFiles = struct {
	sync.Mutex
	paths map[string]int
}{paths: make(map[string]int)}

type auditEvent struct {
	name    string
	payload any
}

func newAuditWriter(
	userID string,
	terminalID uint32,
	root string,
	retention int,
) *auditWriter {
	userID = safeAuditName(userID)
	if userID == "" || strings.TrimSpace(root) == "" {
		return nil
	}
	userRoot := filepath.Join(root, userID)
	if err := os.MkdirAll(userRoot, 0700); err != nil {
		logger.Errorf("Create terminal AI memory directory failed: %s", err)
		return nil
	}
	_ = os.Chmod(userRoot, 0700)
	removeStalePending(userRoot)
	file, err := os.CreateTemp(userRoot, ".pending-*.jsonl")
	if err != nil {
		logger.Errorf("Open terminal AI memory file failed: %s", err)
		return nil
	}
	_ = file.Chmod(0600)
	return &auditWriter{
		file: file, root: userRoot, tempPath: file.Name(), userID: userID,
		terminalID: terminalID, retention: max(1, retention),
	}
}

func (w *auditWriter) Record(event string, payload any) {
	if event == "provider_fallback" || event == "context_fallback" {
		value, _ := json.Marshal(payload)
		logger.Infof("Terminal AI %s: %s", event, value)
	}
	w.Write(event, payload)
}

func (w *auditWriter) SetSessionID(value string) {
	if w == nil {
		return
	}
	sessionID := safeAuditName(value)
	if sessionID == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.sessionID != "" || w.file == nil {
		return
	}
	path := filepath.Join(w.root, sessionID+".jsonl")
	_ = w.file.Sync()
	_ = w.file.Close()
	w.file = nil
	if err := os.Rename(w.tempPath, path); err != nil {
		logger.Errorf("Rename terminal AI memory file failed: %s", err)
		file, openErr := os.OpenFile(w.tempPath, os.O_APPEND|os.O_WRONLY, 0600)
		if openErr == nil {
			w.file = file
		}
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		logger.Errorf("Reopen terminal AI memory file failed: %s", err)
		w.file = nil
		return
	}
	w.file = file
	w.path = path
	w.tempPath = ""
	w.sessionID = sessionID
	_ = file.Chmod(0600)
	registerActiveAudit(path)
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

func (r *Runtime) SetAuditWriter(writer *auditWriter) {
	r.mu.Lock()
	if r.audit == nil {
		r.audit = writer
	}
	pending := r.auditPending
	r.auditPending = nil
	r.mu.Unlock()
	if writer == nil {
		return
	}
	for _, event := range pending {
		writer.Write(event.name, event.payload)
	}
}

func (r *Runtime) writeAudit(event string, payload any) {
	r.mu.Lock()
	writer := r.audit
	if writer == nil && len(r.auditPending) < 1000 {
		r.auditPending = append(r.auditPending, auditEvent{name: event, payload: payload})
	}
	r.mu.Unlock()
	if writer != nil {
		writer.Write(event, payload)
	}
}

func (w *auditWriter) Write(event string, payload any) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return
	}
	w.updateMetricsLocked(event, payload)
	record := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"userId":    w.userID, "sessionId": w.sessionID,
		"terminalId": w.terminalID, "event": event, "payload": payload,
	}
	value, err := json.Marshal(record)
	if err != nil {
		return
	}
	if _, err = w.file.Write(append(value, '\n')); err != nil {
		logger.Errorf("Write terminal AI memory file failed: %s", err)
	}
}

func (w *auditWriter) updateMetricsLocked(event string, payload any) {
	switch event {
	case "provider_request":
		w.metrics.ProviderRequests++
	case "provider_response":
		w.metrics.ProviderResponses++
		values, _ := payload.(map[string]any)
		result, _ := values["result"].(provider.CompletionResult)
		w.metrics.Usage.InputTokens += result.Usage.InputTokens
		w.metrics.Usage.OutputTokens += result.Usage.OutputTokens
		w.metrics.Usage.ReasoningTokens += result.Usage.ReasoningTokens
		w.metrics.Usage.CachedTokens += result.Usage.CachedTokens
		w.metrics.Usage.CacheWriteTokens += result.Usage.CacheWriteTokens
		w.metrics.Usage.TotalTokens += result.Usage.TotalTokens
	case "provider_error", "data-error", "model_output_repair":
		w.metrics.ErrorEvents++
	case "provider_retry":
		w.metrics.ProviderRetries++
	case "provider_fallback":
		w.metrics.ProviderFallbacks++
	case "context_fallback":
		w.metrics.ContextFallbacks++
	case "history_checkpoint":
		w.metrics.HistoryCheckpoints++
	}
}

func (w *auditWriter) metricsSnapshot() auditMetrics {
	w.mu.Lock()
	metrics := w.metrics
	w.mu.Unlock()
	return metrics
}

func (w *auditWriter) Close() {
	if w == nil {
		return
	}
	w.Write("session_metrics", w.metricsSnapshot())
	w.mu.Lock()
	file := w.file
	tempPath := w.tempPath
	root := w.root
	path := w.path
	retention := w.retention
	w.file = nil
	w.mu.Unlock()
	if file != nil {
		_ = file.Sync()
		_ = file.Close()
	}
	if path == "" && tempPath != "" {
		_ = os.Remove(tempPath)
		return
	}
	unregisterActiveAudit(path)
	pruneAuditSessions(root, retention)
}

func registerActiveAudit(path string) {
	activeAuditFiles.Lock()
	activeAuditFiles.paths[path]++
	activeAuditFiles.Unlock()
}

func unregisterActiveAudit(path string) {
	activeAuditFiles.Lock()
	if activeAuditFiles.paths[path] <= 1 {
		delete(activeAuditFiles.paths, path)
	} else {
		activeAuditFiles.paths[path]--
	}
	activeAuditFiles.Unlock()
}

func auditIsActive(path string) bool {
	activeAuditFiles.Lock()
	active := activeAuditFiles.paths[path] > 0
	activeAuditFiles.Unlock()
	return active
}

func pruneAuditSessions(root string, retention int) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	type candidate struct {
		path    string
		modTime time.Time
	}
	files := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") ||
			strings.HasPrefix(entry.Name(), ".pending-") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr == nil {
			files = append(files, candidate{
				path: filepath.Join(root, entry.Name()), modTime: info.ModTime(),
			})
		}
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})
	for _, file := range files[min(retention, len(files)):] {
		if auditIsActive(file.path) {
			continue
		}
		if err := os.Remove(file.path); err != nil {
			logger.Errorf("Prune terminal AI memory file failed: %s", err)
		}
	}
}

func removeStalePending(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), ".pending-") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(root, entry.Name()))
		}
	}
}
