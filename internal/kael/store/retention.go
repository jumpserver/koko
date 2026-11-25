package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jumpserver/koko/internal/kael/domain"
	"github.com/jumpserver/koko/internal/kael/ports"
)

// RetentionOptions bounds local JSONL history. The journal and all event files
// count as usage; only terminal conversations are evicted.
type RetentionOptions struct {
	KeepDays     int
	MaxBytes     int64
	MinFreeBytes int64
}

func retentionOptions(values []RetentionOptions) (RetentionOptions, error) {
	value := RetentionOptions{KeepDays: 7, MaxBytes: 1 << 30, MinFreeBytes: 1 << 30}
	if len(values) > 0 {
		value = values[0]
	}
	if len(values) > 1 || value.KeepDays < 1 || value.KeepDays > 3650 || value.MaxBytes <= 0 || value.MinFreeBytes < 0 {
		return value, fmt.Errorf("invalid local history retention options")
	}
	return value, nil
}

// refreshUsage runs at most once a minute, not for every streamed delta. Writes
// update these counters under the journal lock between directory scans.
func (p *jsonlPersistence) refreshUsage(now time.Time) error {
	if !p.usageCheckedAt.IsZero() && now.Sub(p.usageCheckedAt) < time.Minute {
		return nil
	}
	p.archiveSizes = make(map[string]int64)
	p.diskBytes = 0
	for _, directory := range []string{filepath.Dir(p.journalPath), p.eventDir} {
		file, err := os.Open(directory)
		if err != nil {
			return err
		}
		for {
			entries, readErr := file.ReadDir(128)
			for _, entry := range entries {
				info, statErr := entry.Info()
				if statErr != nil {
					_ = file.Close()
					return statErr
				}
				if !info.Mode().IsRegular() {
					continue
				}
				p.diskBytes += info.Size()
				if directory == p.eventDir {
					p.archiveSizes[entry.Name()] = info.Size()
				}
			}
			if readErr != nil {
				_ = file.Close()
				if readErr != io.EOF {
					return readErr
				}
				break
			}
		}
	}
	p.usageCheckedAt = now
	return nil
}

func archiveWriteBytes(events map[string]domain.DomainEvent) (int64, error) {
	var size int64
	for _, event := range events {
		if event.ConversationID == "" {
			continue
		}
		raw, err := json.Marshal(event)
		if err != nil {
			return 0, err
		}
		size += int64(len(raw) + 1)
	}
	return size, nil
}

func newTerminalWork(previous, next *memoryState) map[string]bool {
	ids := make(map[string]bool)
	for id, conversation := range next.conversations {
		if _, exists := previous.conversations[id]; !exists && conversation.Profile == "terminal" {
			ids[id] = true
		}
	}
	for id, message := range next.messages {
		if _, exists := previous.messages[id]; !exists && message.Role == "user" {
			if next.conversations[message.ConversationID].Profile == "terminal" {
				ids[message.ConversationID] = true
			}
		}
	}
	for id, run := range next.runs {
		if _, exists := previous.runs[id]; !exists && next.conversations[run.ConversationID].Profile == "terminal" {
			ids[run.ConversationID] = true
		}
	}
	return ids
}

