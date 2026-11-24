package core

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
	"github.com/jumpserver-dev/sdk-go/service"
	"github.com/jumpserver/koko/pkg/lion/gateway"
	"github.com/jumpserver/koko/pkg/session"
)

type Options struct {
	SpoolDir           string
	ReplayDir          string
	RequestTimeout     time.Duration
	RetryInterval      time.Duration
	CheckInterval      time.Duration
	ReviewTimeout      time.Duration
	ReviewPollInterval time.Duration
	// OnError receives operation codes only, never Core responses or credentials.
	OnError func(error)
}

type CoreProvider struct {
	client           *service.JMService
	options          Options
	mu               sync.RWMutex
	grants           map[string]*Grant
	spool            *outbox
	replays          *replayStore
	wake             chan struct{}
	runOnce          sync.Once
	flushMu          sync.Mutex
	controlAvailable bool
	calls            chan struct{}
	requests         sync.WaitGroup
	stopping         bool
}

func NewCoreProvider(client *service.JMService, options Options) (*CoreProvider, error) {
	if client == nil {
		return nil, errors.New("core_client_required")
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = 10 * time.Second
	}
	if options.RetryInterval <= 0 {
		options.RetryInterval = 5 * time.Second
	}
	if options.CheckInterval <= 0 {
		options.CheckInterval = time.Second
	}
	if options.ReviewTimeout <= 0 || options.ReviewTimeout > 30*time.Minute {
		options.ReviewTimeout = 30 * time.Minute
	}
	if options.ReviewPollInterval <= 0 {
		options.ReviewPollInterval = 5 * time.Second
	}
	spool, err := newOutbox(options.SpoolDir)
	if err != nil {
		return nil, err
	}
	if options.ReplayDir == "" {
		options.ReplayDir = filepath.Join(options.SpoolDir, "replays")
	}
	replays, err := newReplayStore(options.ReplayDir)
	if err != nil {
		_ = spool.close()
		return nil, errors.Join(ErrAuditDisk, err)
	}
	p := &CoreProvider{client: client, options: options, grants: make(map[string]*Grant), spool: spool, replays: replays, wake: make(chan struct{}, 1), calls: make(chan struct{}, 16)}
	if err := p.recoverReplays(); err != nil {
		_ = spool.close()
		return nil, errors.Join(ErrAuditDisk, err)
	}
	return p, nil
}

// Pair admissions with the shutdown barrier before releasing the state directory.
func (p *CoreProvider) startRequest() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.stopping {
		return false
	}
	p.requests.Add(1)
	return true
}

