package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver/koko/internal/agentapi"
	"github.com/jumpserver/koko/pkg/logger"
)

const (
	defaultEventCapacity       = 512
	DefaultSessionLimit        = 128
	MinSessionLimit            = 1
	MaxSessionLimit            = 4096
	DefaultArchiveMaxFiles     = 10_000
	MinArchiveMaxFiles         = 1
	MaxArchiveMaxFiles         = 100_000
	DefaultArchiveMaxBytes     = 10 * 1024 * 1024 * 1024
	MinArchiveMaxBytes         = maxJSONLFileBytes
	MaxArchiveMaxBytes         = 1024 * 1024 * 1024 * 1024
	DefaultIdleSessionTTL      = 30 * time.Minute
	MinIdleSessionTTL          = time.Minute
	MaxIdleSessionTTL          = 24 * time.Hour
	DefaultClosedLogRetention  = 24 * time.Hour
	archiveMaintenanceInterval = 5 * time.Minute
)

type Options struct {
	DataDir         string
	InstanceID      string
	EventCapacity   int
	MaxSessions     int
	ArchiveMaxFiles int
	ArchiveMaxBytes int64
	IdleSessionTTL  time.Duration
	ModelFactory    ModelFactory
}

type Service struct {
	dataDir        string
	instanceID     string
	eventCapacity  int
	maxSessions    int
	archives       *archiveStore
	idleSessionTTL time.Duration
	modelFactory   ModelFactory

	mu                  sync.RWMutex
	sessions            map[string]*agentSession
	resources           map[string]string
	sessionRemoved      func(agentapi.Principal, string)
	degraded            int
	closed              bool
	stopped             bool
	maintenanceStop     chan struct{}
	maintenanceDone     chan struct{}
	maintenanceStopOnce sync.Once
}

func New(options Options) (*Service, error) {
	if strings.TrimSpace(options.DataDir) == "" {
		return nil, fmt.Errorf("Koko agent data directory is required")
	}
	if options.EventCapacity <= 0 {
		options.EventCapacity = defaultEventCapacity
	}
	if options.MaxSessions == 0 {
		options.MaxSessions = DefaultSessionLimit
	}
	if options.MaxSessions < MinSessionLimit || options.MaxSessions > MaxSessionLimit {
		return nil, fmt.Errorf(
			"Koko agent max sessions must be between %d and %d",
			MinSessionLimit, MaxSessionLimit,
		)
	}
	if options.ArchiveMaxFiles == 0 {
		options.ArchiveMaxFiles = DefaultArchiveMaxFiles
	}
	if options.ArchiveMaxFiles < MinArchiveMaxFiles || options.ArchiveMaxFiles > MaxArchiveMaxFiles {
		return nil, fmt.Errorf(
			"Koko agent archive max files must be between %d and %d",
			MinArchiveMaxFiles, MaxArchiveMaxFiles,
		)
	}
	if options.ArchiveMaxBytes == 0 {
		options.ArchiveMaxBytes = DefaultArchiveMaxBytes
	}
	if options.ArchiveMaxBytes < MinArchiveMaxBytes || options.ArchiveMaxBytes > MaxArchiveMaxBytes {
		return nil, fmt.Errorf(
			"Koko agent archive max bytes must be between %d and %d",
			MinArchiveMaxBytes, MaxArchiveMaxBytes,
		)
	}
	if options.IdleSessionTTL == 0 {
		options.IdleSessionTTL = DefaultIdleSessionTTL
	}
	if options.IdleSessionTTL < MinIdleSessionTTL || options.IdleSessionTTL > MaxIdleSessionTTL {
		return nil, fmt.Errorf(
			"Koko agent idle session TTL must be between %s and %s",
			MinIdleSessionTTL, MaxIdleSessionTTL,
		)
	}
	if options.ModelFactory == nil {
		return nil, fmt.Errorf("Koko agent runtime dependency is required")
	}
	if options.InstanceID == "" {
		var err error
		options.InstanceID, err = randomID()
		if err != nil {
			return nil, err
		}
	}
	dataDir, err := filepath.Abs(filepath.Clean(options.DataDir))
	if err != nil {
		return nil, err
	}
	eventsDir := filepath.Join(dataDir, "events")
	if err = os.MkdirAll(eventsDir, 0o700); err != nil {
		return nil, fmt.Errorf("create Koko agent data directory: %w", err)
	}
	if err = os.Chmod(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("protect Koko agent data directory: %w", err)
	}
	if err = os.Chmod(eventsDir, 0o700); err != nil {
		return nil, fmt.Errorf("protect Koko agent event directory: %w", err)
	}
	if _, _, cleanupErr := pruneClosedLogFiles(
		eventsDir, time.Now().Add(-DefaultClosedLogRetention),
	); cleanupErr != nil {
		logger.Warnf("Clean expired agent event logs before startup failed: %s", cleanupErr)
	}
	archives, err := openArchiveStore(
		eventsDir, options.ArchiveMaxFiles, options.ArchiveMaxBytes,
	)
	if err != nil {
		return nil, err
	}
	service := &Service{
		dataDir: dataDir, instanceID: options.InstanceID,
		eventCapacity: options.EventCapacity, maxSessions: options.MaxSessions,
		archives: archives, idleSessionTTL: options.IdleSessionTTL,
		modelFactory: options.ModelFactory,
		sessions:     make(map[string]*agentSession), resources: make(map[string]string),
	}
	if err = service.restoreSessions(); err != nil {
		service.Close()
		return nil, err
	}
	service.startMaintenance()
	return service, nil
}

