package sessiontools

import (
	"context"

	"github.com/jumpserver/koko/pkg/srvconn"
)

type WinRMExecutor struct{ client *srvconn.WinRMClient }

func NewWinRMExecutor(client *srvconn.WinRMClient) *WinRMExecutor {
	return &WinRMExecutor{client: client}
}

func (e *WinRMExecutor) Execute(ctx context.Context, command string, onOutput func(string)) (string, *int, error) {
	output := &boundedOutput{onUpdate: onOutput}
	err := e.client.Execute(ctx, command, output)
	// PSRP reports pipeline state, not an OS process exit code.
	return output.String(), nil, err
}

func (e *WinRMExecutor) Close() error { return nil } // The terminal owns the runspace.
