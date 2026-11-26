package store

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/ports"
)

const (
	journalVersion        = 1
	maxJournalRecordBytes = 64 * 1024 * 1024
	compactJournalBytes   = 64 * 1024 * 1024
	compactJournalRecords = 4096
)

var errJournalRecordTooLarge = errors.New("JSONL store transaction exceeds its size limit")

type statePersistence interface {
	Commit(previous, next *memoryState) error
	Close() error
}

type persistentState struct {
	Conversations     map[string]domain.Conversation
	Messages          map[string]domain.Message
	Artifacts         map[string]domain.Artifact
	Panels            map[string]domain.PanelSession
	Contexts          map[string]domain.ContextSnapshot
	Registrations     map[string]domain.Registration
	Runs              map[string]domain.Run
	ModelCalls        map[string]domain.ModelCall
	ToolCalls         map[string]domain.ToolCall
	ToolResults       map[string]domain.ToolResult
	Approvals         map[string]domain.Approval
	Events            map[string]domain.DomainEvent
	EventHighWater    map[string]uint64
	Deliveries        map[string]domain.PanelDelivery
	DeliveryHighWater map[string]uint64
	Audits            map[string]domain.AuditRecord
}

type persistentDeletes struct {
	Conversations     []string
	Messages          []string
	Artifacts         []string
	Panels            []string
	Contexts          []string
	Registrations     []string
	Runs              []string
	ModelCalls        []string
	ToolCalls         []string
	ToolResults       []string
	Approvals         []string
	Events            []string
	EventHighWater    []string
	Deliveries        []string
	DeliveryHighWater []string
	Audits            []string
}

type persistentDelta struct {
	Upserts persistentState
	Deletes persistentDeletes
}

type journalPayload struct {
	Snapshot *persistentState
	Delta    *persistentDelta
}

type journalRecord struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	Payload   string    `json:"payload"`
	Checksum  string    `json:"checksum"`
}

type jsonlPersistence struct {
	mu             sync.Mutex
	journalPath    string
	eventDir       string
	file           *os.File
	size           int64
	records        int
	poisoned       error
	retention      RetentionOptions
	freeBytes      func(string) (int64, error)
	diskBytes      int64
	archiveSizes   map[string]int64
	usageCheckedAt time.Time
	prunedAt       time.Time
	prunePending   bool
	reclaiming     bool
	compactedAt    time.Time
}

func NewJSONL(root string, options ...RetentionOptions) (*Memory, error) {
	persistence, state, err := openJSONLRoot(root, options...)
	if err != nil {
		return nil, err
	}
	return &Memory{state: state, persistence: persistence}, nil
}

func openJSONLRoot(root string, options ...RetentionOptions) (*jsonlPersistence, *memoryState, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, nil, fmt.Errorf("JSONL store root is required")
	}
	storeDir := filepath.Join(root, "store")
	eventDir := filepath.Join(root, "events")
	for _, path := range []string{root, storeDir, eventDir} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return nil, nil, fmt.Errorf("create JSONL store directory: %w", err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return nil, nil, fmt.Errorf("protect JSONL store directory: %w", err)
		}
	}
	retention, err := retentionOptions(options)
	if err != nil {
		return nil, nil, err
	}
	return openJSONLPersistence(filepath.Join(storeDir, "runtime.jsonl"), eventDir, retention)
}

