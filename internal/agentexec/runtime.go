package agentexec

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/internal/assetexec"
	"github.com/jumpserver/koko/pkg/logger"
	"github.com/jumpserver/koko/pkg/session"
	gossh "golang.org/x/crypto/ssh"
)

type Options struct {
	Directory      string
	MaxSessions    int
	MaxPending     int
	MaxRunning     int
	MaxOutputBytes int64
	MaxFileBytes   int64
	JournalBytes   int64
	IdleTimeout    time.Duration
}

func (o *Options) defaults() {
	if o.MaxSessions <= 0 {
		o.MaxSessions = 128
	}
	if o.MaxPending <= 0 {
		o.MaxPending = 64
	}
	if o.MaxRunning <= 0 {
		o.MaxRunning = 20
	}
	if o.MaxOutputBytes <= 0 {
		o.MaxOutputBytes = 8 << 20
	}
	if o.MaxFileBytes <= 0 {
		o.MaxFileBytes = 100 << 20
	}
	if o.JournalBytes <= 0 {
		o.JournalBytes = 256 << 20
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = 15 * time.Minute
	}
}

type liveSession struct {
	mu            sync.Mutex
	info          Session
	owner         string
	connection    *assetexec.Connection
	lastUsed      time.Time
	active        int
	closing       bool
	closingNow    bool
	key           string
	connectCancel context.CancelFunc
	gate          chan struct{}
}

type Runtime struct {
	core             Core
	jms              *service.JMService
	journal          *Journal
	opts             Options
	ctx              context.Context
	cancel           context.CancelFunc
	mu               sync.Mutex
	sessions         map[string]*liveSession
	jobs             map[string]context.CancelFunc
	running          chan struct{}
	wg               sync.WaitGroup
	flushMu          sync.Mutex
	closing          bool
	storageErr       error
	authorityAfter   string
	lastAuditFailure time.Time
}

func New(core Core, jms *service.JMService, options Options) (*Runtime, error) {
	options.defaults()
	if core == nil || jms == nil || options.Directory == "" {
		return nil, errors.New("agent runtime dependencies are required")
	}
	if options.MaxSessions > 256 || options.MaxPending > 256 || options.MaxRunning > 64 || options.MaxOutputBytes > 8<<20 || options.MaxFileBytes > 100<<20 || options.JournalBytes > 1<<30 {
		return nil, errors.New("agent capacity exceeds supported single-node limits")
	}
	journal, err := OpenJournal(options.Directory, options.JournalBytes)
	if err != nil {
		return nil, err
	}
	if err = journal.Recover(); err != nil {
		_ = journal.Close()
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(options.Directory, "files"), 0700); err != nil {
		_ = journal.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Runtime{core: core, jms: jms, journal: journal, opts: options, ctx: ctx, cancel: cancel,
		sessions: make(map[string]*liveSession), jobs: make(map[string]context.CancelFunc), running: make(chan struct{}, options.MaxRunning)}
	r.wg.Add(1)
	go r.maintain()
	return r, nil
}

func owner(p Principal) string { return p.OrgID + ":" + p.UserID + ":" + p.ClientID }

func (r *Runtime) available() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return errors.New("execution service is shutting down")
	}
	if r.storageErr != nil {
		return errors.New("audit persistence failed; new operations are disabled")
	}
	return nil
}

func (r *Runtime) finish(id, state string, result map[string]any) {
	if err := r.journal.Append(id, state, result); err != nil {
		// A concurrent Core cancellation may already have supplied the final
		// state. Never replace that state or falsely acknowledge a disk failure.
		if rec, readErr := r.journal.Action(id); readErr == nil && terminalState(rec.Action.Status) {
			return
		}
		r.mu.Lock()
		r.storageErr = err
		r.mu.Unlock()
		logger.Errorf("Agent audit persistence failed for action %s: %v", id, err)
	}
}

func (r *Runtime) Introspect(ctx context.Context, token, org, resource string) (p Principal, err error) {
	err = r.core.Call(ctx, "introspect/", map[string]any{"access_token": token, "resource": resource, "org_id": org}, &p)
	if err == nil && (p.ContextToken == "" || p.UserID == "" || p.OrgID == "" || p.ClientID == "" || p.Resource != resource) {
		err = errors.New("authorization service returned an invalid identity")
	}
	return
}

