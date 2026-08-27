package fileai

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type testFileExecutor struct {
	stat      func(string) (Entry, error)
	read      func(string) (TextResult, error)
	readCalls int
	savedWith string
}

func (*testFileExecutor) ListDirectory(context.Context, string, int) (DirectoryResult, error) {
	return DirectoryResult{}, errors.New("unexpected list_directory")
}

func (e *testFileExecutor) Stat(_ context.Context, path string) (Entry, error) {
	return e.stat(path)
}

func (e *testFileExecutor) ReadText(_ context.Context, path string, _ int64) (TextResult, error) {
	e.readCalls++
	if e.read != nil {
		return e.read(path)
	}
	return TextResult{}, errors.New("unexpected read_text")
}

func (e *testFileExecutor) SaveText(_ context.Context, path, _ string, expectedVersion string) (Entry, error) {
	e.savedWith = expectedVersion
	return Entry{Path: path, Exists: true, Version: "sha256:saved"}, nil
}

func (*testFileExecutor) Mkdir(context.Context, string) error {
	return errors.New("unexpected mkdir")
}

func (*testFileExecutor) Rename(context.Context, string, string) error {
	return errors.New("unexpected rename")
}

func (*testFileExecutor) Delete(context.Context, string) error {
	return errors.New("unexpected delete")
}

func autoApprovingTestSession(executor Executor) *builtInSession {
	var session *builtInSession
	session = &builtInSession{
		executor: executor,
		emit: func(message ChatMessage) {
			for _, part := range message.Parts {
				if part.Type != "data-file-approval" {
					continue
				}
				data, _ := part.Data.(map[string]any)
				if data["state"] != "awaiting_approval" {
					continue
				}
				session.mu.Lock()
				pending := session.pending
				session.mu.Unlock()
				pending.decision <- approvalDecision{Decision: "approve"}
			}
		},
	}
	return session
}