func recoverProcessLocalState(state *memoryState, now time.Time) {
	tx := &memoryTx{state: state}
	for id, run := range state.runs {
		if !activeRun(run.State) {
			continue
		}
		run.Failure = tx.interruptedFailure(run, "process_restarted")
		run.State, run.ErrorCode, run.ErrorDetail = "interrupted", "process_restarted", "run interrupted by Kael restart"
		run.ClaimOwner, run.ClaimExpiresAt, run.UpdatedAt, run.FinishedAt = "", nil, now, cloneValue(now)
		state.runs[id] = cloneStoredValue(run)
		if message, exists := state.messages[run.OutputMessageID]; exists && message.Status != "completed" {
			message.Status, message.ErrorCode, message.ErrorDetail, message.UpdatedAt = "cancelled", "process_restarted", "response interrupted by Kael restart", now
			message.Failure = cloneRawMessage(run.Failure)
			state.messages[message.ID] = cloneStoredValue(message)
		}
		payload, _ := json.Marshal(map[string]any{"state": "interrupted", "error_code": "process_restarted", "reason": "run interrupted by Kael restart", "failure": json.RawMessage(run.Failure)})
		_ = tx.CreateEvent(&domain.DomainEvent{
			ID: uuid.NewString(), ConversationID: run.ConversationID, RunID: run.ID, MessageID: run.OutputMessageID,
			AggregateType: "run", AggregateID: run.ID, Type: "run.interrupted", SchemaVersion: "1",
			Payload: payload, CreatedAt: now,
		})
	}
	for id, call := range state.modelCalls {
		if call.State != "running" {
			continue
		}
		call.State, call.ErrorCode, call.FinishedAt = "failed", "process_restarted", cloneValue(now)
		state.modelCalls[id] = cloneStoredValue(call)
	}
	for id, call := range state.toolCalls {
		if call.State != "created" && call.State != "waiting_approval" && call.State != "dispatched" && call.State != "running" {
			continue
		}
		call.State, call.UpdatedAt, call.FinishedAt = "cancelled", now, cloneValue(now)
		state.toolCalls[id] = cloneStoredValue(call)
	}
	for id, approval := range state.approvals {
		if approval.State != "pending" {
			continue
		}
		approval.State, approval.Reason, approval.UpdatedAt, approval.ResolvedAt = "expired", "Kael restarted", now, cloneValue(now)
		state.approvals[id] = cloneStoredValue(approval)
	}
	for id, panel := range state.panels {
		if panel.State != "active" && panel.State != "disconnected" {
			continue
		}
		panel.State, panel.ConnectionOwner, panel.LeaseExpiresAt, panel.UpdatedAt = "expired", "", now, now
		state.panels[id] = cloneStoredValue(panel)
	}
	for id, registration := range state.registrations {
		if registration.State != "active" {
			continue
		}
		registration.State, registration.LeaseExpiresAt, registration.UpdatedAt = "expired", now, now
		state.registrations[id] = cloneStoredValue(registration)
	}
}

func openJSONLPersistence(journalPath, eventDir string, retention RetentionOptions) (*jsonlPersistence, *memoryState, error) {
	if err := truncateIncompleteJournal(journalPath); err != nil {
		return nil, nil, err
	}
	state, records, err := loadJournal(journalPath)
	if err != nil {
		return nil, nil, err
	}
	file, err := os.OpenFile(journalPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open JSONL store journal: %w", err)
	}
	if err = os.Chmod(journalPath, 0o600); err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("protect JSONL store journal: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("inspect JSONL store journal: %w", err)
	}
	p := &jsonlPersistence{journalPath: journalPath, eventDir: eventDir, file: file, size: info.Size(), records: records, retention: retention, freeBytes: availableDiskBytes}
	// Recover and prune before rebuilding derived archives, so expired history
	// is not written back to disk during startup.
	next := state.clone()
	recoverProcessLocalState(next, time.Now().UTC())
	if err = p.cleanStartupFiles(state.events); err == nil {
		err = p.Commit(state, next)
	}
	if err == nil {
		err = p.reconcileEventArchives(next.events)
	}
	if err != nil {
		_ = p.Close()
		return nil, nil, fmt.Errorf("recover local history: %w", err)
	}
	return p, next, nil
}

func truncateIncompleteJournal(path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open JSONL store journal: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 {
		return err
	}
	last := make([]byte, 1)
	if _, err = file.ReadAt(last, info.Size()-1); err != nil {
		return fmt.Errorf("read JSONL store journal tail: %w", err)
	}
	if last[0] == '\n' {
		return nil
	}
	const chunkSize = int64(64 * 1024)
	end := info.Size()
	for end > 0 {
		start := end - chunkSize
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, end-start)
		if _, err = file.ReadAt(chunk, start); err != nil {
			return fmt.Errorf("read JSONL store journal tail: %w", err)
		}
		if index := bytes.LastIndexByte(chunk, '\n'); index >= 0 {
			if err = file.Truncate(start + int64(index) + 1); err != nil {
				return fmt.Errorf("truncate incomplete JSONL store record: %w", err)
			}
			return file.Sync()
		}
		end = start
	}
	if err = file.Truncate(0); err != nil {
		return fmt.Errorf("truncate incomplete JSONL store record: %w", err)
	}
	return file.Sync()
}

