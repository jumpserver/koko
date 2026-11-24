package core

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jumpserver-dev/sdk-go/model"
)

// Run starts the outbox and lifecycle workers for this
// provider. Browser sockets have no part in either lifetime.
func (p *CoreProvider) Run(ctx context.Context) {
	p.runOnce.Do(func() {
		defer p.spool.close()
		var workers sync.WaitGroup
		workers.Add(1)
		go func() { defer workers.Done(); p.runOutbox(ctx) }()
		p.runMaintenance(ctx)
		workers.Wait()
		shutdown, cancel := context.WithTimeout(context.Background(), p.options.RequestTimeout)
		defer cancel()
		p.flush(shutdown)
	})
}

func (p *CoreProvider) runOutbox(ctx context.Context) {
	retry := time.NewTicker(p.options.RetryInterval)
	defer retry.Stop()
	p.flush(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
			p.flush(ctx)
		case <-retry.C:
			p.flush(ctx)
		}
	}
}

func (p *CoreProvider) runMaintenance(ctx context.Context) {
	check := time.NewTicker(p.options.CheckInterval)
	defer check.Stop()
	for {
		select {
		case <-ctx.Done():
			p.mu.Lock()
			p.stopping = true
			p.mu.Unlock()
			p.requests.Wait()
			for _, grant := range p.listGrants() {
				p.terminate(grant, "server_shutdown", "")
				// Run is stopped after its resource owner has drained. This is a
				// final fallback for callers that violated the owner-close contract.
				if err := p.Close(context.Background(), grant); err != nil {
					p.report("session_cleanup_persistence_failed")
				}
			}
			return
		case <-check.C:
			for _, grant := range p.listGrants() {
				if err := grant.Check(); err != nil && !errors.Is(err, ErrLocked) && !errors.Is(err, ErrControlUnavailable) {
					p.terminate(grant, err.Error(), "")
				}
			}
		}
	}
}

func (p *CoreProvider) listGrants() []*Grant {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]*Grant, 0, len(p.grants))
	for _, grant := range p.grants {
		result = append(result, grant)
	}
	return result
}

// A lost administrator control channel pauses access. Its recovery must not
// clear an independent administrator lock.
func (p *CoreProvider) SetControlAvailable(available bool) {
	p.mu.Lock()
	changed := p.controlAvailable != available
	p.controlAvailable = available
	defer p.mu.Unlock()
	if !changed {
		return
	}
	for _, grant := range p.grants {
		grant.mu.Lock()
		if grant.closed {
			grant.mu.Unlock()
			continue
		}
		grant.controlUnavailable = !available
		if !available {
			notifyLocked(grant, Control{Type: "lock", Reason: ErrControlUnavailable.Error()})
		} else if grant.locked {
			notifyLocked(grant, Control{Type: "lock", Reason: ErrLocked.Error()})
		} else {
			notifyLocked(grant, Control{Type: "unlock", Reason: "session_control_restored"})
		}
		grant.mu.Unlock()
	}
}

func waitContext(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (p *CoreProvider) HandleTask(grant *Grant, task *model.TerminalTask) error {
	if grant == nil || task == nil {
		return errors.New("invalid_session_task")
	}
	if task.Name == model.TaskKillSession {
		p.terminate(grant, "admin_terminate", task.Kwargs.TerminatedBy)
		return nil
	}
	grant.mu.Lock()
	defer grant.mu.Unlock()
	if grant.closed {
		return nil
	}
	var control Control
	switch task.Name {
	case model.TaskLockSession:
		grant.locked = true
		control = Control{Type: "lock", Reason: "session_locked", Actor: task.Kwargs.CreatedByUser}
	case model.TaskUnlockSession:
		grant.locked = false
		if grant.controlUnavailable {
			control = Control{Type: "lock", Reason: ErrControlUnavailable.Error(), Actor: task.Kwargs.CreatedByUser}
		} else {
			control = Control{Type: "unlock", Actor: task.Kwargs.CreatedByUser}
		}
	case model.TaskPermExpired:
		grant.permission.Revoke(time.Now())
		control = Control{Type: "permission_revoked", Reason: "permission_expires_in_10_minutes"}
	case model.TaskPermValid:
		grant.permission.Restore()
		control = Control{Type: "permission_restored"}
	default:
		// An unknown admin policy must never silently allow continued access.
		grant.locked = true
		control = Control{Type: "lock", Reason: "unsupported_session_control"}
	}
	notifyLocked(grant, control)
	return nil
}

func notifyLocked(grant *Grant, control Control) {
	for _, watcher := range grant.watchers {
		select {
		case watcher <- control:
		default:
			// Keep the most recent authoritative state, especially termination.
			select {
			case <-watcher:
			default:
			}
			watcher <- control
		}
	}
}

func (p *CoreProvider) terminate(grant *Grant, reason, actor string) {
	grant.mu.Lock()
	if !grant.closed {
		grant.closed = true
		grant.closeReason = reason
		// Connected hands ownership to the engine before it publishes a browser
		// session. This also closes the tiny Connected -> Watch registration gap.
		grant.terminationOwned = grant.opening || grant.connected || len(grant.watchers) > 0
		notifyLocked(grant, Control{Type: "terminate", Reason: reason, Actor: actor})
	}
	owned := grant.terminationOwned
	grant.mu.Unlock()
	if owned {
		return
	}
	if err := p.Close(context.Background(), grant); err != nil {
		p.report("session_cleanup_persistence_failed")
	}
}

// Watch blocks until its caller cancels. It replays current control state so a
// kill/lock arriving between Open and callback registration cannot be missed.
func (p *CoreProvider) Watch(ctx context.Context, grant *Grant, callback func(Control)) {
	if grant == nil || callback == nil {
		return
	}
	grant.mu.Lock()
	grant.nextWatcher++
	id := grant.nextWatcher
	channel := make(chan Control, 16)
	grant.watchers[id] = channel
	switch {
	case grant.closed:
		channel <- Control{Type: "terminate", Reason: grant.closeReason}
	case grant.locked:
		channel <- Control{Type: "lock", Reason: "session_locked"}
	case grant.controlUnavailable:
		channel <- Control{Type: "lock", Reason: ErrControlUnavailable.Error()}
	case grant.permission.Revoked():
		channel <- Control{Type: "permission_revoked", Reason: "permission_expires_in_10_minutes"}
	}
	grant.mu.Unlock()
	defer func() {
		grant.mu.Lock()
		delete(grant.watchers, id)
		grant.mu.Unlock()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case control := <-channel:
			callback(control)
			if control.Type == "terminate" {
				return
			}
		}
	}
}
