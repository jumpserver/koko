package core

import (
	"context"
	"errors"
)

func (p *CoreProvider) controlReady() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.controlAvailable && !p.stopping
}

func (p *CoreProvider) Ready(ctx context.Context) error {
	if !p.controlReady() {
		return ErrControlNotReady
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := callCore(ctx, p, p.client.GetPublicSetting, nil); err != nil {
		return errors.New("core_not_ready")
	}
	if !p.controlReady() {
		return ErrControlNotReady
	}
	return ctx.Err()
}