func (r *Runtime) Search(ctx context.Context, p Principal, query, assetID string, offset, limit, accountOffset int) (map[string]any, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000 || len(query) > 128 || accountOffset < 0 || accountOffset > 100000 || (assetID != "" && !identifier.MatchString(assetID)) {
		return nil, errors.New("invalid asset search bounds")
	}
	var result map[string]any
	request := map[string]any{"context_token": p.ContextToken, "search": query, "offset": offset, "limit": limit, "account_offset": accountOffset}
	if assetID != "" {
		request["asset_id"] = assetID
	}
	err := r.core.Call(ctx, "assets/", request, &result)
	return result, err
}

func (r *Runtime) Connect(ctx context.Context, p Principal, asset, account, protocol, key, remoteAddr string) (map[string]any, error) {
	if err := r.available(); err != nil {
		return nil, err
	}
	if !identifier.MatchString(asset) || account == "" || len(account) > 256 || !identifier.MatchString(key) {
		return nil, errors.New("asset, account and a stable idempotency_key are required")
	}
	switch protocol {
	case "ssh", "sftp", "mysql", "mariadb", "postgresql":
	default:
		return nil, errors.New("unsupported agent protocol")
	}
	r.mu.Lock()
	full := len(r.sessions) >= r.opts.MaxSessions
	if full {
		for _, existing := range r.sessions {
			if existing.key == key && existing.owner == owner(p) {
				full = false
				break
			}
		}
	}
	r.mu.Unlock()
	if full {
		return nil, errors.New("asset session capacity reached")
	}
	var info Session
	err := r.core.Call(ctx, "sessions/", map[string]any{"context_token": p.ContextToken, "asset_id": asset, "account": account,
		"protocol": protocol, "idempotency_key": key, "remote_addr": remoteAddr}, &info)
	if err != nil {
		return nil, err
	}
	if !identifier.MatchString(info.ID) || info.SessionToken == "" || !identifier.MatchString(info.ActionID) {
		return nil, errors.New("invalid asset session response")
	}
	info.Protocol = protocol
	r.mu.Lock()
	if existing := r.sessions[info.ID]; existing != nil {
		r.mu.Unlock()
		existing.mu.Lock()
		defer existing.mu.Unlock()
		if existing.owner != owner(p) {
			return nil, errors.New("session does not belong to this authorization")
		}
		existing.lastUsed = time.Now()
		return publicSession(existing.info), nil
	}
	// A durable operation is never opened again after its process-local
	// transport is lost, including a retry following a node restart.
	if prior, readErr := r.journal.Action(info.ActionID); readErr == nil {
		r.mu.Unlock()
		result := publicSession(info)
		result["status"] = "context_lost"
		result["connection_status"] = prior.Action.Status
		result["error"] = "connection context is unavailable; use a new idempotency key to establish a new session"
		return result, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		r.mu.Unlock()
		return nil, readErr
	}
	if len(r.sessions) >= r.opts.MaxSessions || r.closing {
		r.mu.Unlock()
		if saveErr := r.journal.PutAction(Action{ID: info.ActionID, ActionID: info.ActionID, SessionID: info.ID, Kind: "connect_asset", Status: "accepted"}, info.SessionToken); saveErr == nil {
			_ = r.journal.SaveSession(info)
			r.finish(info.ActionID, "rejected", map[string]any{"error": "asset session capacity reached"})
		}
		return nil, errors.New("asset session capacity reached")
	}
	connectCtx, connectCancel := context.WithTimeout(r.ctx, 10*time.Minute)
	s := &liveSession{info: info, owner: owner(p), key: key, lastUsed: time.Now(), active: 1, gate: make(chan struct{}, 1), connectCancel: connectCancel}
	r.sessions[info.ID] = s
	r.wg.Add(1)
	r.mu.Unlock()
	if err = r.journal.SaveSession(info); err != nil {
		connectCancel()
		r.removeSession(info.ID)
		r.wg.Done()
		return nil, err
	}
	action := Action{ID: info.ActionID, ActionID: info.ActionID, SessionID: info.ID, Kind: "connect_asset", Status: "accepted"}
	if err = r.journal.PutAction(action, info.SessionToken); err != nil {
		connectCancel()
		r.removeSession(info.ID)
		r.wg.Done()
		return nil, err
	}
	go func() {
		defer r.wg.Done()
		defer connectCancel()
		defer func() { s.mu.Lock(); s.active--; s.mu.Unlock() }()
		r.openSession(connectCtx, s, remoteAddr)
	}()
	return publicSession(info), nil
}

func publicSession(s Session) map[string]any {
	result := map[string]any{"session_id": s.ID, "action_id": s.ActionID, "status": s.Status}
	if s.Protocol != "" {
		result["protocol"] = s.Protocol
	}
	if s.Protocol == "sftp" {
		result["path_scope"] = "virtual SFTP root /; SSH paths may differ"
	}
	if s.RequiresTerminal {
		result["requires_terminal"] = true
	}
	if s.ApprovalURL != "" {
		result["approval_url"] = s.ApprovalURL
	}
	return result
}