func loadJournal(path string) (*memoryState, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("read JSONL store journal: %w", err)
	}
	defer file.Close()
	state := newMemoryState()
	records := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxJournalRecordBytes)
	for scanner.Scan() {
		var record journalRecord
		if err = json.Unmarshal(scanner.Bytes(), &record); err != nil || record.Version != journalVersion {
			return nil, 0, fmt.Errorf("JSONL store journal contains an invalid record")
		}
		payload, err := decodeJournalPayload(record)
		if err != nil {
			return nil, 0, err
		}
		if payload.Snapshot != nil {
			state = payload.Snapshot.memoryState()
		}
		if payload.Delta != nil {
			applyPersistentDelta(state, *payload.Delta)
		}
		records++
	}
	if err = scanner.Err(); err != nil {
		return nil, 0, fmt.Errorf("scan JSONL store journal: %w", err)
	}
	return state, records, nil
}

func encodeJournalRecord(payload journalPayload) ([]byte, error) {
	var encoded bytes.Buffer
	if err := gob.NewEncoder(&encoded).Encode(payload); err != nil {
		return nil, fmt.Errorf("encode JSONL store record: %w", err)
	}
	if encoded.Len() > maxJournalRecordBytes/2 {
		return nil, errJournalRecordTooLarge
	}
	digest := sha256.Sum256(encoded.Bytes())
	record := journalRecord{Version: journalVersion, CreatedAt: time.Now().UTC(), Payload: base64.StdEncoding.EncodeToString(encoded.Bytes()), Checksum: hex.EncodeToString(digest[:])}
	line, err := json.Marshal(record)
	if err != nil {
		return nil, fmt.Errorf("marshal JSONL store record: %w", err)
	}
	if len(line)+1 > maxJournalRecordBytes {
		return nil, errJournalRecordTooLarge
	}
	return append(line, '\n'), nil
}

func decodeJournalPayload(record journalRecord) (journalPayload, error) {
	raw, err := base64.StdEncoding.DecodeString(record.Payload)
	if err != nil {
		return journalPayload{}, fmt.Errorf("JSONL store record payload is invalid")
	}
	digest := sha256.Sum256(raw)
	if !strings.EqualFold(record.Checksum, hex.EncodeToString(digest[:])) {
		return journalPayload{}, fmt.Errorf("JSONL store record checksum is invalid")
	}
	var payload journalPayload
	if err = gob.NewDecoder(bytes.NewReader(raw)).Decode(&payload); err != nil {
		return journalPayload{}, fmt.Errorf("decode JSONL store record: %w", err)
	}
	return payload, nil
}