// SDK requests have a transport timeout but take no caller context. Bound the
// number still running after a caller cancels, and recover a late ticket if needed.
func callCore[T any](ctx context.Context, p *CoreProvider, call func() (T, error), abandoned func(T)) (T, error) {
	var zero T
	ctx, cancel := context.WithTimeout(ctx, p.options.RequestTimeout)
	defer cancel()
	select {
	case p.calls <- struct{}{}:
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	if !p.startRequest() {
		<-p.calls
		return zero, ErrClosed
	}
	type response struct {
		value T
		err   error
	}
	done := make(chan response)
	go func() {
		defer p.requests.Done()
		defer func() { <-p.calls }()
		if ctx.Err() != nil {
			return
		}
		value, err := call()
		select {
		case done <- response{value, err}:
		case <-ctx.Done():
			if abandoned != nil {
				abandoned(value)
			}
		}
	}()
	select {
	case result := <-done:
		return result.value, result.err
	case <-ctx.Done():
		return zero, ctx.Err()
	}
}

func (p *CoreProvider) Open(ctx context.Context, token, remoteAddr string) (result *Grant, resultErr error) {
	if !p.startRequest() {
		return nil, ErrClosed
	}
	defer p.requests.Done()
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("connection_token_required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !p.controlReady() {
		return nil, ErrControlNotReady
	}
	conf, err := callCore(ctx, p, p.client.GetTerminalConfig, nil)
	if err != nil {
		// No credential has been consumed, so the same connection attempt may retry.
		return nil, ErrControlNotReady
	}
	if conf.MaxIdleTime < 0 || conf.MaxSessionTime < 0 {
		return nil, errors.New("invalid_session_time_limit")
	}
	// Gate the one-use credential exchange on the shared terminal control channel.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !p.controlReady() {
		return nil, ErrControlNotReady
	}
	data, err := callCore(ctx, p, func() (model.ConnectToken, error) { return p.client.GetConnectTokenInfo(token, true) }, nil)
	if data.Code != "" || data.Error != "" {
		return nil, errors.New("connection_token_rejected")
	}
	if err != nil {
		return nil, errors.New("token_exchange_failed")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if !data.Actions.EnableConnect() {
		return nil, errors.New("connect_permission_denied")
	}
	if int64(data.ExpireAt) <= time.Now().Unix() {
		return nil, ErrExpired
	}
	if data.Id == "" || data.Asset.ID == "" || data.User.ID == "" || data.Account.ID == "" || data.Asset.Address == "" || data.Account.Username == "" {
		return nil, errors.New("incomplete_connection_identity")
	}
	if t := data.Account.SecretType.Value; t != "" && t != "password" {
		return nil, errors.New("unsupported_database_credential_type")
	}
	config, err := managedConnectionConfig(data)
	if err != nil {
		return nil, err
	}
	matchers, err := compileMatchers(data.CommandFilterACLs)
	if err != nil {
		return nil, err
	}
	maskers, err := compileMaskers(data.DataMaskingRules)
	if err != nil {
		return nil, err
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remoteAddr = host
	}
	sess, err := session.NewConnectionInfo(&data, remoteAddr, model.LoginFromWeb, "db_workbench")
	if err != nil {
		return nil, err
	}
	id := sess.ID
	sess.Asset = data.Asset.Name
	sess.OrgID = data.Asset.OrgID
	sess.ErrReason = ""
	permissions := Permissions{Connect: true, Upload: data.Actions.EnableUpload(), Download: data.Actions.EnableDownload(), Copy: data.Actions.EnableCopy(), Paste: data.Actions.EnablePaste()}
	if policy := data.ClipboardPolicy; policy != nil {
		if policy.Copy != nil {
			permissions.Copy = permissions.Copy && policy.Copy.Enabled
		}
		if policy.Paste != nil {
			permissions.Paste = permissions.Paste && policy.Paste.Enabled
		}
	}
	now := time.Now()
	g := &Grant{
		ID: id, OwnerID: data.User.ID, OwnerName: sess.User,
		AssetID: data.Asset.ID, AssetName: data.Asset.Name, AccountName: sess.Account,
		Config: config, Permissions: permissions, ExpiresAt: time.Unix(int64(data.ExpireAt), 0),
		MaxIdle: time.Duration(conf.MaxIdleTime) * time.Minute, MaxDuration: time.Duration(conf.MaxSessionTime) * time.Hour,
		session: &sess, matchers: matchers, maskers: maskers, created: now, lastActive: now,
		watchers: make(map[uint64]chan Control),
	}
	// Once creation starts, observe the SDK response even if the browser stops
	// waiting. A session may already exist and its ID is needed to finish it.
	select {
	case p.calls <- struct{}{}:
		defer func() { <-p.calls }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	compensate := true
	defer func() {
		g.mu.Lock()
		if resultErr == nil {
			resultErr = g.checkLocked(time.Now())
			if resultErr != nil {
				result = nil
			}
		}
		g.opening = false
		g.mu.Unlock()
		if resultErr != nil && compensate {
			resultErr = errors.Join(resultErr, p.Close(context.Background(), g))
		}
	}()
	created, err := p.client.CreateSession(sess)
	if err != nil || created.ID == "" {
		return nil, errors.New("core_session_create_failed")
	}
	// Publish the session to the existing heartbeat and token permission check
	// before face monitoring or opening a potentially slow SSH forward. Open
	// owns cleanup until initialization completes.
	p.mu.Lock()
	if _, exists := p.grants[created.ID]; exists {
		_, oursAlreadyUsed := p.grants[id]
		compensate = !oursAlreadyUsed
		p.mu.Unlock()
		return nil, errors.New("duplicate_core_session_id")
	}
	g.ID, sess.ID = created.ID, created.ID
	g.opening = true
	g.controlUnavailable = !p.controlAvailable || p.stopping
	p.grants[g.ID] = g
	session.AddSession(session.NewSession(&sess, func(task *model.TerminalTask) error { return p.HandleTask(g, task) }))
	p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := session.BindConnection(p.client, g.ID, &data, func(error) {
		p.report("session_ticket_relation_failed")
	}); err != nil {
		return nil, errors.New("face_monitor_join_failed")
	}
	if err = p.replays.start(g); err != nil {
		return nil, errors.Join(ErrAuditDisk, err)
	}
	if data.Gateway != nil {
		forward := &gateway.DomainGateway{DstAddr: net.JoinHostPort(config.Host, strconv.Itoa(config.Port)), BindHost: "127.0.0.1", SelectedGateway: data.Gateway}
		// Open has a deadline, while an established forward lives as long as its grant.
		stop := context.AfterFunc(ctx, forward.Stop)
		err = forward.Start()
		if stopped := stop(); err != nil || !stopped {
			forward.Stop()
			return nil, errors.New("gateway_forward_failed")
		}
		g.mu.Lock()
		if g.closed {
			g.mu.Unlock()
			forward.Stop()
			return nil, ErrClosed
		}
		g.gateway = forward
		g.Config.Host, g.Config.Port = "127.0.0.1", forward.GetListenAddr().Port
		g.mu.Unlock()
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return g, nil
}

func (p *CoreProvider) Connected(ctx context.Context, g *Grant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := g.Check(); err != nil {
		return err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := g.checkLocked(time.Now()); err != nil {
		return err
	}
	if g.connected {
		return nil
	}
	if err := p.enqueueLifecycle(g.ID, model.AssetConnectSuccess, ""); err != nil {
		return err
	}
	g.connected = true
	return nil
}

func (p *CoreProvider) Close(_ context.Context, g *Grant) error {
	if g == nil {
		return nil
	}
	g.closeMu.Lock()
	defer g.closeMu.Unlock()
	g.mu.Lock()
	g.closed = true
	if g.closeReason == "" {
		g.closeReason = "connect_disconnect"
	}
	forward := g.gateway
	g.gateway = nil
	cleanup := closeEnvelope(g, g.closeReason, g.connected)
	g.mu.Unlock()
	if forward != nil {
		forward.Stop()
	}
	if g.closePersisted {
		return nil
	}
	replay, err := p.replays.finish(g.ID)
	if err != nil {
		return errors.Join(ErrAuditDisk, err)
	}
	cleanup.Replay = replay
	// All cleanup requests are persisted together before attempting any remote
	// mutation, allowing restart recovery and avoiding a half-written close.
	if err := p.spool.put(cleanup); err != nil {
		return errors.Join(ErrAuditDisk, err)
	}
	g.closePersisted = true
	p.mu.Lock()
	if p.grants[g.ID] == g {
		delete(p.grants, g.ID)
		session.RemoveSessionById(g.ID)
	}
	p.mu.Unlock()
	p.signal()
	return nil
}

func (p *CoreProvider) Record(_ context.Context, g *Grant, record AuditRecord) error {
	if g == nil || g.session == nil {
		return errors.New("audit_session_required")
	}
	if record.StartedAt.IsZero() {
		record.StartedAt = time.Now()
	}
	risk := int64(record.RiskLevel)
	if !record.RiskClassified && record.ACLID == "" {
		if matched := matchPolicy(g, record.SQL); matched != nil {
			record.ACLID, record.GroupID = matched.aclID, matched.groupID
			switch matched.action {
			case model.ActionWarning, model.ActionNotifyAndWarn:
				risk = model.WarningLevel
			case model.ActionReject:
				risk = model.RejectLevel
			case model.ActionReview:
				risk = model.ReviewReject
			}
		}
	}
	output := record.Output
	if record.Error != "" {
		output = record.Error
	}
	replay := output
	if record.Error == "" && record.ReplayOutput != "" {
		replay = record.ReplayOutput
	}
	for _, secret := range []string{g.Config.Password, g.Config.TLS.ClientKey} {
		if secret != "" {
			output = strings.ReplaceAll(output, secret, "[redacted]")
			replay = strings.ReplaceAll(replay, secret, "[redacted]")
		}
	}
	output = truncate(output, 2048)
	request := &model.Command{
		SessionID: g.ID, OrgID: g.session.OrgID, User: g.session.User,
		Server: g.session.Asset, Account: g.session.Account, DateCreated: record.StartedAt.UTC(),
		Input: record.SQL, Output: output, Timestamp: record.StartedAt.Unix(),
		RiskLevel: risk, CmdFilterAclId: record.ACLID, CmdGroupId: record.GroupID,
	}
	if err := p.replays.record(g, record.SQL, replay, record.StartedAt); err != nil {
		return errors.Join(ErrAuditDisk, err)
	}
	if err := p.spool.put(envelope{Command: request}); err != nil {
		return errors.Join(ErrAuditDisk, err)
	}
	p.signal()
	return nil
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	for max > 0 && value[max]&0xc0 == 0x80 {
		max--
	}
	return value[:max]
}

func (p *CoreProvider) enqueueLifecycle(id string, event model.LifecycleEvent, reason string) error {
	if err := p.spool.put(envelope{Lifecycle: &lifecycleRequest{SessionID: id, Event: event, Reason: reason}}); err != nil {
		return errors.Join(ErrAuditDisk, err)
	}
	p.signal()
	return nil
}

func (p *CoreProvider) signal() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *CoreProvider) report(code string) {
	if p.options.OnError != nil {
		p.options.OnError(errors.New(code))
	}
}

var _ Provider = (*CoreProvider)(nil)