func (r *Runtime) removeSession(id string) { r.mu.Lock(); delete(r.sessions, id); r.mu.Unlock() }

func (r *Runtime) sessionCall(ctx context.Context, s Session, operation string, extra map[string]any, result any) error {
	path, err := operationPath("sessions", s.ID, operation)
	if err != nil {
		return err
	}
	if extra == nil {
		extra = map[string]any{}
	}
	extra["session_token"] = s.SessionToken
	return r.core.Call(ctx, path, extra, result)
}

func (r *Runtime) openSession(ctx context.Context, s *liveSession, remoteAddr string) {
	s.mu.Lock()
	info := s.info
	s.mu.Unlock()
	fail := func(err error) {
		r.finish(info.ActionID, "failed", map[string]any{"error": err.Error()})
		s.mu.Lock()
		s.info.Status = "failed"
		// Keep the bounded logical session available for a human handoff even
		// when this adapter cannot establish an automatic connection.
		s.lastUsed = time.Now()
		s.mu.Unlock()
	}
	for {
		var checked Session
		if err := r.sessionCall(ctx, info, "check", nil, &checked); err != nil {
			fail(err)
			return
		}
		if checked.RequiresTerminal {
			fail(errors.New("this connection requires a controlled web terminal; call open_terminal with this session_id"))
			return
		}
		if checked.Status != "awaiting_approval" {
			break
		}
		s.mu.Lock()
		s.info.Status = checked.Status
		s.info.ApprovalURL = checked.ApprovalURL
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			fail(ctx.Err())
			return
		case <-time.After(2 * time.Second):
		}
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fail(err)
		return
	}
	defer clear(private)
	sshPublic, err := gossh.NewPublicKey(public)
	if err != nil {
		fail(err)
		return
	}
	if err = r.journal.Intent(info.ActionID); err != nil {
		fail(err)
		return
	}
	var token model.ConnectToken
	err = r.sessionCall(ctx, info, "credentials", map[string]any{"public_key": strings.TrimSpace(string(gossh.MarshalAuthorizedKey(sshPublic)))}, &token)
	if err != nil {
		fail(err)
		return
	}
	if token.Account.IsSSHCertificate() {
		token.Account.SSHCertificatePrivateKey = private
	}
	if err = r.journal.Append(info.ActionID, "running", map[string]any{"phase": "connecting"}); err != nil {
		fail(err)
		return
	}
	connection, err := assetexec.Open(ctx, &token, assetexec.Options{JMS: r.jms, RemoteAddr: remoteAddr, LoginFrom: "WT", ConnectTimeout: 30 * time.Second, MaxFileBytes: r.opts.MaxFileBytes, MaxOutputBytes: r.opts.MaxOutputBytes, MaxRows: 1000})
	if err != nil {
		fail(err)
		return
	}
	ids := connection.TerminalSessionIDs()
	terminal := token.CreateSession(remoteAddr, model.LabelField("WT"), model.NORMALType)
	managedTerminal := len(ids) == 0
	if len(ids) == 0 {
		terminal.ID = uuid.NewString()
		created, createErr := r.jms.CreateSession(terminal)
		if createErr != nil {
			_ = connection.Close()
			fail(errors.New("could not register the audited terminal session"))
			return
		}
		ids = []string{created.ID}
		terminal.ID = created.ID
		_, _ = r.jms.SessionSuccess(created.ID)
	}
	for _, id := range ids {
		if err = r.sessionCall(ctx, info, "bind", map[string]any{"terminal_session_id": id}, &map[string]any{}); err != nil {
			_ = connection.Close()
			if managedTerminal {
				_, _ = r.jms.SessionDisconnect(id)
			}
			fail(err)
			return
		}
		info.TerminalSessionID = id
	}
	if err = r.journal.SaveSession(info); err != nil {
		_ = connection.Close()
		if managedTerminal {
			_, _ = r.jms.SessionDisconnect(info.TerminalSessionID)
		}
		fail(err)
		return
	}
	if err = r.journal.Append(info.ActionID, "succeeded", map[string]any{"session_id": info.ID, "terminal_session_ids": ids}); err != nil {
		_ = connection.Close()
		fail(err)
		return
	}
	s.mu.Lock()
	s.connection = connection
	s.info.TerminalSessionID = info.TerminalSessionID
	s.info.Status = "active"
	s.lastUsed = time.Now()
	s.mu.Unlock()
	for _, id := range ids {
		terminal.ID = id
		terminal.TokenId = token.Id
		r.registerSessionControl(s, terminal)
	}
}