func (p *jsonlPersistence) Commit(previous, next *memoryState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.poisoned != nil {
		return p.unavailableLocked()
	}
	now := time.Now().UTC()
	if err := p.refreshUsage(now); err != nil {
		return fmt.Errorf("%w: inspect local history usage: %v", ports.ErrCapacity, err)
	}
	delta := diffPersistentState(previous, next)
	archiveBytes, err := archiveWriteBytes(delta.Upserts.Events)
	if err != nil {
		return err
	}
	free, err := p.freeBytes(filepath.Dir(p.journalPath))
	if err != nil {
		return fmt.Errorf("%w: inspect free disk space: %v", ports.ErrCapacity, err)
	}
	reserve := min(p.retention.MaxBytes/10, int64(maxJournalRecordBytes))
	if p.diskBytes+archiveBytes >= p.retention.MaxBytes/10*9 || free < p.retention.MinFreeBytes+archiveBytes+reserve {
		p.reclaiming = true
	} else if p.diskBytes+archiveBytes <= p.retention.MaxBytes/10*8 {
		p.reclaiming = false
	}
	pressure := p.reclaiming
	reclaim := max(p.diskBytes+archiveBytes-p.retention.MaxBytes/10*8, p.retention.MinFreeBytes+archiveBytes+reserve-free)
	removed := p.prune(previous, next, now, pressure, reclaim)
	if len(removed) > 0 {
		delta = diffPersistentState(previous, next)
		archiveBytes, err = archiveWriteBytes(delta.Upserts.Events)
		if err != nil {
			return err
		}
	}
	snapshot := len(removed) > 0 || p.size >= compactJournalBytes || p.records >= compactJournalRecords || pressure && now.Sub(p.compactedAt) >= time.Minute
	if delta.empty() && !snapshot {
		return nil
	}
	payload := journalPayload{Delta: &delta}
	if snapshot {
		state := persistentStateFromMemory(next)
		payload = journalPayload{Snapshot: &state}
	}
	line, err := encodeJournalRecord(payload)
	if err != nil {
		return err
	}
	retained := p.diskBytes + int64(len(line)) + archiveBytes
	if snapshot {
		retained -= p.size
	}
	for _, id := range removed {
		retained -= p.archiveSizes[eventArchiveName(id)+".jsonl"]
	}
	newWork := len(newTerminalWork(previous, next)) > 0
	if err = p.checkSpace(int64(len(line))+archiveBytes, retained, newWork, snapshot && retained < p.diskBytes && !newWork); err != nil {
		return err
	}
	archives, err := p.appendEventArchives(delta.Upserts.Events)
	if err != nil {
		return p.poisonLocked(err)
	}
	if snapshot {
		oldSize := p.size
		if err = p.compact(line); err != nil {
			return p.poisonLocked(errors.Join(err, rollbackArchives(archives)))
		}
		p.diskBytes += p.size - oldSize + archiveBytes
		p.compactedAt = now
	} else {
		if p.file == nil {
			rollbackErr := rollbackArchives(archives)
			return p.poisonLocked(errors.Join(fmt.Errorf("JSONL store journal is closed"), rollbackErr))
		}
		info, err := p.file.Stat()
		if err != nil {
			rollbackErr := rollbackArchives(archives)
			return p.poisonLocked(errors.Join(fmt.Errorf("inspect JSONL store journal before append: %w", err), rollbackErr))
		}
		offset := info.Size()
		err = writeBytes(p.file, line)
		if err == nil {
			err = p.file.Sync()
		}
		if err != nil {
			return p.failJournalLocked(offset, archives, err)
		}
		p.size = offset + int64(len(line))
		p.records++
		p.diskBytes += int64(len(line)) + archiveBytes
	}
	for _, archive := range archives {
		info, statErr := os.Stat(archive.path)
		if statErr != nil {
			return p.poisonLocked(statErr)
		}
		p.archiveSizes[filepath.Base(archive.path)] = info.Size()
	}
	if err = p.removeArchives(removed); err != nil {
		return p.poisonLocked(fmt.Errorf("remove expired event archive: %w", err))
	}
	return nil
}

func (p *jsonlPersistence) Ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.poisoned != nil {
		return p.unavailableLocked()
	}
	if p.file == nil {
		return fmt.Errorf("%w: JSONL store journal is closed", ports.ErrUnavailable)
	}
	return nil
}

func (p *jsonlPersistence) failJournalLocked(offset int64, archives []archiveOffset, cause error) error {
	journalErr := p.rollbackJournalLocked(offset)
	archiveErr := rollbackArchives(archives)
	return p.poisonLocked(errors.Join(fmt.Errorf("append JSONL store journal: %w", cause), journalErr, archiveErr))
}

func (p *jsonlPersistence) rollbackJournalLocked(offset int64) error {
	if p.file == nil {
		return fmt.Errorf("rollback JSONL store journal: journal is closed")
	}
	var rollbackErrs []error
	if err := p.file.Truncate(offset); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("truncate JSONL store journal to pre-commit offset: %w", err))
	} else {
		p.size = offset
	}
	if err := p.file.Sync(); err != nil {
		rollbackErrs = append(rollbackErrs, fmt.Errorf("sync rolled back JSONL store journal: %w", err))
	}
	return errors.Join(rollbackErrs...)
}

func (p *jsonlPersistence) poisonLocked(err error) error {
	if p.poisoned == nil {
		p.poisoned = err
	}
	return p.unavailableLocked()
}

func (p *jsonlPersistence) unavailableLocked() error {
	return fmt.Errorf("%w: JSONL store requires restart: %v", ports.ErrUnavailable, p.poisoned)
}

