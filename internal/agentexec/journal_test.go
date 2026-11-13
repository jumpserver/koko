package agentexec

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func TestJournalRestartPreservesDispatchAndAuditIdentity(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"started", "waiting", "human"} {
		if err := j.PutAction(Action{ID: id, Status: "queued"}, "private-session-capability"); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Intent("started"); err != nil {
		t.Fatal(err)
	}
	if err := j.Intent("started"); err == nil {
		t.Fatal("allowed repeat dispatch")
	}
	if err := j.Append("started", "running", map[string]any{"data": "private-output"}); err != nil {
		t.Fatal(err)
	}
	if err := j.Intent("human"); err != nil {
		t.Fatal(err)
	}
	if err := j.Append("human", "awaiting_input", map[string]any{"handoff_url": "/human-action"}); err != nil {
		t.Fatal(err)
	}
	before, err := j.Pending(10)
	if err != nil || len(before) != 2 {
		t.Fatalf("pending events: %v", err)
	}
	var accounting []byte
	if err := j.db.View(func(tx *bolt.Tx) error {
		accounting = bytes.Clone(tx.Bucket(metadataBucket).Get(logicalSizeKey))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("private-session-capability")) || bytes.Contains(data, []byte("private-output")) {
		t.Fatal("journal leaked plaintext")
	}
	j, err = OpenJournal(dir, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := j.db.View(func(tx *bolt.Tx) error {
		if !bytes.Equal(accounting, tx.Bucket(metadataBucket).Get(logicalSizeKey)) {
			t.Error("reopening changed persisted capacity accounting")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if rec, err := j.Action("started"); err != nil || rec.PendingEvents != 1 {
		t.Fatal("reopening lost the persisted pending event count")
	}
	if err := j.Recover(); err != nil {
		t.Fatal(err)
	}
	waiting, err := j.Unfinished(1)
	if err != nil || len(waiting) != 1 || waiting[0].Action.ID != "human" {
		t.Fatal("recovery lost its human reconciliation record")
	}
	for id, want := range map[string]string{"started": "unknown", "waiting": "expired", "human": "awaiting_input"} {
		rec, err := j.Action(id)
		if err != nil || rec.Action.Status != want {
			t.Fatalf("%s status: %#v, %v", id, rec.Action, err)
		}
		if id == "human" && rec.Action.Result["handoff_url"] != "/human-action" {
			t.Fatal("lost human handoff result")
		}
	}
	if err := j.Intent("waiting"); err == nil {
		t.Fatal("dispatched a terminal action")
	}
	if err := j.Recover(); err != nil {
		t.Fatal(err)
	}
	after, err := j.Pending(10)
	if err != nil || len(after) != 4 {
		t.Fatalf("recovery duplicated events: %d, %v", len(after), err)
	}
	for _, original := range before {
		found := false
		for _, event := range after {
			if event.EventID == original.EventID && event.Sequence == original.Sequence && event.Key == original.Key {
				found = true
			}
		}
		if !found {
			t.Fatal("retry changed persisted event identity")
		}
	}
	for _, event := range after {
		if err := j.Ack(event); err != nil {
			t.Fatal(err)
		}
		if err := j.Ack(event); err != nil {
			t.Fatal("acknowledgement is not idempotent")
		}
	}
}

func TestJournalRejectsInvalidStructureAndKeysWithoutRepair(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*bolt.Tx) error
		damage string
	}{
		{"missing bucket", func(tx *bolt.Tx) error { return tx.DeleteBucket(eventsBucket) }, ""},
		{"missing event index", func(tx *bolt.Tx) error { return tx.DeleteBucket(eventIndexBucket) }, ""},
		{"missing accounting", func(tx *bolt.Tx) error { return tx.Bucket(metadataBucket).Delete(logicalSizeKey) }, ""},
		{"invalid accounting", func(tx *bolt.Tx) error { return tx.Bucket(metadataBucket).Put(logicalSizeKey, []byte{0}) }, ""},
		{"missing key check", func(tx *bolt.Tx) error { return tx.Bucket(metadataBucket).Delete(keyCheckKey) }, ""},
		{"missing key", nil, "remove"},
		{"wrong key", nil, "replace"},
		{"empty database", nil, "truncate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			j, err := OpenJournal(dir, 16<<20)
			if err != nil {
				t.Fatal(err)
			}
			if test.change != nil {
				err = j.db.Update(test.change)
			}
			closeErr := j.Close()
			if err != nil || closeErr != nil {
				t.Fatalf("prepare invalid journal: %v, %v", err, closeErr)
			}
			keyPath := filepath.Join(dir, "journal.key")
			dbPath := filepath.Join(dir, "journal.db")
			switch test.damage {
			case "remove":
				err = os.Remove(keyPath)
			case "replace":
				err = os.WriteFile(keyPath, bytes.Repeat([]byte{1}, 32), 0600)
			case "truncate":
				err = os.Truncate(dbPath, 0)
			}
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			if reopened, err := OpenJournal(dir, 16<<20); err == nil {
				_ = reopened.Close()
				t.Fatal("opened an invalid journal")
			}
			after, err := os.ReadFile(dbPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("failed opening modified the existing journal")
			}
			if test.damage == "remove" {
				if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
					t.Fatal("replaced a missing key for an existing journal")
				}
			}
		})
	}
}

func TestJournalCoreSyncNeverDropsUndeliveredEvidence(t *testing.T) {
	j, err := OpenJournal(t.TempDir(), 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := j.PutAction(Action{ID: "action", Status: "queued"}, "capability"); err != nil {
		t.Fatal(err)
	}
	if err := j.Append("action", "running", map[string]any{"output": "evidence"}); err != nil {
		t.Fatal(err)
	}
	if err := j.Append("action", "awaiting_input", map[string]any{"handoff_url": "/human"}); err != nil {
		t.Fatal(err)
	}
	core := Action{ID: "action", Status: "succeeded", Result: map[string]any{"terminal_session_id": "human-session"}}
	if adopted, err := j.SyncCore(core); err != nil || adopted {
		t.Fatal("Core sync discarded local evidence")
	}
	events, _ := j.Pending(10)
	local, err := j.ActionEvents("action", 0, 1)
	if err != nil || len(local) != 1 || local[0].Sequence != 1 || local[0].SessionToken != "" {
		t.Fatal("local event pagination leaked a capability or lost its sequence")
	}
	local, err = j.ActionEvents("action", 1, 32)
	if err != nil || len(local) != 1 || local[0].Sequence != 2 {
		t.Fatal("local event cursor was not respected")
	}
	bad := events[0]
	bad.EventID = "wrong-event"
	if err := j.Ack(bad); err == nil {
		t.Fatal("accepted a mismatched acknowledgement")
	}
	if err := j.Ack(events[1]); err != nil {
		t.Fatal(err)
	}
	rec, _ := j.Action("action")
	if rec.PendingEvents != 1 || rec.Action.AuditStatus == "delivered" {
		t.Fatal("out-of-order acknowledgement lost pending evidence")
	}
	if adopted, _ := j.SyncCore(core); adopted {
		t.Fatal("adopted with an older unacknowledged event")
	}
	if err := j.Ack(events[0]); err != nil {
		t.Fatal(err)
	}
	core.LastSequence = 3
	if adopted, err := j.SyncCore(core); err != nil || !adopted {
		t.Fatalf("Core sync: %v", err)
	}
	if events, _ := j.Pending(10); len(events) != 0 {
		t.Fatal("Core terminal state was emitted as a duplicate event")
	}
	rec, _ = j.Action("action")
	if rec.Action.Result["terminal_session_id"] != "human-session" || rec.Action.LastSequence != 3 || rec.Sequence != 3 {
		t.Fatal("lost authoritative terminal result")
	}
	if err := j.Append("action", "failed", nil); err == nil {
		t.Fatal("overwrote a terminal state")
	}
	if adopted, err := j.SyncCore(Action{ID: "action", Status: "awaiting_input"}); err != nil || adopted {
		t.Fatal("stale Core response reopened a completed handoff")
	}
	if err := j.PutAction(Action{ID: "bounded", Status: "queued"}, "capability"); err != nil {
		t.Fatal(err)
	}
	if err := j.Append("bounded", "running", map[string]any{"data": strings.Repeat("x", maxEventBytes)}); err == nil {
		t.Fatal("accepted oversized event")
	}
	rec, _ = j.Action("bounded")
	if rec.Sequence != 0 || rec.PendingEvents != 0 {
		t.Fatal("oversized event left a partial state change")
	}
	if err := j.PutAction(Action{ID: "later", Status: "queued"}, "capability"); err != nil {
		t.Fatal(err)
	}
	first, err := j.Unfinished(1)
	if err != nil || len(first) != 1 {
		t.Fatal("missing unfinished page")
	}
	second, err := j.Unfinished(1)
	if err != nil || len(second) != 1 || second[0].Action.ID == first[0].Action.ID {
		t.Fatal("unfinished pagination starved later operations")
	}
}

func TestJournalPruneReleasesCapacityOnlyAfterDeliveryAndRetention(t *testing.T) {
	j, err := OpenJournal(t.TempDir(), 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	// A small runtime budget exercises exhaustion without filling disk or RAM.
	j.maxBytes = 8192
	if err := j.PutAction(Action{ID: "action", Status: "queued", Request: map[string]any{"command": strings.Repeat("x", 5000)}}, "capability"); err != nil {
		t.Fatal(err)
	}
	if err := j.update("action", func(rec *record) error { rec.Artifact = "/journal/files/action.data"; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := j.Append("action", "succeeded", nil); err != nil {
		t.Fatal(err)
	}
	if paths, err := j.Prune(time.Now().Add(48 * time.Hour)); err != nil || len(paths) != 0 {
		t.Fatal("pruned undelivered evidence")
	}
	second := Action{ID: "next", Status: "queued", Request: map[string]any{"command": strings.Repeat("y", 3000)}}
	if err := j.PutAction(second, "capability"); !errors.Is(err, ErrJournalFull) {
		t.Fatalf("expected full journal, got %v", err)
	}
	events, _ := j.Pending(10)
	if err := j.Ack(events[0]); err != nil {
		t.Fatal(err)
	}
	if paths, err := j.Prune(time.Now().Add(23 * time.Hour)); err != nil || len(paths) != 0 {
		t.Fatal("pruned before retention elapsed")
	}
	paths, err := j.Prune(time.Now().Add(25 * time.Hour))
	if err != nil || len(paths) != 1 || paths[0] != "/journal/files/action.data" {
		t.Fatalf("artifact cleanup: %v %v", paths, err)
	}
	if _, err := j.Action("action"); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("retained pruned record")
	}
	if err := j.PutAction(second, "capability"); err != nil {
		t.Fatalf("deleted pages did not release capacity: %v", err)
	}
}

func TestJournalEvidenceLimitsReserveFinalReceipt(t *testing.T) {
	for _, limit := range []string{"bytes", "sequence"} {
		t.Run(limit, func(t *testing.T) {
			j, err := OpenJournal(t.TempDir(), 16<<20)
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			if err = j.PutAction(Action{ID: "limited", Status: "running"}, "capability"); err != nil {
				t.Fatal(err)
			}
			if err = j.update("limited", func(rec *record) error {
				if limit == "bytes" {
					rec.EvidenceBytes = maxEvidenceBytes - maxEventBytes - 1
				} else {
					rec.Sequence = 999999
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before, _ := j.Action("limited")
			if err = j.Append("limited", "running", map[string]any{"data": "x"}); !errors.Is(err, ErrEvidenceLimit) {
				t.Fatalf("small output escaped the evidence budget: %v", err)
			}
			after, _ := j.Action("limited")
			if after.Sequence != before.Sequence || after.EvidenceBytes != before.EvidenceBytes || after.PendingEvents != 0 {
				t.Fatal("rejected output changed its durable state")
			}
			if err = j.Append("limited", "unknown", map[string]any{"output_truncated": true}); err != nil {
				t.Fatal("exhaustion consumed the final receipt reserve", err)
			}
			events, err := j.Pending(1)
			if err != nil || len(events) != 1 || events[0].State != "unknown" {
				t.Fatal("missing final evidence", err)
			}
			if err = j.Ack(events[0]); err != nil {
				t.Fatal(err)
			}
			after, _ = j.Action("limited")
			if after.EvidenceBytes <= before.EvidenceBytes {
				t.Fatal("delivery released the per-action evidence budget")
			}
		})
	}
}