func (s *Service) startMaintenance() {
	s.maintenanceStop = make(chan struct{})
	s.maintenanceDone = make(chan struct{})
	s.runMaintenance(time.Now())
	go func() {
		ticker := time.NewTicker(archiveMaintenanceInterval)
		defer ticker.Stop()
		defer close(s.maintenanceDone)
		for {
			select {
			case now := <-ticker.C:
				s.runMaintenance(now)
			case <-s.maintenanceStop:
				return
			}
		}
	}()
}

func (s *Service) stopMaintenance() {
	if s.maintenanceStop == nil || s.maintenanceDone == nil {
		return
	}
	s.maintenanceStopOnce.Do(func() { close(s.maintenanceStop) })
	<-s.maintenanceDone
}

func (s *Service) runMaintenance(now time.Time) {
	removed, err := s.archives.pruneClosedBefore(now.Add(-DefaultClosedLogRetention))
	if err != nil {
		logger.Warnf("Clean expired agent event logs failed: %s", err)
		s.mu.Lock()
		s.degraded++
		s.mu.Unlock()
	} else if removed > 0 {
		logger.Infof("Cleaned %d expired agent event log(s)", removed)
	}
	s.reapIdleSessions(now, s.maxSessions)
}

func (s *Service) BeginShutdown() error {
	s.stopMaintenance()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	sessions := make([]*agentSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mu.Unlock()
	var shutdownErrors []error
	for _, session := range sessions {
		if err := session.beginShutdown("Koko agent runtime shutting down"); err != nil {
			shutdownErrors = append(shutdownErrors, err)
		}
	}
	return errors.Join(shutdownErrors...)
}

func (s *Service) Close() {
	_ = s.BeginShutdown()
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	sessions := s.sessions
	s.sessions = make(map[string]*agentSession)
	s.resources = make(map[string]string)
	s.mu.Unlock()
	for _, session := range sessions {
		session.stop()
	}
}

func (s *Service) reapIdleSessions(now time.Time, needed int) int {
	if needed <= 0 {
		return 0
	}
	s.mu.RLock()
	candidates := make([]*agentSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		candidates = append(candidates, session)
	}
	s.mu.RUnlock()
	cutoff := now.Add(-s.idleSessionTTL)
	reclaimed := 0
	for _, session := range candidates {
		removed, err := session.deleteIfIdleWithArchive(cutoff, s.archives)
		if err != nil {
			s.mu.Lock()
			s.degraded++
			s.mu.Unlock()
		}
		if !removed {
			continue
		}
		removedFromRegistry := false
		s.mu.Lock()
		if s.sessions[session.id] == session {
			delete(s.sessions, session.id)
			if s.resources[session.resourceID] == session.id {
				delete(s.resources, session.resourceID)
			}
			reclaimed++
			removedFromRegistry = true
		}
		s.mu.Unlock()
		if removedFromRegistry {
			s.notifySessionRemoved(session.principal, session.resourceID)
		}
		if reclaimed >= needed {
			break
		}
	}
	return reclaimed
}

func (s *Service) restoreSessions() error {
	paths, err := filepath.Glob(filepath.Join(s.dataDir, "events", "*"+activeLogSuffix))
	if err != nil {
		return err
	}
	if len(paths) > s.maxSessions {
		return fmt.Errorf(
			"agent event store has %d active sessions but the configured maximum is %d",
			len(paths), s.maxSessions,
		)
	}
	for _, path := range paths {
		events, loaded, openErr := openEventLog(path, s.eventCapacity)
		if openErr != nil {
			if errors.Is(openErr, errEventLogCorrupt) {
				if quarantineErr := s.archives.quarantinePath(path); quarantineErr != nil {
					return fmt.Errorf("quarantine corrupt agent event store: %w", quarantineErr)
				}
				s.degraded++
				continue
			}
			return fmt.Errorf("open agent event store: %w", openErr)
		}
		session, deleted, restoreErr := restoreAgentSession(events, loaded, s.modelFactory)
		if restoreErr != nil {
			_ = events.close()
			if !errors.Is(restoreErr, errEventLogCorrupt) {
				if errors.Is(restoreErr, errRuntimeUnavailable) {
					return fmt.Errorf("restore agent session runtime: %w", errRuntimeUnavailable)
				}
				return fmt.Errorf("restore agent session: %w", restoreErr)
			}
			if quarantineErr := s.archives.quarantine(events); quarantineErr != nil {
				return fmt.Errorf("quarantine invalid agent session: %w", quarantineErr)
			}
			s.degraded++
			continue
		}
		if deleted {
			if closeErr := events.close(); closeErr != nil {
				s.degraded++
				continue
			}
			if archiveErr := s.archives.archive(events); archiveErr != nil {
				return fmt.Errorf("archive restored closed agent session: %w", archiveErr)
			}
			continue
		}
		if _, exists := s.sessions[session.id]; exists {
			session.stop()
			if quarantineErr := s.archives.quarantine(events); quarantineErr != nil {
				return fmt.Errorf("quarantine duplicate agent session: %w", quarantineErr)
			}
			s.degraded++
			continue
		}
		if _, exists := s.resources[session.resourceID]; exists {
			session.stop()
			if quarantineErr := s.archives.quarantine(events); quarantineErr != nil {
				return fmt.Errorf("quarantine duplicate resource session: %w", quarantineErr)
			}
			s.degraded++
			continue
		}
		s.sessions[session.id] = session
		s.resources[session.resourceID] = session.id
	}
	return nil
}