func (p *jsonlPersistence) compact(line []byte) error {
	temporary, err := os.CreateTemp(filepath.Dir(p.journalPath), ".runtime-*.jsonl")
	if err != nil {
		return fmt.Errorf("create compacted JSONL store journal: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = temporary.Chmod(0o600); err == nil {
		err = writeBytes(temporary, line)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write compacted JSONL store journal: %w", err)
	}
	if err = p.file.Close(); err != nil {
		return fmt.Errorf("close JSONL store journal for compaction: %w", err)
	}
	if err = os.Rename(temporaryPath, p.journalPath); err != nil {
		p.file, _ = os.OpenFile(p.journalPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		return fmt.Errorf("replace JSONL store journal: %w", err)
	}
	p.file, err = os.OpenFile(p.journalPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("reopen compacted JSONL store journal: %w", err)
	}
	if err = syncJournalDirectory(filepath.Dir(p.journalPath)); err != nil {
		return fmt.Errorf("sync compacted JSONL store directory: %w", err)
	}
	p.size, p.records = int64(len(line)), 1
	return nil
}

type archiveOffset struct {
	path string
	size int64
}

func (p *jsonlPersistence) appendEventArchives(events map[string]domain.DomainEvent) ([]archiveOffset, error) {
	grouped := make(map[string][]domain.DomainEvent)
	for _, event := range events {
		if event.ConversationID != "" {
			grouped[event.ConversationID] = append(grouped[event.ConversationID], event)
		}
	}
	conversationIDs := make([]string, 0, len(grouped))
	for conversationID := range grouped {
		conversationIDs = append(conversationIDs, conversationID)
	}
	sort.Strings(conversationIDs)
	written := make([]archiveOffset, 0, len(conversationIDs))
	for _, conversationID := range conversationIDs {
		events := grouped[conversationID]
		sort.Slice(events, func(i, j int) bool { return events[i].Sequence < events[j].Sequence })
		path := filepath.Join(p.eventDir, eventArchiveName(conversationID)+".jsonl")
		file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			rollbackErr := rollbackArchives(written)
			return nil, errors.Join(fmt.Errorf("open conversation event archive: %w", err), rollbackErr)
		}
		info, err := file.Stat()
		if err == nil {
			written = append(written, archiveOffset{path: path, size: info.Size()})
		}
		for _, event := range events {
			var line []byte
			if err == nil {
				line, err = json.Marshal(event)
			}
			if err == nil {
				err = writeBytes(file, append(line, '\n'))
			}
			if err != nil {
				break
			}
		}
		if err == nil {
			err = file.Sync()
		}
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			rollbackErr := rollbackArchives(written)
			return nil, errors.Join(fmt.Errorf("append conversation event archive: %w", err), rollbackErr)
		}
	}
	return written, nil
}

func rollbackArchives(written []archiveOffset) error {
	var rollbackErrs []error
	for _, archive := range written {
		file, err := os.OpenFile(archive.path, os.O_WRONLY, 0o600)
		if err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("open conversation event archive for rollback: %w", err))
			continue
		}
		if err = file.Truncate(archive.size); err != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("truncate conversation event archive: %w", err))
		}
		if syncErr := file.Sync(); syncErr != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("sync rolled back conversation event archive: %w", syncErr))
		}
		if closeErr := file.Close(); closeErr != nil {
			rollbackErrs = append(rollbackErrs, fmt.Errorf("close rolled back conversation event archive: %w", closeErr))
		}
	}
	return errors.Join(rollbackErrs...)
}

