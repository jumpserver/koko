package agentexec

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	bolt "go.etcd.io/bbolt"
)

var (
	actionsBucket    = []byte("actions")
	eventsBucket     = []byte("events")
	eventIndexBucket = []byte("event_index")
	sessionsBucket   = []byte("sessions")
	metadataBucket   = []byte("metadata")
	logicalSizeKey   = []byte("logical_size")
	keyCheckKey      = []byte("key_check")
	ErrJournalFull   = errors.New("audit journal is full; new operations are disabled")
	ErrEvidenceLimit = errors.New("operation evidence limit reached")
)

const maxEventBytes = 128 << 10
const maxEvidenceBytes = 16 << 20

type Event struct {
	EventID      string         `json:"event_id"`
	ActionID     string         `json:"action_id"`
	Sequence     uint64         `json:"sequence"`
	State        string         `json:"state"`
	Payload      map[string]any `json:"payload"`
	OccurredAt   time.Time      `json:"occurred_at"`
	SessionToken string         `json:"session_token,omitempty"`
	Key          uint64         `json:"-"`
}

type record struct {
	Action        Action    `json:"action"`
	SessionToken  string    `json:"session_token"`
	Sequence      uint64    `json:"sequence"`
	PendingEvents uint64    `json:"pending_events,omitempty"`
	EvidenceBytes uint64    `json:"evidence_bytes"`
	Dispatched    bool      `json:"dispatched"`
	Artifact      string    `json:"artifact,omitempty"`
	Size          int64     `json:"size,omitempty"`
	SHA256        string    `json:"sha256,omitempty"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// Journal atomically persists each state change and its outbound audit event.
// Values are encrypted because retry records include scoped Core capabilities.
// bbolt's default synchronous commits are intentionally never disabled.
type Journal struct {
	db           *bolt.DB
	aead         cipher.AEAD
	maxBytes     int64
	scanMu       sync.Mutex
	scanAfter    string
	pendingMu    sync.Mutex
	pendingAfter string
}

func OpenJournal(directory string, maxBytes int64) (*Journal, error) {
	if maxBytes < 16<<20 {
		return nil, errors.New("audit journal capacity must be at least 16 MiB")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(directory, 0700); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(directory, "journal.db")
	info, err := os.Stat(dbPath)
	newDatabase := os.IsNotExist(err)
	if err != nil && !newDatabase {
		return nil, err
	}
	if !newDatabase && info.Size() == 0 {
		return nil, errors.New("existing audit journal is empty")
	}
	keyPath := filepath.Join(directory, "journal.key")
	key, err := os.ReadFile(keyPath)
	if os.IsNotExist(err) {
		if !newDatabase {
			return nil, errors.New("existing audit journal requires its original key")
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		var f *os.File
		f, err = os.OpenFile(keyPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			_, err = f.Write(key)
			if err == nil {
				err = f.Sync()
			}
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
			if err == nil {
				err = syncDirectory(directory)
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(keyPath); err != nil || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("audit key must be accessible only to its owner")
	}
	block, err := aes.NewCipher(key)
	clear(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	j := &Journal{db: db, aead: aead, maxBytes: maxBytes}
	if newDatabase {
		err = db.Update(func(tx *bolt.Tx) error {
			for _, bucket := range [][]byte{actionsBucket, eventsBucket, eventIndexBucket, sessionsBucket, metadataBucket} {
				if _, err := tx.CreateBucket(bucket); err != nil {
					return err
				}
			}
			check, err := j.seal("jumpserver.agent.journal")
			if err != nil {
				return err
			}
			meta := tx.Bucket(metadataBucket)
			if err := meta.Put(keyCheckKey, check); err != nil {
				return err
			}
			return meta.Put(logicalSizeKey, binary.BigEndian.AppendUint64(nil, 0))
		})
	} else {
		err = db.View(func(tx *bolt.Tx) error {
			for _, bucket := range [][]byte{actionsBucket, eventsBucket, eventIndexBucket, sessionsBucket, metadataBucket} {
				if tx.Bucket(bucket) == nil {
					return errors.New("invalid audit journal structure")
				}
			}
			meta := tx.Bucket(metadataBucket)
			if len(meta.Get(logicalSizeKey)) != 8 {
				return errors.New("invalid audit capacity accounting")
			}
			// Validate the key even when no operations have been recorded. Counters
			// are already persisted atomically with their records; never rebuild them.
			var check string
			if err := j.open(meta.Get(keyCheckKey), &check); err != nil {
				return err
			}
			if check != "jumpserver.agent.journal" {
				return errors.New("invalid audit journal key check")
			}
			return nil
		})
	}
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return j, nil
}

func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

func (j *Journal) seal(value any) ([]byte, error) {
	plain, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	defer clear(plain)
	nonce := make([]byte, j.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return j.aead.Seal(nonce, nonce, plain, nil), nil
}

func (j *Journal) open(value []byte, out any) error {
	n := j.aead.NonceSize()
	if len(value) < n {
		return errors.New("invalid audit journal entry")
	}
	plain, err := j.aead.Open(nil, value[:n], value[n:], nil)
	if err != nil {
		return errors.New("audit journal authentication failed")
	}
	defer clear(plain)
	return json.Unmarshal(plain, out)
}

func entryBytes(key, value []byte) uint64 {
	if value == nil {
		return 0
	}
	// Include a conservative fixed allowance for bbolt key/leaf bookkeeping.
	return uint64(len(key) + len(value) + 128)
}

func (j *Journal) put(tx *bolt.Tx, bucket, key, value []byte, terminal bool) error {
	b := tx.Bucket(bucket)
	meta := tx.Bucket(metadataBucket)
	used := binary.BigEndian.Uint64(meta.Get(logicalSizeKey))
	old := entryBytes(key, b.Get(key))
	if old > used {
		return errors.New("invalid audit capacity accounting")
	}
	next := used - old + entryBytes(key, value)
	limit := uint64(j.maxBytes * 9 / 10)
	if terminal {
		limit = uint64(j.maxBytes)
	}
	// Deletion and shrinking remain possible when the capacity is exhausted.
	if next > limit && next > used {
		return ErrJournalFull
	}
	var err error
	if value == nil {
		err = b.Delete(key)
	} else {
		err = b.Put(key, value)
	}
	if err != nil {
		return err
	}
	return meta.Put(logicalSizeKey, binary.BigEndian.AppendUint64(nil, next))
}

func (j *Journal) PutAction(action Action, sessionToken string) error {
	return j.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(actionsBucket)
		if b.Get([]byte(action.ID)) != nil {
			return nil
		}
		data, err := j.seal(record{Action: action, SessionToken: sessionToken, ExpiresAt: time.Now().Add(24 * time.Hour)})
		if err != nil {
			return err
		}
		return j.put(tx, actionsBucket, []byte(action.ID), data, false)
	})
}

func (j *Journal) Action(id string) (rec record, err error) {
	err = j.db.View(func(tx *bolt.Tx) error {
		data := tx.Bucket(actionsBucket).Get([]byte(id))
		if data == nil {
			return os.ErrNotExist
		}
		return j.open(data, &rec)
	})
	return
}

func (j *Journal) update(id string, change func(*record) error) error {
	return j.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(actionsBucket)
		data := b.Get([]byte(id))
		if data == nil {
			return os.ErrNotExist
		}
		var rec record
		if err := j.open(data, &rec); err != nil {
			return err
		}
		if err := change(&rec); err != nil {
			return err
		}
		data, err := j.seal(rec)
		if err != nil {
			return err
		}
		return j.put(tx, actionsBucket, []byte(id), data, terminalState(rec.Action.Status))
	})
}

func (j *Journal) Intent(id string) error {
	return j.update(id, func(rec *record) error {
		if terminalState(rec.Action.Status) {
			return errors.New("operation is already terminal")
		}
		if rec.Dispatched {
			return errors.New("operation has already been dispatched")
		}
		rec.Dispatched = true
		return nil
	})
}

func (j *Journal) Append(id, state string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	return j.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(actionsBucket)
		var rec record
		data := b.Get([]byte(id))
		if data == nil {
			return os.ErrNotExist
		}
		if err := j.open(data, &rec); err != nil {
			return err
		}
		if terminalState(rec.Action.Status) {
			return errors.New("operation is already terminal")
		}
		sequenceLimit := uint64(999999)
		evidenceLimit := uint64(maxEvidenceBytes - maxEventBytes)
		if terminalState(state) {
			sequenceLimit++
			evidenceLimit += maxEventBytes
		}
		if rec.Sequence >= sequenceLimit {
			return ErrEvidenceLimit
		}
		rec.Sequence++
		rec.PendingEvents++
		rec.Action.LastSequence = rec.Sequence
		rec.Action.Status = state
		rec.Action.AuditStatus = "pending"
		if terminalState(state) || state == "awaiting_input" {
			rec.Action.Result = payload
		}
		event := Event{EventID: uuid.NewString(), ActionID: id, Sequence: rec.Sequence,
			State: state, Payload: payload, OccurredAt: time.Now().UTC(), SessionToken: rec.SessionToken}
		plain, err := json.Marshal(event)
		if err != nil {
			return err
		}
		tooLarge := len(plain) > maxEventBytes
		// Count the complete encoded event, including its capability and metadata,
		// as a conservative bound for Core's JSON payload plus occurrence time.
		// This protects small output chunks too, and survives delivery/restarts.
		rec.EvidenceBytes += uint64(len(plain))
		clear(plain)
		if tooLarge {
			return errors.New("audit event exceeds 128 KiB limit")
		}
		if rec.EvidenceBytes > evidenceLimit {
			return ErrEvidenceLimit
		}
		data, err = j.seal(event)
		if err != nil {
			return err
		}
		events := tx.Bucket(eventsBucket)
		seq, err := events.NextSequence()
		if err != nil {
			return err
		}
		if seq == 0 {
			return errors.New("journal event sequence exhausted")
		}
		if err := j.put(tx, eventsBucket, binary.BigEndian.AppendUint64(nil, seq), data, terminalState(state)); err != nil {
			return err
		}
		if err := j.put(tx, eventIndexBucket, eventIndexKey(id, rec.Sequence), binary.BigEndian.AppendUint64(nil, seq), terminalState(state)); err != nil {
			return err
		}
		data, err = j.seal(rec)
		if err != nil {
			return err
		}
		return j.put(tx, actionsBucket, []byte(id), data, terminalState(state))
	})
}

func eventIndexKey(id string, sequence uint64) []byte {
	return binary.BigEndian.AppendUint64(append([]byte(id), 0), sequence)
}

// Pending rotates between actions while preserving each action's event order.
// The index avoids scanning a failed action's entire backlog to reach another.
func (j *Journal) Pending(limit int) (events []Event, err error) {
	if limit < 1 || limit > 1024 {
		return nil, errors.New("audit event page limit must be between 1 and 1024")
	}
	j.pendingMu.Lock()
	defer j.pendingMu.Unlock()
	first := ""
	err = j.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(eventIndexBucket).Cursor()
		key, _ := cursor.First()
		if j.pendingAfter != "" {
			key, _ = cursor.Seek([]byte(j.pendingAfter + "\x01"))
			if key == nil {
				key, _ = cursor.First()
			}
		}
		var ids []string
		for key != nil && len(ids) < min(limit, 16) {
			if len(key) < 10 || key[len(key)-9] != 0 {
				return errors.New("invalid audit event index")
			}
			id := string(key[:len(key)-9])
			if id == first {
				break
			}
			if first == "" {
				first = id
			}
			ids = append(ids, id)
			key, _ = cursor.Seek([]byte(id + "\x01"))
			if key == nil {
				key, _ = cursor.First()
			}
		}
		for index, id := range ids {
			prefix := append([]byte(id), 0)
			budget := (limit - len(events)) / (len(ids) - index)
			for key, value := cursor.Seek(prefix); key != nil && bytes.HasPrefix(key, prefix) && budget > 0; key, value = cursor.Next() {
				if len(value) != 8 {
					return errors.New("invalid audit event index")
				}
				var event Event
				if err := j.open(tx.Bucket(eventsBucket).Get(value), &event); err != nil {
					return err
				}
				if event.ActionID != id || !bytes.Equal(key, eventIndexKey(id, event.Sequence)) {
					return errors.New("audit event index mismatch")
				}
				event.Key = binary.BigEndian.Uint64(value)
				events = append(events, event)
				budget--
			}
		}
		return nil
	})
	if err == nil {
		// Rotate the first attempted action even if Core consumes the entire
		// flush deadline before the rest of this page can be delivered.
		j.pendingAfter = first
	}
	return
}

// ActionEvents exposes only bounded, undelivered output. Core authorizes the
// corresponding action before the runtime merges this page with delivered data.
func (j *Journal) ActionEvents(id string, after uint64, limit int) ([]Event, error) {
	if limit < 1 || limit > 32 {
		return nil, errors.New("action event page limit must be between 1 and 32")
	}
	var events []Event
	err := j.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(eventsBucket).Cursor()
		for key, value := cursor.First(); key != nil && len(events) < limit; key, value = cursor.Next() {
			var event Event
			if err := j.open(value, &event); err != nil {
				return err
			}
			if event.ActionID == id && event.Sequence > after {
				event.SessionToken = ""
				events = append(events, event)
			}
		}
		return nil
	})
	return events, err
}

func (j *Journal) Ack(event Event) error {
	return j.db.Update(func(tx *bolt.Tx) error {
		key := binary.BigEndian.AppendUint64(nil, event.Key)
		stored := tx.Bucket(eventsBucket).Get(key)
		if stored == nil {
			return nil
		}
		var original Event
		if err := j.open(stored, &original); err != nil {
			return err
		}
		if original.EventID != event.EventID || original.ActionID != event.ActionID || original.Sequence != event.Sequence {
			return errors.New("audit acknowledgement does not match persisted event")
		}
		b := tx.Bucket(actionsBucket)
		var rec record
		if err := j.open(b.Get([]byte(event.ActionID)), &rec); err != nil {
			return err
		}
		if rec.PendingEvents == 0 {
			return errors.New("invalid pending audit event count")
		}
		if err := j.put(tx, eventsBucket, key, nil, true); err != nil {
			return err
		}
		if err := j.put(tx, eventIndexBucket, eventIndexKey(event.ActionID, event.Sequence), nil, true); err != nil {
			return err
		}
		rec.PendingEvents--
		if rec.PendingEvents == 0 {
			rec.Action.AuditStatus = "delivered"
			if terminalState(rec.Action.Status) {
				rec.ExpiresAt = time.Now().Add(24 * time.Hour)
			}
		}
		data, err := j.seal(rec)
		if err != nil {
			return err
		}
		return j.put(tx, actionsBucket, []byte(event.ActionID), data, true)
	})
}

// SyncCore accepts an authoritative Core state without emitting it back to
// Core as a new event. Local evidence must be delivered before reconciliation.
// A false result means no local record, an ineligible state, or pending evidence.
func (j *Journal) SyncCore(action Action) (bool, error) {
	if !terminalState(action.Status) && action.Status != "awaiting_input" {
		return false, nil
	}
	adopted := false
	err := j.db.Update(func(tx *bolt.Tx) error {
		data := tx.Bucket(actionsBucket).Get([]byte(action.ID))
		if data == nil {
			return nil
		}
		var rec record
		if err := j.open(data, &rec); err != nil {
			return err
		}
		if rec.PendingEvents != 0 {
			return nil
		}
		if terminalState(rec.Action.Status) && !terminalState(action.Status) {
			return nil
		}
		if action.SessionID != "" && rec.Action.SessionID != action.SessionID {
			return errors.New("Core action does not match the local session")
		}
		if terminalState(action.Status) && (!terminalState(rec.Action.Status) || rec.Action.AuditStatus != "delivered" || rec.ExpiresAt.IsZero()) {
			rec.ExpiresAt = time.Now().Add(24 * time.Hour)
		}
		rec.Action.Status = action.Status
		rec.Action.Result = action.Result
		rec.Action.ApprovalURL = action.ApprovalURL
		rec.Action.CancelRequested = action.CancelRequested
		rec.Action.AuditStatus = "delivered"
		rec.Sequence = max(rec.Sequence, action.LastSequence)
		rec.Action.LastSequence = rec.Sequence
		data, err := j.seal(rec)
		if err != nil {
			return err
		}
		if err = j.put(tx, actionsBucket, []byte(action.ID), data, true); err != nil {
			return err
		}
		adopted = true
		return nil
	})
	return adopted && err == nil, err
}

// Prune deletes at most 128 completed records per call, only after every audit
// event was acknowledged and its 24-hour retention elapsed. Returned artifact
// paths may be deleted by the runtime after the transaction commits.
func (j *Journal) Prune(now time.Time) ([]string, error) {
	var artifacts []string
	err := j.db.Update(func(tx *bolt.Tx) error {
		var ids []string
		cursor := tx.Bucket(actionsBucket).Cursor()
		for key, data := cursor.First(); key != nil && len(ids) < 128; key, data = cursor.Next() {
			var rec record
			if err := j.open(data, &rec); err != nil {
				return err
			}
			if !terminalState(rec.Action.Status) || rec.PendingEvents != 0 || rec.Action.AuditStatus != "delivered" || rec.ExpiresAt.IsZero() || rec.ExpiresAt.After(now) {
				continue
			}
			ids = append(ids, string(key))
			if rec.Artifact != "" {
				artifacts = append(artifacts, rec.Artifact)
			}
		}
		for _, id := range ids {
			if err := j.put(tx, actionsBucket, []byte(id), nil, true); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return artifacts, nil
}

func (j *Journal) SaveSession(session Session) error {
	return j.db.Update(func(tx *bolt.Tx) error {
		data, err := j.seal(session)
		if err != nil {
			return err
		}
		return j.put(tx, sessionsBucket, []byte(session.ID), data, false)
	})
}

func (j *Journal) DeleteSession(id string) error {
	return j.db.Update(func(tx *bolt.Tx) error { return j.put(tx, sessionsBucket, []byte(id), nil, true) })
}

func (j *Journal) Sessions() (sessions []Session, err error) {
	err = j.db.View(func(tx *bolt.Tx) error {
		return tx.Bucket(sessionsBucket).ForEach(func(_, data []byte) error {
			var session Session
			if err := j.open(data, &session); err != nil {
				return err
			}
			sessions = append(sessions, session)
			return nil
		})
	})
	return
}

// Unfinished rotates through bounded pages so long-lived human handoffs cannot
// starve later actions that need cancellation or terminal-state reconciliation.
func (j *Journal) Unfinished(limit int) ([]record, error) {
	if limit < 1 || limit > 128 {
		return nil, errors.New("unfinished action page limit must be between 1 and 128")
	}
	j.scanMu.Lock()
	defer j.scanMu.Unlock()
	var records []record
	last := j.scanAfter
	err := j.db.View(func(tx *bolt.Tx) error {
		cursor := tx.Bucket(actionsBucket).Cursor()
		key, value := cursor.First()
		if last != "" {
			key, value = cursor.Seek([]byte(last))
			if string(key) == last {
				key, value = cursor.Next()
			}
		}
		for ; key != nil && len(records) < limit; key, value = cursor.Next() {
			last = string(key)
			var rec record
			if err := j.open(value, &rec); err != nil {
				return err
			}
			if !terminalState(rec.Action.Status) {
				records = append(records, rec)
			}
		}
		if key == nil {
			last = ""
		}
		return nil
	})
	if err == nil {
		j.scanAfter = last
	}
	return records, err
}

func (j *Journal) Recover() error {
	last := ""
	for {
		var unfinished []struct {
			id         string
			dispatched bool
		}
		done := false
		if err := j.db.View(func(tx *bolt.Tx) error {
			cursor := tx.Bucket(actionsBucket).Cursor()
			key, data := cursor.First()
			if last != "" {
				key, data = cursor.Seek([]byte(last))
				if string(key) == last {
					key, data = cursor.Next()
				}
			}
			for ; key != nil && len(unfinished) < 64; key, data = cursor.Next() {
				last = string(key)
				var rec record
				if err := j.open(data, &rec); err != nil {
					return err
				}
				if !terminalState(rec.Action.Status) && rec.Action.Status != "awaiting_input" {
					unfinished = append(unfinished, struct {
						id         string
						dispatched bool
					}{rec.Action.ID, rec.Dispatched})
				}
			}
			done = key == nil
			return nil
		}); err != nil {
			return err
		}
		for _, rec := range unfinished {
			state := "expired"
			if rec.dispatched {
				state = "unknown"
			}
			if err := j.Append(rec.id, state, map[string]any{"error": "execution node restarted; the operation was not replayed"}); err != nil {
				return fmt.Errorf("recover operation %s: %w", rec.id, err)
			}
		}
		if done {
			return nil
		}
	}
}

func (j *Journal) Close() error { return j.db.Close() }
