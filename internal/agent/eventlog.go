package agent

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/internal/agentapi"
)

var (
	errEventPayloadTooLarge = errors.New("agent event payload is too large")
	errEventLogClosed       = errors.New("agent event log is closed")
	errEventLogPoisoned     = errors.New("agent event log is unavailable after a durable write failure")
	errEventLogCorrupt      = errors.New("agent event log is structurally corrupt")
	errEventStoreFull       = errors.New("agent event store reached its size limit")
)

const (
	maxJSONLLineBytes = agentapi.MaxEventPayloadBytes + 16*1024
	maxJSONLFileBytes = 64 * 1024 * 1024
	activeLogSuffix   = ".active.jsonl"
	closedLogSuffix   = ".closed.jsonl"
)

type eventLog struct {
	mu       sync.Mutex
	capacity int
	next     uint64
	events   []agentapi.Event
	notify   chan struct{}
	path     string
	file     durableEventFile
	closed   bool
	poisoned error
	fileSize int64
	records  uint64
	index    []eventOffset
}

type eventOffset struct {
	sequence uint64
	offset   int64
}

const eventIndexStride = 256

type durableEventFile interface {
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

func newEventLog(capacity int) *eventLog {
	if capacity <= 0 {
		capacity = 256
	}
	return &eventLog{
		capacity: capacity,
		events:   make([]agentapi.Event, 0, capacity),
		notify:   make(chan struct{}),
	}
}

func openEventLog(path string, capacity int) (*eventLog, []agentapi.Event, error) {
	log := newEventLog(capacity)
	log.path = path
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open agent event log: %w", err)
	}
	if err = file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("protect agent event log: %w", err)
	}
	log.file = file
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if info.Size() > maxJSONLFileBytes {
		_ = file.Close()
		return nil, nil, errEventStoreFull
	}
	loaded := make([]agentapi.Event, 0)
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	reader := bufio.NewReaderSize(file, maxJSONLLineBytes+1)
	offset := int64(0)
	fileSize := info.Size()
	for {
		lineStart := offset
		line, readErr := reader.ReadSlice('\n')
		offset += int64(len(line))
		if errors.Is(readErr, bufio.ErrBufferFull) {
			_ = file.Close()
			return nil, nil, fmt.Errorf("%w: record exceeds its limit", errEventLogCorrupt)
		}
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			_ = file.Close()
			return nil, nil, fmt.Errorf("read agent event log: %w", readErr)
		}
		if errors.Is(readErr, io.EOF) {
			if len(line) == 0 {
				break
			}
			if incompleteJSONRecord(line) {
				if err = file.Truncate(lineStart); err == nil {
					err = file.Sync()
				}
				if err != nil {
					_ = file.Close()
					return nil, nil, fmt.Errorf("truncate incomplete agent event tail: %w", err)
				}
				fileSize = lineStart
				break
			}
			_ = file.Close()
			return nil, nil, fmt.Errorf("%w: non-terminated final record", errEventLogCorrupt)
		}
		line = bytes.TrimSuffix(line, []byte{'\n'})
		var event agentapi.Event
		if err = json.Unmarshal(line, &event); err != nil {
			_ = file.Close()
			return nil, nil, fmt.Errorf("%w: invalid JSON record", errEventLogCorrupt)
		}
		if event.Sequence == 0 || event.Sequence <= log.next || event.ResourceSessionID == "" {
			_ = file.Close()
			return nil, nil, fmt.Errorf("%w: invalid event sequence", errEventLogCorrupt)
		}
		log.next = event.Sequence
		if log.records%eventIndexStride == 0 {
			log.index = append(log.index, eventOffset{sequence: event.Sequence, offset: lineStart})
		}
		log.records++
		loaded = append(loaded, event)
		log.push(event)
	}
	log.fileSize = fileSize
	if _, err = file.Seek(0, io.SeekEnd); err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	return log, loaded, nil
}