func (p *jsonlPersistence) reconcileEventArchives(events map[string]domain.DomainEvent) error {
	grouped := make(map[string][]domain.DomainEvent)
	for _, event := range events {
		if event.ConversationID != "" {
			grouped[event.ConversationID] = append(grouped[event.ConversationID], event)
		}
	}
	conversationIDs := make([]string, 0, len(grouped))
	for conversationID := range grouped {
		conversationIDs = append(conversationIDs, conversationID)
	}
	sort.Strings(conversationIDs)
	for _, conversationID := range conversationIDs {
		values := grouped[conversationID]
		sort.Slice(values, func(i, j int) bool { return values[i].Sequence < values[j].Sequence })
		name := eventArchiveName(conversationID) + ".jsonl"
		path := filepath.Join(p.eventDir, name)
		if archiveMatches(path, values) {
			continue
		}
		var size int64
		for _, value := range values {
			raw, err := json.Marshal(value)
			if err != nil {
				return err
			}
			size += int64(len(raw) + 1)
		}
		if err := p.checkSpace(size, p.diskBytes-p.archiveSizes[name]+size, false, false); err != nil {
			return err
		}
		temporary, err := os.CreateTemp(p.eventDir, ".events-*.jsonl")
		if err != nil {
			return fmt.Errorf("create reconciled conversation event archive: %w", err)
		}
		temporaryPath := temporary.Name()
		removeTemporary := true
		if err = temporary.Chmod(0o600); err == nil {
			for _, event := range values {
				var line []byte
				line, err = json.Marshal(event)
				if err == nil {
					err = writeBytes(temporary, append(line, '\n'))
				}
				if err != nil {
					break
				}
			}
		}
		if err == nil {
			err = temporary.Sync()
		}
		if closeErr := temporary.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(temporaryPath, path)
			removeTemporary = err != nil
		}
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
		if err != nil {
			return fmt.Errorf("reconcile conversation event archive: %w", err)
		}
		p.diskBytes += size - p.archiveSizes[name]
		p.archiveSizes[name] = size
	}
	return nil
}

func eventArchiveName(conversationID string) string {
	for _, char := range conversationID {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.' {
			continue
		}
		digest := sha256.Sum256([]byte(conversationID))
		return hex.EncodeToString(digest[:])
	}
	if conversationID == "" || conversationID == "." || conversationID == ".." {
		digest := sha256.Sum256([]byte(conversationID))
		return hex.EncodeToString(digest[:])
	}
	return conversationID
}

func (p *jsonlPersistence) Close() error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.file == nil {
		return nil
	}
	if err := p.file.Sync(); err != nil {
		_ = p.file.Close()
		p.file = nil
		return err
	}
	err := p.file.Close()
	p.file = nil
	return err
}

func writeBytes(file *os.File, value []byte) error {
	written, err := file.Write(value)
	if err == nil && written != len(value) {
		return io.ErrShortWrite
	}
	return err
}

func persistentStateFromMemory(state *memoryState) persistentState {
	return persistentState{
		Conversations: cloneMap(state.conversations), Messages: cloneMap(state.messages), Artifacts: cloneMap(state.artifacts),
		Panels: cloneMap(state.panels), Contexts: cloneMap(state.contexts), Registrations: cloneMap(state.registrations),
		Runs: cloneMap(state.runs), ModelCalls: cloneMap(state.modelCalls), ToolCalls: cloneMap(state.toolCalls),
		ToolResults: cloneMap(state.toolResults), Approvals: cloneMap(state.approvals), Events: cloneMap(state.events),
		EventHighWater: cloneMap(state.eventHighWater), Deliveries: cloneMap(state.deliveries),
		DeliveryHighWater: cloneMap(state.deliveryHighWater), Audits: cloneMap(state.audits),
	}
}

func (state persistentState) memoryState() *memoryState {
	result := newMemoryState()
	result.conversations = cloneMap(state.Conversations)
	result.messages = cloneMap(state.Messages)
	result.artifacts = cloneMap(state.Artifacts)
	result.panels = cloneMap(state.Panels)
	result.contexts = cloneMap(state.Contexts)
	result.registrations = cloneMap(state.Registrations)
	result.runs = cloneMap(state.Runs)
	result.modelCalls = cloneMap(state.ModelCalls)
	result.toolCalls = cloneMap(state.ToolCalls)
	result.toolResults = cloneMap(state.ToolResults)
	result.approvals = cloneMap(state.Approvals)
	result.events = cloneMap(state.Events)
	result.eventHighWater = cloneMap(state.EventHighWater)
	result.deliveries = cloneMap(state.Deliveries)
	result.deliveryHighWater = cloneMap(state.DeliveryHighWater)
	result.audits = cloneMap(state.Audits)
	return result
}