func TestPrepareActionAndApprovalDigest(t *testing.T) {
	action, err := prepareAction(Action{
		Tool: ToolRename, Path: "/var/log/app.log",
		DestinationPath: "app.old", Rationale: "rotate",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if action.DestinationPath != "/var/log/app.old" {
		t.Fatalf("destinationPath = %q", action.DestinationPath)
	}
	digest := approvalDigest("approval-1", action)
	changed := action
	changed.Path = "/var/log/other.log"
	if digest == approvalDigest("approval-1", changed) {
		t.Fatal("approval digest did not bind the action")
	}
}

func TestPrepareActionResolvesRelativePathAgainstCurrentDirectory(t *testing.T) {
	context := map[string]any{"currentPath": "/tmp"}
	action, err := prepareAction(Action{Tool: ToolStat, Path: "a.txt"}, context)
	if err != nil {
		t.Fatal(err)
	}
	if action.Path != "/tmp/a.txt" {
		t.Fatalf("relative action path = %q", action.Path)
	}

	absolute, err := prepareAction(Action{Tool: ToolStat, Path: "/a.txt"}, context)
	if err != nil {
		t.Fatal(err)
	}
	if absolute.Path != "/a.txt" {
		t.Fatalf("absolute action path = %q", absolute.Path)
	}
}

func TestNormalizeDecisionUsesKindAsDiscriminator(t *testing.T) {
	expectedAction := Action{
		Tool: ToolSaveText, Path: "/root/a.txt", Content: "aaa",
		ExpectedVersion: ExpectedVersionAbsent, Rationale: "create file",
	}
	actionDecision := normalizeDecision(Decision{
		Kind: "action", Answer: "I will create the file.",
		Action: expectedAction,
	})
	if actionDecision.Answer != "" {
		t.Fatalf("action answer = %q", actionDecision.Answer)
	}
	if actionDecision.Action != expectedAction {
		t.Fatalf("action changed = %#v", actionDecision.Action)
	}
	if err := validateDecision(actionDecision); err != nil {
		t.Fatal(err)
	}

	answerDecision := normalizeDecision(Decision{
		Kind: "answer", Answer: "The file is ready.",
		Action: Action{Tool: ToolDelete, Path: "/root/a.txt"},
	})
	if answerDecision.Action != (Action{}) {
		t.Fatalf("answer action = %#v", answerDecision.Action)
	}
	if err := validateDecision(answerDecision); err != nil {
		t.Fatal(err)
	}

	invalidDecisions := []Decision{
		{Kind: "unknown"},
		{Kind: "answer"},
		{Kind: "action", Answer: "ignored", Action: Action{Tool: "unsupported"}},
	}
	for _, decision := range invalidDecisions {
		if err := validateDecision(normalizeDecision(decision)); err == nil {
			t.Fatalf("invalid decision was accepted: %#v", decision)
		}
	}
}

func TestSaveRequiresMatchingPrecondition(t *testing.T) {
	results := []ActionResult{{
		Tool: ToolReadText, Path: "/etc/app.conf", Outcome: "success",
		Details: TextResult{
			Path: "/etc/app.conf", Exists: true, Version: "sha256:current",
		},
	}}
	if !hasMatchingSavePrecondition(results, "/etc/app.conf", "sha256:current") {
		t.Fatal("matching approved read result was not accepted")
	}
	if hasMatchingSavePrecondition(results, "/etc/app.conf", "sha256:stale") {
		t.Fatal("stale read result was accepted")
	}
}

func TestSaveBindsConfirmedAbsentVersion(t *testing.T) {
	results := []ActionResult{{
		Tool: ToolStat, Path: "/root/a.txt", Outcome: "success",
		Details: Entry{
			Path: "/root/a.txt", Exists: false, Version: ExpectedVersionAbsent,
		},
	}}
	action := Action{Tool: ToolSaveText, Path: "/root/a.txt", Content: "aaa"}
	bindSaveExpectedVersion(&action, results)
	if action.ExpectedVersion != ExpectedVersionAbsent {
		t.Fatalf("expectedVersion = %q", action.ExpectedVersion)
	}
	if !hasMatchingSavePrecondition(results, action.Path, action.ExpectedVersion) {
		t.Fatal("confirmed absent file was not accepted as a create precondition")
	}
	if hasMatchingSavePrecondition(results, "/root/other.txt", ExpectedVersionAbsent) {
		t.Fatal("absent observation for another path was accepted")
	}
	readResults := []ActionResult{{
		Tool: ToolReadText, Path: "/root/a.txt", Outcome: "success",
		Details: TextResult{
			Path: "/root/a.txt", Exists: false, Version: ExpectedVersionAbsent,
		},
	}}
	action.ExpectedVersion = ""
	bindSaveExpectedVersion(&action, readResults)
	if action.ExpectedVersion != ExpectedVersionAbsent {
		t.Fatalf("read not-found expectedVersion = %q", action.ExpectedVersion)
	}
	inconsistentReadResults := []ActionResult{{
		Tool: ToolReadText, Path: "/root/a.txt", Outcome: "success",
		Details: TextResult{
			Path: "/root/a.txt", Exists: false, Version: "sha256:unexpected",
		},
	}}
	if hasMatchingSavePrecondition(inconsistentReadResults, "/root/a.txt", "sha256:unexpected") {
		t.Fatal("inconsistent missing-file observation was accepted")
	}
}

func TestAbsentFileIsStructuredObservation(t *testing.T) {
	details, ok := absentObservationDetails(Action{Tool: ToolStat, Path: "/root/a.txt"})
	if !ok {
		t.Fatal("stat not-found was not recognized")
	}
	entry, ok := details.(Entry)
	if !ok || entry.Exists || entry.Version != ExpectedVersionAbsent {
		t.Fatalf("details = %#v", details)
	}
}

func TestStatNotFoundReturnsSuccessfulAbsentObservation(t *testing.T) {
	executor := &testFileExecutor{
		stat: func(string) (Entry, error) { return Entry{}, os.ErrNotExist },
	}
	session := &builtInSession{executor: executor, emit: func(ChatMessage) {}}
	result, err := session.executeAction(context.Background(), "file-1", Action{
		ID: "action-1", Tool: ToolStat, Path: "/root/a.txt",
	})
	if err != nil || result.Outcome != "success" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	entry, ok := result.Details.(Entry)
	if !ok || entry.Exists || entry.Version != ExpectedVersionAbsent {
		t.Fatalf("details = %#v", result.Details)
	}
}

func TestStatPermissionDeniedIsNotAnAbsentObservation(t *testing.T) {
	permissionErr := errors.New("outside configured SFTP root: permission denied")
	executor := &testFileExecutor{
		stat: func(string) (Entry, error) { return Entry{}, permissionErr },
	}
	session := &builtInSession{executor: executor, emit: func(ChatMessage) {}}
	result, err := session.executeAction(context.Background(), "file-1", Action{
		ID: "action-1", Tool: ToolStat, Path: "/root/a.txt",
	})
	if !errors.Is(err, permissionErr) || result.Outcome != "error" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if result.Details != nil {
		t.Fatalf("permission error details = %#v", result.Details)
	}
}

func TestReadNotFoundReturnsSuccessfulAbsentObservation(t *testing.T) {
	executor := &testFileExecutor{
		read: func(string) (TextResult, error) { return TextResult{}, os.ErrNotExist },
	}
	session := autoApprovingTestSession(executor)
	result, err := session.executeAction(context.Background(), "file-1", Action{
		ID: "action-1", Tool: ToolReadText, Path: "/root/a.txt",
	})
	if err != nil || result.Outcome != "success" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	text, ok := result.Details.(TextResult)
	if !ok || text.Exists || text.Version != ExpectedVersionAbsent {
		t.Fatalf("details = %#v", result.Details)
	}
}

func TestSaveCreatesAbsentFileWithoutReadingIt(t *testing.T) {
	executor := &testFileExecutor{
		stat: func(string) (Entry, error) { return Entry{}, os.ErrNotExist },
	}
	session := autoApprovingTestSession(executor)
	result, err := session.executeAction(context.Background(), "file-1", Action{
		ID: "action-1", Tool: ToolSaveText, Path: "/root/a.txt",
		Content: "aaa", ExpectedVersion: ExpectedVersionAbsent,
	})
	if err != nil || result.Outcome != "success" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
	if executor.readCalls != 0 {
		t.Fatalf("read_text calls = %d", executor.readCalls)
	}
	if executor.savedWith != ExpectedVersionAbsent {
		t.Fatalf("saved expectedVersion = %q", executor.savedWith)
	}
}

func TestPrepareActionRejectsUnsafeWrite(t *testing.T) {
	_, err := prepareAction(Action{
		Tool: ToolSaveText, Path: "/", Content: "x", ExpectedVersion: "v1",
	}, nil)
	if err == nil {
		t.Fatal("root write was accepted")
	}
	_, err = prepareAction(Action{
		Tool: ToolDelete, Path: ".", Recursive: true,
	}, map[string]any{"currentPath": "/tmp"})
	if err == nil {
		t.Fatal("current-directory write was accepted")
	}
	_, err = prepareAction(Action{
		Tool: ToolDelete, Path: "../outside", Recursive: true,
	}, nil)
	if err == nil {
		t.Fatal("parent path segment was accepted")
	}
}

func TestResolveApprovalRequiresDigest(t *testing.T) {
	pending := &pendingApproval{
		id: "approval-1", digest: "expected", targetID: "file-1",
		decision: make(chan approvalDecision, 1),
	}
	session := &builtInSession{pending: pending}
	if err := session.resolveApproval("file-1", approvalDecision{
		ID: "approval-1", Digest: "wrong", Decision: "approve",
	}); err == nil {
		t.Fatal("mismatched approval digest was accepted")
	}
	if err := session.resolveApproval("file-1", approvalDecision{
		ID: "approval-1", Digest: "expected", Decision: "approve",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestMaxTextObservationPreservesVersion(t *testing.T) {
	version := "sha256:current"
	encoded, err := encodeModelObservations([]ActionResult{{
		Tool: ToolReadText, Path: "/etc/app.conf", Outcome: "success",
		Details: TextResult{
			Path: "/etc/app.conf", Exists: true, Content: strings.Repeat("x", MaxTextBytes),
			Version: version,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(encoded, `"version":"`+version+`"`) {
		t.Fatal("max-size read observation lost its version")
	}
}

func TestSessionCancelAndCloseAreIdempotent(t *testing.T) {
	lifetimeCtx, cancelLife := context.WithCancel(context.Background())
	session := &builtInSession{
		lifetimeCtx: lifetimeCtx,
		cancelLife:  cancelLife,
	}
	session.Cancel()
	session.Cancel()
	session.Close()
	session.Close()
	if !session.closed {
		t.Fatal("session was not marked closed")
	}
	select {
	case <-lifetimeCtx.Done():
	default:
		t.Fatal("session lifetime was not cancelled")
	}
}