func (l *eventLog) append(event agentapi.Event) (agentapi.Event, error) {
	if len(event.Payload) > agentapi.MaxEventPayloadBytes {
		return agentapi.Event{}, errEventPayloadTooLarge
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return agentapi.Event{}, errEventLogClosed
	}
	if l.poisoned != nil {
		return agentapi.Event{}, l.poisoned
	}
	l.next++
	event.Sequence = l.next
	if event.EventID == "" {
		event.EventID = fmt.Sprintf("%s:%d", event.ResourceSessionID, event.Sequence)
	}
	if event.Timestamp == 0 {
		event.Timestamp = time.Now().UnixMilli()
	}
	if l.file != nil {
		recordOffset := l.fileSize
		persisted, err := eventForStorage(event)
		if err != nil {
			l.next--
			return agentapi.Event{}, err
		}
		encoded, err := json.Marshal(persisted)
		if err != nil {
			l.next--
			return agentapi.Event{}, err
		}
		if len(encoded)+1 > maxJSONLLineBytes {
			l.next--
			return agentapi.Event{}, errEventPayloadTooLarge
		}
		encoded = append(encoded, '\n')
		if l.fileSize+int64(len(encoded)) > maxJSONLFileBytes {
			l.next--
			return agentapi.Event{}, errEventStoreFull
		}
		written, writeErr := l.file.Write(encoded)
		if writeErr != nil || written != len(encoded) {
			if writeErr == nil {
				writeErr = io.ErrShortWrite
			}
			return agentapi.Event{}, l.poisonLocked(fmt.Errorf("append agent event: %w", writeErr))
		}
		if err = l.file.Sync(); err != nil {
			return agentapi.Event{}, l.poisonLocked(fmt.Errorf("sync agent event: %w", err))
		}
		l.fileSize += int64(written)
		if l.records%eventIndexStride == 0 {
			l.index = append(l.index, eventOffset{sequence: event.Sequence, offset: recordOffset})
		}
		l.records++
	}
	l.push(event)
	close(l.notify)
	l.notify = make(chan struct{})
	return event, nil
}

func eventForStorage(event agentapi.Event) (agentapi.Event, error) {
	switch event.Type {
	case agentapi.EventToolCall:
		var call agentapi.ToolCall
		if err := json.Unmarshal(event.Payload, &call); err != nil {
			return agentapi.Event{}, err
		}
		payload, err := json.Marshal(struct {
			RunID           string          `json:"run_id"`
			ToolCallID      string          `json:"tool_call_id"`
			Revision        uint64          `json:"revision"`
			ToolName        string          `json:"tool_name"`
			Arguments       json.RawMessage `json:"arguments"`
			ModelDurationMS int64           `json:"model_duration_ms,omitempty"`
		}{call.RunID, call.ToolCallID, call.Revision, call.ToolName, call.Arguments, call.ModelDurationMS})
		if err != nil {
			return agentapi.Event{}, err
		}
		event.Payload = payload
	case agentapi.EventToolCancel:
		var cancel agentapi.ToolCancel
		if err := json.Unmarshal(event.Payload, &cancel); err != nil {
			return agentapi.Event{}, err
		}
		payload, err := json.Marshal(struct {
			RunID      string `json:"run_id"`
			ToolCallID string `json:"tool_call_id"`
			Reason     string `json:"reason,omitempty"`
		}{cancel.RunID, cancel.ToolCallID, cancel.Reason})
		if err != nil {
			return agentapi.Event{}, err
		}
		event.Payload = payload
	}
	return event, nil
}

func (l *eventLog) appendJSON(event agentapi.Event, payload any) (agentapi.Event, error) {
	value, err := json.Marshal(payload)
	if err != nil {
		return agentapi.Event{}, err
	}
	event.Payload = value
	return l.append(event)
}

func (l *eventLog) cursor() uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.next
}

func (l *eventLog) after(cursor uint64) (
	events []agentapi.Event,
	next <-chan struct{},
	expired bool,
) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.poisoned != nil {
		return nil, l.notify, true
	}
	if len(l.events) > 0 {
		oldest := l.events[0].Sequence
		if cursor+1 < oldest {
			return nil, l.notify, true
		}
	}
	for index := range l.events {
		if l.events[index].Sequence > cursor {
			events = append(events, l.events[index:]...)
			break
		}
	}
	return events, l.notify, false
}

func (l *eventLog) history(after uint64, limit int) ([]agentapi.Event, bool, error) {
	if limit <= 0 || limit > agentapi.MaxHistoryLimit {
		limit = agentapi.MaxHistoryLimit
	}
	l.mu.Lock()
	path := l.path
	offset := l.historyOffsetLocked(after)
	snapshotSize := l.fileSize
	if path == "" {
		result := make([]agentapi.Event, 0, limit+1)
		for _, event := range l.events {
			if event.Sequence > after {
				stored, err := eventForStorage(event)
				if err != nil {
					l.mu.Unlock()
					return nil, false, err
				}
				result = append(result, stored)
				if len(result) > limit {
					break
				}
			}
		}
		l.mu.Unlock()
		return trimPage(result, limit)
	}
	file, err := os.Open(path)
	l.mu.Unlock()
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	if offset < 0 || offset > snapshotSize {
		return nil, false, fmt.Errorf("agent history offset is invalid")
	}
	result := make([]agentapi.Event, 0, limit+1)
	reader := io.NewSectionReader(file, offset, snapshotSize-offset)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxJSONLLineBytes+1)
	for scanner.Scan() {
		var event agentapi.Event
		if err = json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, false, err
		}
		if event.Sequence > after {
			result = append(result, event)
			if len(result) > limit {
				break
			}
		}
	}
	if err = scanner.Err(); err != nil {
		return nil, false, err
	}
	return trimPage(result, limit)
}