func diffPersistentState(previous, next *memoryState) persistentDelta {
	return persistentDelta{
		Upserts: persistentState{
			Conversations: changedValues(previous.conversations, next.conversations), Messages: changedValues(previous.messages, next.messages), Artifacts: changedValues(previous.artifacts, next.artifacts),
			Panels: changedValues(previous.panels, next.panels), Contexts: changedValues(previous.contexts, next.contexts), Registrations: changedValues(previous.registrations, next.registrations),
			Runs: changedValues(previous.runs, next.runs), ModelCalls: changedValues(previous.modelCalls, next.modelCalls), ToolCalls: changedValues(previous.toolCalls, next.toolCalls),
			ToolResults: changedValues(previous.toolResults, next.toolResults), Approvals: changedValues(previous.approvals, next.approvals), Events: changedValues(previous.events, next.events),
			EventHighWater: changedValues(previous.eventHighWater, next.eventHighWater), Deliveries: changedValues(previous.deliveries, next.deliveries),
			DeliveryHighWater: changedValues(previous.deliveryHighWater, next.deliveryHighWater), Audits: changedValues(previous.audits, next.audits),
		},
		Deletes: persistentDeletes{
			Conversations: removedKeys(previous.conversations, next.conversations), Messages: removedKeys(previous.messages, next.messages), Artifacts: removedKeys(previous.artifacts, next.artifacts),
			Panels: removedKeys(previous.panels, next.panels), Contexts: removedKeys(previous.contexts, next.contexts), Registrations: removedKeys(previous.registrations, next.registrations),
			Runs: removedKeys(previous.runs, next.runs), ModelCalls: removedKeys(previous.modelCalls, next.modelCalls), ToolCalls: removedKeys(previous.toolCalls, next.toolCalls),
			ToolResults: removedKeys(previous.toolResults, next.toolResults), Approvals: removedKeys(previous.approvals, next.approvals), Events: removedKeys(previous.events, next.events),
			EventHighWater: removedKeys(previous.eventHighWater, next.eventHighWater), Deliveries: removedKeys(previous.deliveries, next.deliveries),
			DeliveryHighWater: removedKeys(previous.deliveryHighWater, next.deliveryHighWater), Audits: removedKeys(previous.audits, next.audits),
		},
	}
}

func changedValues[T any](previous, next map[string]T) map[string]T {
	result := make(map[string]T)
	for key, value := range next {
		if old, exists := previous[key]; !exists || !reflect.DeepEqual(old, value) {
			result[key] = cloneStoredValue(value)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func removedKeys[T any](previous, next map[string]T) []string {
	var result []string
	for key := range previous {
		if _, exists := next[key]; !exists {
			result = append(result, key)
		}
	}
	sort.Strings(result)
	return result
}

func (delta persistentDelta) empty() bool {
	return reflect.DeepEqual(delta, persistentDelta{})
}

func applyPersistentDelta(state *memoryState, delta persistentDelta) {
	applyChanges(state.conversations, delta.Upserts.Conversations, delta.Deletes.Conversations)
	applyChanges(state.messages, delta.Upserts.Messages, delta.Deletes.Messages)
	applyChanges(state.artifacts, delta.Upserts.Artifacts, delta.Deletes.Artifacts)
	applyChanges(state.panels, delta.Upserts.Panels, delta.Deletes.Panels)
	applyChanges(state.contexts, delta.Upserts.Contexts, delta.Deletes.Contexts)
	applyChanges(state.registrations, delta.Upserts.Registrations, delta.Deletes.Registrations)
	applyChanges(state.runs, delta.Upserts.Runs, delta.Deletes.Runs)
	applyChanges(state.modelCalls, delta.Upserts.ModelCalls, delta.Deletes.ModelCalls)
	applyChanges(state.toolCalls, delta.Upserts.ToolCalls, delta.Deletes.ToolCalls)
	applyChanges(state.toolResults, delta.Upserts.ToolResults, delta.Deletes.ToolResults)
	applyChanges(state.approvals, delta.Upserts.Approvals, delta.Deletes.Approvals)
	applyChanges(state.events, delta.Upserts.Events, delta.Deletes.Events)
	applyChanges(state.eventHighWater, delta.Upserts.EventHighWater, delta.Deletes.EventHighWater)
	applyChanges(state.deliveries, delta.Upserts.Deliveries, delta.Deletes.Deliveries)
	applyChanges(state.deliveryHighWater, delta.Upserts.DeliveryHighWater, delta.Deletes.DeliveryHighWater)
	applyChanges(state.audits, delta.Upserts.Audits, delta.Deletes.Audits)
}

func applyChanges[T any](target, upserts map[string]T, deletes []string) {
	for key, value := range upserts {
		target[key] = cloneStoredValue(value)
	}
	for _, key := range deletes {
		delete(target, key)
	}
}