func (r *Runtime) getSession(p Principal, id string, requireConnection bool) (*liveSession, error) {
	r.mu.Lock()
	s := r.sessions[id]
	r.mu.Unlock()
	if s == nil {
		return nil, errors.New("asset session is unavailable; establish a new session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.owner != owner(p) {
		return nil, errors.New("session does not belong to this authorization")
	}
	if !s.info.ExpiresAt.IsZero() && !s.info.ExpiresAt.After(time.Now()) {
		s.closing = true
		return nil, errors.New("asset session expired; establish a new session")
	}
	if s.closing || (requireConnection && (s.connection == nil || s.info.Status != "active")) {
		return nil, errors.New("asset session is not ready")
	}
	s.lastUsed = time.Now()
	return s, nil
}

func (r *Runtime) Submit(ctx context.Context, p Principal, sessionID, kind, key string, request map[string]any) (Action, error) {
	if err := r.available(); err != nil {
		return Action{}, err
	}
	if !identifier.MatchString(key) {
		return Action{}, errors.New("a stable idempotency_key is required")
	}
	s, err := r.getSession(p, sessionID, kind != "open_terminal")
	if err != nil {
		return Action{}, err
	}
	if kind == "execute_command" {
		var command assetexec.Request
		if err = decodeRequest(request, &command); err != nil {
			return Action{}, err
		}
		if len(command.Command) == 0 || len(command.Command) > 4096 {
			return Action{}, errors.New("command must contain between 1 and 4096 bytes")
		}
		s.mu.Lock()
		if s.closing || s.connection == nil {
			s.mu.Unlock()
			return Action{}, errors.New("asset session is closing")
		}
		protocol := s.connection.Protocol()
		s.mu.Unlock()
		if protocol != "ssh" && (command.Cwd != "" || len(command.Env) != 0) {
			return Action{}, errors.New("cwd and env apply only to SSH")
		}
		prepared, prepareErr := assetexec.PrepareCommand(command)
		if prepareErr != nil {
			return Action{}, prepareErr
		}
		request["prepared_command"] = prepared
	}
	var action Action
	err = r.core.Call(ctx, "actions/", map[string]any{"context_token": p.ContextToken, "session_id": sessionID, "kind": kind, "idempotency_key": key, "request": request}, &action)
	if err != nil {
		return action, err
	}
	if action.ID == "" {
		return action, errors.New("invalid operation response")
	}
	action.ActionID = action.ID
	action.SessionID = sessionID
	if action.Kind == "" {
		action.Kind = kind
	}
	if action.Request == nil {
		action.Request = request
	}
	s.mu.Lock()
	info := s.info
	s.mu.Unlock()
	if err = r.journal.PutAction(action, info.SessionToken); err != nil {
		return Action{}, err
	}
	if terminalState(action.Status) {
		_, _ = r.journal.SyncCore(action)
		return action, nil
	}
	if local, readErr := r.journal.Action(action.ID); readErr == nil && (local.Dispatched || terminalState(local.Action.Status)) {
		if terminalState(local.Action.Status) || local.Action.Status == "awaiting_input" {
			action.Status, action.Result, action.AuditStatus = local.Action.Status, local.Action.Result, local.Action.AuditStatus
		}
		return action, nil
	}
	if kind == "file_upload" {
		// Upload content must be durably staged and verified before dispatch.
		authorized, err := r.authorize(ctx, action, info)
		if err == nil && authorized.Status == "queued" {
			if record, readErr := r.journal.Action(action.ID); readErr == nil && record.Artifact == "" && !record.Dispatched && !terminalState(record.Action.Status) {
				authorized.NextStep = "upload_bytes"
			}
		}
		return authorized, err
	}
	err = r.enqueue(action, s)
	if err != nil {
		r.finish(action.ID, "rejected", map[string]any{"error": err.Error()})
	}
	return action, err
}

func (r *Runtime) authorize(ctx context.Context, action Action, s Session) (Action, error) {
	path, err := operationPath("actions", action.ID, "authorize")
	if err != nil {
		return action, err
	}
	var result Action
	err = r.core.Call(ctx, path, map[string]any{"session_token": s.SessionToken}, &result)
	if result.ID == "" {
		result.ID = action.ID
	}
	result.ActionID = result.ID
	if result.SessionID == "" {
		result.SessionID = action.SessionID
	}
	if result.Kind == "" {
		result.Kind = action.Kind
	}
	if result.Request == nil {
		result.Request = action.Request
	}
	return result, err
}

func (r *Runtime) enqueue(action Action, s *liveSession) error {
	r.mu.Lock()
	if _, exists := r.jobs[action.ID]; exists {
		r.mu.Unlock()
		return nil
	}
	if len(r.jobs) >= r.opts.MaxPending || r.closing || r.storageErr != nil {
		r.mu.Unlock()
		return errors.New("operation queue capacity reached")
	}
	ctx, cancel := context.WithTimeout(r.ctx, 20*time.Minute)
	r.jobs[action.ID] = cancel
	s.mu.Lock()
	if s.closing || (s.connection == nil && action.Kind != "open_terminal") {
		s.mu.Unlock()
		delete(r.jobs, action.ID)
		r.mu.Unlock()
		cancel()
		return errors.New("asset session is closing")
	}
	s.active++
	s.mu.Unlock()
	r.wg.Add(1)
	r.mu.Unlock()
	go func() {
		defer r.wg.Done()
		defer cancel()
		defer func() {
			s.mu.Lock()
			s.active--
			s.lastUsed = time.Now()
			s.mu.Unlock()
			r.mu.Lock()
			delete(r.jobs, action.ID)
			r.mu.Unlock()
		}()
		r.run(ctx, action, s)
	}()
	return nil
}

func (r *Runtime) run(ctx context.Context, action Action, s *liveSession) {
	s.mu.Lock()
	info := s.info
	s.mu.Unlock()
	finish := func(state string, result map[string]any) { r.finish(action.ID, state, result) }
	for {
		checked, err := r.authorize(ctx, action, info)
		if err != nil {
			finish("rejected", map[string]any{"error": err.Error()})
			return
		}
		if terminalState(checked.Status) {
			_, _ = r.journal.SyncCore(checked)
			return
		}
		if checked.CancelRequested {
			finish("cancelled", map[string]any{"stop_confirmed": true})
			return
		}
		if checked.Status == "dispatching" || checked.Status == "running" || checked.Status == "awaiting_input" {
			return
		}
		if checked.Status == "queued" || checked.Status == "ready" {
			break
		}
		select {
		case <-ctx.Done():
			finish("expired", map[string]any{"error": "approval or queue timeout"})
			return
		case <-time.After(2 * time.Second):
		}
	}
	select {
	case s.gate <- struct{}{}:
		defer func() { <-s.gate }()
	case <-ctx.Done():
		finish("expired", map[string]any{"error": "session queue timeout"})
		return
	}
	// Waiting behind work on the same connection must not consume another
	// session's global execution capacity.
	select {
	case r.running <- struct{}{}:
		defer func() { <-r.running }()
	case <-ctx.Done():
		finish("expired", map[string]any{"error": "queue timeout"})
		return
	}
	s.mu.Lock()
	unavailable := action.Kind != "open_terminal" && (s.connection == nil || s.info.Status != "active")
	s.mu.Unlock()
	if unavailable {
		finish("rejected", map[string]any{"error": "asset connection context is unavailable"})
		return
	}
	if err := r.available(); err != nil {
		finish("rejected", map[string]any{"error": err.Error()})
		return
	}
	if action.Kind != "open_terminal" {
		if err := r.sessionCall(ctx, info, "check", nil, &map[string]any{}); err != nil {
			finish("rejected", map[string]any{"error": err.Error()})
			return
		}
	}
	if err := r.journal.Intent(action.ID); err != nil {
		return
	}
	path, _ := operationPath("actions", action.ID, "claim")
	var claim struct {
		Action
		Claimed bool `json:"claimed"`
	}
	if err := r.core.Call(ctx, path, map[string]any{"session_token": info.SessionToken}, &claim); err != nil {
		finish("unknown", map[string]any{"error": "dispatch claim could not be confirmed"})
		return
	}
	if !claim.Claimed {
		if terminalState(claim.Status) {
			_, _ = r.journal.SyncCore(claim.Action)
		} else if claim.Status == "awaiting_approval" || claim.Status == "queued" {
			finish("rejected", map[string]any{"error": "authorization changed before dispatch; submit a new operation after review"})
		}
		return
	}
	if action.Kind == "open_terminal" {
		finish("awaiting_input", map[string]any{"handoff_url": claim.HandoffURL, "requires_user": true})
		return
	}
	if err := r.journal.Append(action.ID, "running", map[string]any{"phase": "dispatch"}); err != nil {
		finish("failed", map[string]any{"error": "execution did not start because audit persistence failed"})
		return
	}
	timeout := 120 * time.Second
	var timing struct {
		Seconds int `json:"timeout_seconds"`
	}
	if decodeRequest(action.Request, &timing) == nil && timing.Seconds > 0 {
		timeout = time.Duration(timing.Seconds) * time.Second
	}
	if timeout > 10*time.Minute {
		timeout = 10 * time.Minute
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	guardDone := make(chan struct{})
	go r.guard(execCtx, cancel, info, action.ID, guardDone)
	defer func() { cancel(); <-guardDone }()
	result, state := r.execute(execCtx, action, s)
	finish(state, result)
}

func (r *Runtime) guard(ctx context.Context, cancel context.CancelFunc, s Session, id string, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var check struct {
				CancelRequested []string `json:"cancel_requested_actions"`
			}
			if err := r.sessionCall(ctx, s, "check", nil, &check); err != nil || slices.Contains(check.CancelRequested, id) {
				cancel()
				return
			}
		}
	}
}