func (l *eventLog) historyOffsetLocked(after uint64) int64 {
	if after == 0 || len(l.index) == 0 {
		return 0
	}
	position := sort.Search(len(l.index), func(index int) bool {
		return l.index[index].sequence > after
	})
	if position == 0 {
		return 0
	}
	return l.index[position-1].offset
}

func (l *eventLog) close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	if l.poisoned == nil {
		close(l.notify)
	}
	file := l.file
	l.file = nil
	l.mu.Unlock()
	if file != nil {
		return file.Close()
	}
	return nil
}

func (l *eventLog) poisonLocked(cause error) error {
	if l.poisoned == nil {
		l.poisoned = fmt.Errorf("%w: %v", errEventLogPoisoned, cause)
		if !l.closed {
			close(l.notify)
		}
	}
	return l.poisoned
}

func (l *eventLog) invalidate(cause error) {
	l.mu.Lock()
	l.poisonLocked(cause)
	l.mu.Unlock()
}

func (l *eventLog) isPoisoned() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.poisoned != nil
}

func incompleteJSONRecord(value []byte) bool {
	value = bytes.TrimSpace(value)
	var decoded any
	err := json.Unmarshal(value, &decoded)
	var syntax *json.SyntaxError
	return errors.As(err, &syntax) &&
		strings.Contains(strings.ToLower(syntax.Error()), "unexpected end") &&
		syntax.Offset >= int64(len(value))
}

func (l *eventLog) archiveClosed() error {
	_, _, err := l.archiveClosedWithAllowance(nil)
	return err
}

func (l *eventLog) archiveClosedWithAllowance(
	allow func(int64) error,
) (bool, int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	path := l.path
	closed := l.closed
	if path == "" || !strings.HasSuffix(path, activeLogSuffix) {
		return false, 0, nil
	}
	if !closed {
		return false, 0, fmt.Errorf("archive an open agent event log")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false, 0, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxJSONLFileBytes {
		return false, 0, fmt.Errorf("archive agent event log: invalid active file")
	}
	if allow != nil {
		if err = allow(info.Size()); err != nil {
			return false, 0, err
		}
	}
	target := strings.TrimSuffix(path, activeLogSuffix) + closedLogSuffix
	if _, err = os.Lstat(target); err == nil {
		return false, 0, fmt.Errorf("closed agent event log already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, 0, err
	}
	if err = os.Rename(path, target); err != nil {
		return false, 0, fmt.Errorf("archive agent event log: %w", err)
	}
	if l.path == path {
		l.path = target
	}
	return true, info.Size(), nil
}

func (l *eventLog) quarantine() error {
	_, _, err := l.quarantineWithAllowance(nil)
	return err
}

func (l *eventLog) quarantineWithAllowance(
	allow func(int64) error,
) (bool, int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	path := l.path
	closed := l.closed
	if path == "" || !strings.HasSuffix(path, activeLogSuffix) {
		return false, 0, nil
	}
	if !closed {
		return false, 0, fmt.Errorf("quarantine an open agent event log")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false, 0, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxJSONLFileBytes {
		return false, 0, fmt.Errorf("quarantine agent event log: invalid active file")
	}
	if allow != nil {
		if err = allow(info.Size()); err != nil {
			return false, 0, err
		}
	}
	target := quarantineTarget(path)
	if _, err = os.Lstat(target); err == nil {
		return false, 0, fmt.Errorf("quarantined agent event log already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, 0, err
	}
	if err = os.Rename(path, target); err != nil {
		return false, 0, fmt.Errorf("quarantine agent event log: %w", err)
	}
	if l.path == path {
		l.path = target
	}
	return true, info.Size(), nil
}

func quarantineTarget(path string) string {
	return strings.TrimSuffix(path, activeLogSuffix) +
		fmt.Sprintf(".quarantined-%d.jsonl", time.Now().UnixNano())
}

func (l *eventLog) push(event agentapi.Event) {
	if len(l.events) == l.capacity {
		copy(l.events, l.events[1:])
		l.events[len(l.events)-1] = event
	} else {
		l.events = append(l.events, event)
	}
}

func trimPage(events []agentapi.Event, limit int) ([]agentapi.Event, bool, error) {
	if len(events) > limit {
		return events[:limit], true, nil
	}
	return events, false, nil
}
