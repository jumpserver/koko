package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var errArchiveStoreFull = errors.New("agent archive store reached its quota")

type archiveStore struct {
	mu        sync.Mutex
	directory string
	maxFiles  int
	maxBytes  int64
	files     int
	bytes     int64
}

type archiveReservation struct {
	store *archiveStore
	bytes int64
	done  bool
}

func openArchiveStore(directory string, maxFiles int, maxBytes int64) (*archiveStore, error) {
	store := &archiveStore{directory: directory, maxFiles: maxFiles, maxBytes: maxBytes}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("scan agent archive store: %w", err)
	}
	for _, entry := range entries {
		if !isRetainedLogName(entry.Name()) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil, fmt.Errorf("inspect agent archive %q: %w", entry.Name(), infoErr)
		}
		if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxJSONLFileBytes {
			return nil, fmt.Errorf("agent archive %q is not a valid event log", entry.Name())
		}
		if store.files >= maxFiles || info.Size() > maxBytes-store.bytes {
			return nil, fmt.Errorf(
				"%w: configured maximum is %d files and %d bytes",
				errArchiveStoreFull, maxFiles, maxBytes,
			)
		}
		store.files++
		store.bytes += info.Size()
	}
	return store, nil
}

func pruneClosedLogFiles(directory string, cutoff time.Time) (int, int64, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, 0, err
	}
	removed := 0
	var removedBytes int64
	var cleanupErrors []error
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), closedLogSuffix) {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			cleanupErrors = append(cleanupErrors, infoErr)
			continue
		}
		if !info.Mode().IsRegular() || info.ModTime().After(cutoff) {
			continue
		}
		if removeErr := os.Remove(filepath.Join(directory, entry.Name())); removeErr != nil {
			cleanupErrors = append(cleanupErrors, removeErr)
			continue
		}
		removed++
		removedBytes += info.Size()
	}
	return removed, removedBytes, errors.Join(cleanupErrors...)
}

func (s *archiveStore) pruneClosedBefore(cutoff time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed, removedBytes, err := pruneClosedLogFiles(s.directory, cutoff)
	s.files -= removed
	if s.files < 0 {
		s.files = 0
	}
	s.bytes -= removedBytes
	if s.bytes < 0 {
		s.bytes = 0
	}
	return removed, err
}

func isRetainedLogName(name string) bool {
	return strings.HasSuffix(name, closedLogSuffix) ||
		(strings.HasSuffix(name, ".jsonl") && strings.Contains(name, ".quarantined-"))
}

func (s *archiveStore) archive(log *eventLog) error {
	return s.retain(log, (*eventLog).archiveClosedWithAllowance)
}

func (s *archiveStore) quarantine(log *eventLog) error {
	return s.retain(log, (*eventLog).quarantineWithAllowance)
}

func (s *archiveStore) retain(
	log *eventLog,
	move func(*eventLog, func(int64) error) (bool, int64, error),
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	retained, size, err := move(log, func(size int64) error {
		if s.files >= s.maxFiles || size > s.maxBytes-s.bytes {
			return errArchiveStoreFull
		}
		return nil
	})
	if err != nil {
		return err
	}
	if retained {
		s.files++
		s.bytes += size
	}
	return nil
}

func (s *archiveStore) quarantinePath(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !strings.HasSuffix(path, activeLogSuffix) {
		return fmt.Errorf("quarantine path is not an active agent event log")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxJSONLFileBytes {
		return fmt.Errorf("quarantine agent event log: invalid active file")
	}
	if s.files >= s.maxFiles || info.Size() > s.maxBytes-s.bytes {
		return errArchiveStoreFull
	}
	target := quarantineTarget(path)
	if _, err = os.Lstat(target); err == nil {
		return fmt.Errorf("quarantined agent event log already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = os.Rename(path, target); err != nil {
		return fmt.Errorf("quarantine agent event log: %w", err)
	}
	s.files++
	s.bytes += info.Size()
	return nil
}

func (s *archiveStore) reserveClose() (*archiveReservation, error) {
	// An active JSONL can still receive an in-flight result that does not take
	// the session lock. Reserving its hard per-file maximum prevents any such
	// append from making concurrent archive accounting exceed the byte quota.
	reserved := int64(maxJSONLFileBytes)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.files >= s.maxFiles || reserved > s.maxBytes-s.bytes {
		return nil, errArchiveStoreFull
	}
	s.files++
	s.bytes += reserved
	return &archiveReservation{store: s, bytes: reserved}, nil
}

func (r *archiveReservation) release() {
	if r == nil {
		return
	}
	r.store.mu.Lock()
	if !r.done {
		r.store.files--
		r.store.bytes -= r.bytes
		r.done = true
	}
	r.store.mu.Unlock()
}

func (r *archiveReservation) archive(log *eventLog) error {
	return r.retain(log, (*eventLog).archiveClosedWithAllowance)
}

func (r *archiveReservation) quarantine(log *eventLog) error {
	return r.retain(log, (*eventLog).quarantineWithAllowance)
}

func (r *archiveReservation) retain(
	log *eventLog,
	move func(*eventLog, func(int64) error) (bool, int64, error),
) error {
	if r == nil {
		return fmt.Errorf("agent archive reservation is required")
	}
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if r.done {
		return fmt.Errorf("agent archive reservation is no longer active")
	}
	retained, size, err := move(log, func(size int64) error {
		extra := size - r.bytes
		if extra > 0 && extra > r.store.maxBytes-r.store.bytes {
			return errArchiveStoreFull
		}
		return nil
	})
	if err != nil {
		r.store.files--
		r.store.bytes -= r.bytes
		r.done = true
		return err
	}
	if retained {
		r.store.bytes += size - r.bytes
	} else {
		r.store.files--
		r.store.bytes -= r.bytes
	}
	r.done = true
	return nil
}