func (r *Runtime) execute(ctx context.Context, action Action, s *liveSession) (map[string]any, string) {
	if action.Kind != "execute_command" {
		result, state := r.executeFile(ctx, action, s)
		if ctx.Err() != nil {
			s.mu.Lock()
			s.info.Status = "context_lost"
			s.closing = true
			s.mu.Unlock()
			result["context_lost"] = true
		}
		return result, state
	}
	var command assetexec.Request
	if err := decodeRequest(action.Request, &command); err != nil {
		return map[string]any{"error": err.Error()}, "failed"
	}
	var outputMu sync.Mutex
	var written int64
	emit := func(stream string, data []byte) error {
		outputMu.Lock()
		defer outputMu.Unlock()
		if written+int64(len(data)) > r.opts.MaxOutputBytes {
			return errors.New("execution evidence output limit exceeded")
		}
		for len(data) > 0 {
			n := min(len(data), 16<<10)
			if err := r.journal.Append(action.ID, "running", map[string]any{"stream": stream, "data": base64.StdEncoding.EncodeToString(data[:n]), "encoding": "base64"}); err != nil {
				return err
			}
			written += int64(n)
			data = data[n:]
		}
		return nil
	}
	s.mu.Lock()
	connection := s.connection
	s.mu.Unlock()
	result, err := connection.Execute(ctx, command, emit)
	if result.ContextLost {
		s.mu.Lock()
		s.info.Status = "context_lost"
		s.closing = true
		s.mu.Unlock()
	}
	payload := map[string]any{"exit_code": result.ExitCode, "rows_affected": result.RowsAffected, "output_truncated": result.Truncated || errors.Is(err, ErrEvidenceLimit), "stop_confirmed": result.StopConfirmed, "context_lost": result.ContextLost}
	if err != nil {
		payload["error"] = err.Error()
		if result.Dispatched && !result.Completed {
			return payload, "unknown"
		}
		return payload, "failed"
	}
	if result.ExitCode != nil && *result.ExitCode != 0 {
		return payload, "failed"
	}
	return payload, "succeeded"
}

