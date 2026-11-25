package session

import (
	"sync/atomic"
	"time"
)

// PermissionGrace tracks Core's ten-minute grace period after permission is
// revoked. Repeated notifications must not postpone the original deadline.
type PermissionGrace struct {
	deadline atomic.Pointer[time.Time]
}

func (g *PermissionGrace) Revoke(now time.Time) bool {
	deadline := now.Add(10 * time.Minute)
	return g.deadline.CompareAndSwap(nil, &deadline)
}

func (g *PermissionGrace) Restore() bool { return g.deadline.Swap(nil) != nil }

func (g *PermissionGrace) Revoked() bool { return g.deadline.Load() != nil }

func (g *PermissionGrace) Expired(now time.Time) bool {
	deadline := g.deadline.Load()
	return deadline != nil && !now.Before(*deadline)
}
