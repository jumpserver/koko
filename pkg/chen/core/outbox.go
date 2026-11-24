package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver-dev/sdk-go/model"
	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"
)

// A file represents a recoverable unit. Cleanup operations share one file so a
// process crash cannot persist only half the local intention to close a session.
type envelope struct {
	Version      int               `json:"version"`
	Command      *model.Command    `json:"command,omitempty"`
	CommandSaved bool              `json:"command_saved,omitempty"`
	Lifecycle    *lifecycleRequest `json:"lifecycle,omitempty"`
	Finish       *finishRequest    `json:"finish,omitempty"`
	Replay       *replayRequest    `json:"replay,omitempty"`
	TicketCancel *model.ReqInfo    `json:"ticket_cancel,omitempty"`
}

type lifecycleRequest struct {
	SessionID string               `json:"session_id"`
	Event     model.LifecycleEvent `json:"event"`
	Reason    string               `json:"reason,omitempty"`
}

type finishRequest struct {
	SessionID string `json:"session_id"`
	DateEnd   int64  `json:"date_end"`
	Connected bool   `json:"connected"`
}

type replayRequest struct {
	SessionID string `json:"session_id"`
	Uploaded  bool   `json:"uploaded,omitempty"`
	Confirmed bool   `json:"confirmed,omitempty"`
	Size      int64  `json:"size,omitempty"`
}

const nativeOutboxVersion = 2

var errLegacyOutbox = errors.New("existing Chen audit data belongs to an earlier deployment. Drain its pending commands, replays and session cleanup using that deployment before reusing CHEN_STATE_DIR")

type outbox struct {
	owner *bolt.DB
	dir   string
	mu    sync.Mutex
}

func newOutbox(dir string) (*outbox, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("audit_spool_directory_required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, errors.Join(ErrAuditDisk, err)
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("invalid_audit_spool_directory")
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, errors.Join(ErrAuditDisk, err)
	}
	ownerPath := filepath.Join(dir, ".koko-owner.db")
	if info, err := os.Lstat(ownerPath); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("invalid_audit_spool_owner_file")
	}
	// Check write/fsync ability at startup, before accepting any query.
	probe, err := os.CreateTemp(dir, ".probe-")
	if err != nil {
		return nil, errors.Join(ErrAuditDisk, err)
	}
	err = probe.Sync()
	err = errors.Join(err, probe.Close(), os.Remove(probe.Name()))
	if err != nil {
		return nil, errors.Join(ErrAuditDisk, err)
	}
	// The existing bbolt store supplies a crash-released exclusive file lock
	// and a durable marker distinguishing native replays from pre-migration casts.
	owner, err := bolt.Open(ownerPath, 0600, &bolt.Options{Timeout: 100 * time.Millisecond})
	if errors.Is(err, bolterrors.ErrTimeout) {
		return nil, errors.New("Chen audit state is already in use by another KoKo instance")
	}
	if err != nil {
		return nil, errors.Join(ErrAuditDisk, err)
	}
	return &outbox{dir: dir, owner: owner}, nil
}

func (o *outbox) close() error { return o.owner.Close() }

func (o *outbox) claimReplays(replayFiles []os.DirEntry) error {
	return o.owner.Update(func(tx *bolt.Tx) error {
		bucket := []byte("native-outbox-v2")
		if tx.Bucket(bucket) != nil {
			return nil
		}
		for _, file := range replayFiles {
			if strings.HasSuffix(file.Name(), ".cast") || strings.HasSuffix(file.Name(), ".cast.gz") {
				return errLegacyOutbox
			}
		}
		_, err := tx.CreateBucket(bucket)
		return err
	})
}

// A previous Chen deployment used different command and cleanup fields in
// version 1. Reject it before decoding into native SDK models.
func readEnvelope(path string) (envelope, error) {
	var entry envelope
	info, err := os.Lstat(path)
	if err != nil {
		return entry, err
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		return entry, errors.New("audit_spool_file_invalid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return entry, err
	}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return entry, err
	}
	switch header.Version {
	case 1:
		return entry, errLegacyOutbox
	case nativeOutboxVersion:
		if err := json.Unmarshal(data, &entry); err != nil {
			return entry, err
		}
	default:
		return entry, fmt.Errorf("unsupported Chen audit outbox version %d", header.Version)
	}
	return entry, nil
}

func closeEnvelope(g *Grant, reason string, connected bool) envelope {
	result := envelope{Finish: &finishRequest{SessionID: g.ID, DateEnd: time.Now().Unix(), Connected: connected}}
	if connected {
		result.Lifecycle = &lifecycleRequest{SessionID: g.ID, Event: model.AssetConnectFinished, Reason: reason}
	}
	return result
}

func (o *outbox) put(value envelope) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	id, err := uuid.NewRandom()
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%020d-%s.json", time.Now().UnixNano(), id)
	return o.write(name, value)
}

// write performs file+directory fsync before acknowledging durable acceptance.
// SQL text is audit data; every temporary and final file is owner-readable only.
func (o *outbox) write(name string, value envelope) error {
	value.Version = nativeOutboxVersion
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(o.dir, ".pending-")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp, filepath.Join(o.dir, name)); err != nil {
		return err
	}
	return syncDirectory(o.dir)
}

func syncDirectory(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func (p *CoreProvider) flush(ctx context.Context) {
	p.flushMu.Lock()
	defer p.flushMu.Unlock()
	files, err := os.ReadDir(p.spool.dir) // sorted filenames preserve local order.
	if err != nil {
		p.report("audit_spool_read_failed")
		return
	}
	config := sync.OnceValues(p.client.GetTerminalConfig)
	blockedCommands := make(map[string]bool)
	for _, file := range files {
		if ctx.Err() != nil {
			return
		}
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		path := filepath.Join(p.spool.dir, file.Name())
		entry, err := readEnvelope(path)
		if err != nil {
			p.report("audit_spool_file_invalid")
			continue
		}
		commandSession := ""
		if entry.Command != nil {
			commandSession = entry.Command.SessionID
			if blockedCommands[commandSession] {
				continue
			}
		}
		if err := p.deliver(ctx, &entry, config); err != nil {
			// Successful sub-operations are checkpointed. Unknown remote outcomes
			// may be retried when Core accepted a request but its response was lost.
			p.spool.mu.Lock()
			writeErr := p.spool.write(file.Name(), entry)
			p.spool.mu.Unlock()
			if writeErr != nil {
				p.report("audit_spool_checkpoint_failed")
			}
			p.report("audit_delivery_deferred")
			// Preserve this session's command order while allowing unrelated
			// sessions and resource cleanup to make progress during an outage.
			if commandSession != "" {
				blockedCommands[commandSession] = true
			}
			continue
		}
		if entry.Replay != nil {
			// Commit the remote confirmation before unlinking either replay file.
			// A restart can finish local cleanup even if both files are already gone.
			if err := p.spool.write(file.Name(), entry); err != nil {
				p.report("audit_spool_checkpoint_failed")
				return
			}
			if err := p.replays.remove(entry.Replay.SessionID); err != nil {
				p.report("audit_replay_cleanup_deferred")
				continue
			}
		}
		if err := os.Remove(path); err != nil {
			p.report("audit_spool_remove_failed")
			return
		}
		if err := syncDirectory(p.spool.dir); err != nil {
			p.report("audit_spool_sync_failed")
			return
		}
	}
}