func (r *Runtime) Get(ctx context.Context, p Principal, id string, offset, limit int64) (Action, error) {
	path, err := operationPath("actions", id, "get")
	if err != nil {
		return Action{}, err
	}
	if offset < 0 || offset > 1000000 || limit < 1 || limit > 32 {
		return Action{}, errors.New("invalid event page bounds")
	}
	var action Action
	for attempt := 0; attempt < 2; attempt++ {
		// A fresh decode prevents a null/missing page field from retaining data
		// from the previous response. Every lookup rechecks Core ownership.
		action = Action{}
		if err = r.core.Call(ctx, path, map[string]any{"context_token": p.ContextToken, "offset": offset, "limit": limit}, &action); err != nil {
			return Action{}, err
		}
		if action.ID != id {
			return Action{}, errors.New("authorization service returned a different action")
		}
		action.ActionID = id
		action.NextOffset = nil
		action.EvidencePending = false
		if terminalState(action.Status) {
			_, _ = r.journal.SyncCore(action)
		}
		connectionPending := false
		local, readErr := r.journal.Action(id)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return Action{}, readErr
		}
		if readErr == nil {
			if action.Kind == "file_upload" && action.Status == "queued" && local.Artifact == "" && !local.Dispatched && !terminalState(local.Action.Status) {
				action.NextStep = "upload_bytes"
			}
			if !terminalState(action.Status) && (terminalState(local.Action.Status) || local.Action.Status == "awaiting_input") {
				// Core must observe the connection's success before it accepts
				// commands. Reporting local success here would race that gate.
				connectionPending = action.Kind == "connect_asset" || local.Action.Kind == "connect_asset"
				if !connectionPending {
					action.Status = local.Action.Status
				}
				action.Result = local.Action.Result
			}
			action.AuditStatus = local.Action.AuditStatus
			action.LastSequence = max(action.LastSequence, local.Sequence)
			pending, err := r.journal.ActionEvents(id, uint64(offset), int(limit))
			if err != nil {
				return Action{}, err
			}
			action.Events = append(action.Events, pending...)
		}
		slices.SortStableFunc(action.Events, func(a, b Event) int {
			if a.Sequence < b.Sequence {
				return -1
			}
			if a.Sequence > b.Sequence {
				return 1
			}
			return 0
		})
		if len(action.Events) > 0 {
			action.LastSequence = max(action.LastSequence, action.Events[len(action.Events)-1].Sequence)
		}
		continuous := make([]Event, 0, min(int(limit), len(action.Events)))
		last := uint64(offset)
		for _, event := range action.Events {
			if event.Sequence <= last {
				continue
			}
			if event.Sequence != last+1 || len(continuous) == int(limit) {
				break
			}
			event.SessionToken = ""
			continuous = append(continuous, event)
			last = event.Sequence
		}
		action.Events = continuous
		gap := len(continuous) < int(limit) && last < action.LastSequence
		if (gap || connectionPending) && attempt == 0 {
			// Flush may have acknowledged an event after Core's snapshot but
			// before the outbox read. One fresh lookup can recover that gap.
			continue
		}
		action.EvidencePending = gap || connectionPending
		if last < action.LastSequence || action.EvidencePending {
			next := int64(last)
			action.NextOffset = &next
		}
		break
	}
	// Decode the bounded stream evidence for agents while retaining an explicit
	// untrusted-output boundary in the MCP server instructions.
	for i := range action.Events {
		payload := action.Events[i].Payload
		if payload["encoding"] == "base64" {
			if encoded, ok := payload["data"].(string); ok {
				if data, err := base64.StdEncoding.DecodeString(encoded); err == nil {
					payload["data"] = strings.ToValidUTF8(string(data), "�")
					payload["encoding"] = "utf-8"
				}
			}
		}
		action.Events[i].SessionToken = ""
	}
	return action, nil
}