// prune removes at most 100 inactive terminal conversations per pass. Capacity
// pressure may evict history before its age limit; live panels/runs are protected.
func (p *jsonlPersistence) prune(previous, next *memoryState, now time.Time, pressure bool, reclaim int64) []string {
	interval := time.Hour
	if pressure || p.prunePending {
		interval = time.Minute
	}
	if !p.prunedAt.IsZero() && now.Sub(p.prunedAt) < interval {
		return nil
	}
	p.prunedAt = now
	protected := newTerminalWork(previous, next)
	for _, panel := range next.panels {
		if (panel.State == "active" || panel.State == "disconnected") && panel.LeaseExpiresAt.After(now) {
			protected[panel.ConversationID] = true
		}
	}
	for _, run := range next.runs {
		if activeRun(run.State) {
			protected[run.ConversationID] = true
		}
	}
	// Do not evict history changed in this transaction before Core has
	// acknowledged the corresponding audit projection.
	for id, message := range next.messages {
		prior, exists := previous.messages[id]
		if !exists || !reflect.DeepEqual(prior, message) {
			protected[message.ConversationID] = true
		}
	}
	latest := make(map[string]time.Time)
	for id, conversation := range next.conversations {
		if conversation.Profile == "terminal" && !protected[id] {
			latest[id] = maxTime(conversation.CreatedAt, conversation.UpdatedAt)
		}
	}
	for _, message := range next.messages {
		if last, exists := latest[message.ConversationID]; exists {
			latest[message.ConversationID] = maxTime(last, maxTime(message.CreatedAt, message.UpdatedAt))
		}
	}
	ids := make([]string, 0, len(latest))
	for id := range latest {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if latest[ids[i]].Equal(latest[ids[j]]) {
			return ids[i] < ids[j]
		}
		return latest[ids[i]].Before(latest[ids[j]])
	})
	cutoff := now.Add(-time.Duration(p.retention.KeepDays) * 24 * time.Hour)
	removed := make(map[string]struct{})
	for _, id := range ids {
		if len(removed) == 100 || (!latest[id].Before(cutoff) && (!pressure || reclaim <= 0)) {
			break
		}
		removed[id] = struct{}{}
		reclaim -= p.archiveSizes[eventArchiveName(id)+".jsonl"]
	}
	removeConversationSubgraphs(next, removed)
	result := make([]string, 0, len(removed))
	for id := range removed {
		result = append(result, id)
	}
	p.prunePending = len(removed) == 100
	return result
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func (p *jsonlPersistence) checkSpace(additional, retained int64, newWork, shrinking bool) error {
	free, err := p.freeBytes(filepath.Dir(p.journalPath))
	if err != nil {
		return fmt.Errorf("%w: inspect free disk space: %v", ports.ErrCapacity, err)
	}
	// Reserve 10% of the quota (at most one journal record) for in-flight work.
	reserve := min(p.retention.MaxBytes/10, int64(maxJournalRecordBytes))
	minimum := p.retention.MinFreeBytes
	limit := p.retention.MaxBytes
	if newWork {
		minimum += reserve
		limit -= reserve
	}
	if shrinking {
		// Cleanup may use the low-water reserve, but must fit its entire temporary
		// snapshot before replacing the only authoritative journal.
		minimum = 0
	}
	// Compaction of an already oversized journal may temporarily exceed quota,
	// but only when it reduces retained bytes and the disk has scratch space.
	if free < additional || free-additional < minimum || (!shrinking && p.diskBytes+additional > limit) || (retained > limit && !shrinking) {
		return fmt.Errorf("%w: usage=%d, retained=%d, write=%d, free=%d", ports.ErrCapacity, p.diskBytes, retained, additional, free)
	}
	return nil
}

func archiveMatches(path string, events []domain.DomainEvent) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), maxJournalRecordBytes)
	for _, event := range events {
		raw, err := json.Marshal(event)
		if err != nil || !scanner.Scan() || !bytes.Equal(scanner.Bytes(), raw) {
			return false
		}
	}
	return !scanner.Scan() && scanner.Err() == nil
}

// A committed snapshot is authoritative if a crash interrupted archive removal.
// Also discard only this store's abandoned atomic-write temporary files.
func (p *jsonlPersistence) cleanStartupFiles(events map[string]domain.DomainEvent) error {
	expected := make(map[string]bool)
	for _, event := range events {
		if event.ConversationID != "" {
			expected[eventArchiveName(event.ConversationID)+".jsonl"] = true
		}
	}
	for _, dir := range []string{filepath.Dir(p.journalPath), p.eventDir} {
		file, err := os.Open(dir)
		if err != nil {
			return err
		}
		for {
			entries, readErr := file.ReadDir(128)
			for _, entry := range entries {
				name := entry.Name()
				temporary := strings.HasPrefix(name, ".runtime-") && dir != p.eventDir || strings.HasPrefix(name, ".events-") && dir == p.eventDir
				orphan := dir == p.eventDir && strings.HasSuffix(name, ".jsonl") && !expected[name]
				if !entry.IsDir() && (temporary || orphan) {
					if err = os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
						_ = file.Close()
						return err
					}
				}
			}
			if readErr != nil {
				_ = file.Close()
				if readErr != io.EOF {
					return readErr
				}
				break
			}
		}
	}
	return nil
}

func (p *jsonlPersistence) removeArchives(ids []string) error {
	for _, id := range ids {
		name := eventArchiveName(id) + ".jsonl"
		if err := os.Remove(filepath.Join(p.eventDir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
		p.diskBytes -= p.archiveSizes[name]
		delete(p.archiveSizes, name)
	}
	return nil
}