func (r *Runtime) Cancel(ctx context.Context, p Principal, id string) (Action, error) {
	path, err := operationPath("actions", id, "cancel")
	if err != nil {
		return Action{}, err
	}
	var action Action
	if err = r.core.Call(ctx, path, map[string]any{"context_token": p.ContextToken}, &action); err != nil {
		return action, err
	}
	action.ActionID = action.ID
	if terminalState(action.Status) {
		_, _ = r.journal.SyncCore(action)
	}
	r.mu.Lock()
	cancel := r.jobs[id]
	if cancel == nil {
		for _, s := range r.sessions {
			s.mu.Lock()
			if s.info.ActionID == id {
				cancel = s.connectCancel
			}
			s.mu.Unlock()
			if cancel != nil {
				break
			}
		}
	}
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return action, nil
}

func (r *Runtime) Disconnect(ctx context.Context, p Principal, id string) (map[string]any, error) {
	// The Core action lookup checks current user/org/client ownership even
	// when the process-local connection is already gone.
	r.mu.Lock()
	s := r.sessions[id]
	r.mu.Unlock()
	if s == nil {
		return nil, errors.New("asset session is unavailable or already closed")
	}
	s.mu.Lock()
	if s.owner != owner(p) {
		s.mu.Unlock()
		return nil, errors.New("session does not belong to this authorization")
	}
	info := s.info
	s.mu.Unlock()
	if _, err := r.Get(ctx, p, info.ActionID, 0, 1); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.closing = true
	active := s.active
	s.mu.Unlock()
	status := "draining"
	if active == 0 {
		r.closeSession(s)
		status = "closed"
	}
	return map[string]any{"session_id": id, "status": status, "active_operations": active, "audit_status": "pending"}, nil
}

func (r *Runtime) closeSession(s *liveSession) {
	s.mu.Lock()
	if s.active > 0 || s.closingNow {
		s.mu.Unlock()
		return
	}
	s.closing = true
	s.closingNow = true
	connection := s.connection
	s.connection = nil
	info := s.info
	s.mu.Unlock()
	if connection != nil {
		_ = connection.Close()
	}
	if info.TerminalSessionID != "" {
		session.RemoveSessionById(info.TerminalSessionID)
	}
	// Persisted session capabilities let maintenance retry Core closure after
	// an outage or process restart; releasing a transport never waits on it.
	r.removeSession(info.ID)
}

func (r *Runtime) flush(parent context.Context) {
	r.flushMu.Lock()
	defer r.flushMu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	events, err := r.journal.Pending(128)
	if err != nil {
		logger.Errorf("Read Agent audit outbox: %v", err)
		return
	}
	failed := map[string]bool{}
	for _, event := range events {
		if ctx.Err() != nil {
			return
		}
		if failed[event.ActionID] {
			continue
		}
		path, err := operationPath("actions", event.ActionID, "events")
		if err != nil {
			return
		}
		err = r.core.Call(ctx, path, event, &map[string]any{})
		if err != nil {
			failed[event.ActionID] = true
			if time.Since(r.lastAuditFailure) >= time.Minute {
				logger.Errorf("Agent audit delivery remains pending for action %s: %v", event.ActionID, err)
				r.lastAuditFailure = time.Now()
			}
			continue
		}
		if err = r.journal.Ack(event); err != nil {
			return
		}
	}
}

func (r *Runtime) reconcile(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	sessions, err := r.journal.Sessions()
	if err != nil {
		return
	}
	for _, info := range sessions {
		if ctx.Err() != nil {
			return
		}
		r.mu.Lock()
		active := r.sessions[info.ID] != nil
		r.mu.Unlock()
		if active {
			continue
		}
		if err = r.sessionCall(ctx, info, "close", nil, &map[string]any{}); err == nil {
			_ = r.journal.DeleteSession(info.ID)
		}
	}
	waiting, err := r.journal.Unfinished(128)
	if err != nil {
		return
	}
	for _, rec := range waiting {
		if ctx.Err() != nil {
			return
		}
		// Authorization returns existing final states without re-executing or
		// requesting credentials. Active workers own their own state updates.
		r.mu.Lock()
		_, working := r.jobs[rec.Action.ID]
		s := r.sessions[rec.Action.SessionID]
		r.mu.Unlock()
		if working || (s != nil && rec.Action.Kind == "connect_asset") {
			continue
		}
		if action, err := r.authorize(ctx, rec.Action, Session{ID: rec.Action.SessionID, SessionToken: rec.SessionToken}); err == nil {
			_, _ = r.journal.SyncCore(action)
		}
	}
}

func (r *Runtime) maintain() {
	defer r.wg.Done()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	lastCleanup := time.Time{}
	lastAuthorityCheck := time.Time{}
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-tick.C:
			r.flush(r.ctx)
			if time.Since(lastAuthorityCheck) >= 5*time.Second {
				r.checkSessionAuthorities(r.ctx)
				lastAuthorityCheck = time.Now()
			}
			r.mu.Lock()
			sessions := make([]*liveSession, 0, len(r.sessions))
			for _, s := range r.sessions {
				sessions = append(sessions, s)
			}
			r.mu.Unlock()
			for _, s := range sessions {
				s.mu.Lock()
				expired := !s.info.ExpiresAt.IsZero() && !s.info.ExpiresAt.After(time.Now())
				closeIt := s.active == 0 && (s.closing || expired || time.Since(s.lastUsed) > r.opts.IdleTimeout)
				s.mu.Unlock()
				if closeIt {
					r.closeSession(s)
				}
			}
			if time.Since(lastCleanup) > 30*time.Second {
				r.reconcile(r.ctx)
				r.cleanupFiles()
				lastCleanup = time.Now()
			}
		}
	}
}

func (r *Runtime) Close() error {
	r.mu.Lock()
	r.closing = true
	r.mu.Unlock()
	r.cancel()
	r.wg.Wait()
	r.mu.Lock()
	sessions := make([]*liveSession, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.mu.Unlock()
	for _, s := range sessions {
		r.closeSession(s)
	}
	r.flush(context.Background())
	r.reconcile(context.Background())
	return r.journal.Close()
}

func (r *Runtime) cleanupFiles() {
	paths, err := r.journal.Prune(time.Now())
	if err != nil {
		logger.Errorf("Prune Agent audit journal: %v", err)
		return
	}
	for _, path := range paths {
		if filepath.Dir(path) == filepath.Join(r.opts.Directory, "files") {
			_ = os.Remove(path)
		}
	}
	// Interrupted staging files have no live owner after a restart. Retain
	// completed artifacts for 24 hours; never unlink an active operation.
	entries, err := os.ReadDir(filepath.Join(r.opts.Directory, "files"))
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := filepath.Ext(name)
		if ext != ".part" && ext != ".data" {
			continue
		}
		id := strings.TrimSuffix(name, ext)
		info, err := entry.Info()
		if err != nil || time.Since(info.ModTime()) < 24*time.Hour {
			continue
		}
		if _, err := r.journal.Action(id); !errors.Is(err, os.ErrNotExist) {
			continue
		}
		_ = os.Remove(filepath.Join(r.opts.Directory, "files", name))
	}
}

func (r *Runtime) String() string {
	return "agent execution service"
}
