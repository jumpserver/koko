package assetexec

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/jumpserver/koko/internal/sessiontools"
	"go.mongodb.org/mongo-driver/mongo"
)

type commandExecutor struct{ err error }

func (e commandExecutor) Execute(context.Context, string, func(string)) (string, *int, error) {
	return "result", nil, e.err
}
func (e commandExecutor) Close() error { return nil }

func TestMongoDBDispatchCompletionAndContextLoss(t *testing.T) {
	for _, tc := range []struct {
		err             error
		completed, lost bool
	}{
		{nil, true, false},
		{mongo.CommandError{Code: 13, Message: "denied"}, true, false},
		{io.ErrUnexpectedEOF, false, true},
	} {
		conn := &Connection{protocol: "mongodb", opts: Options{MaxOutputBytes: 1024},
			executor: commandExecutor{err: tc.err}, dbState: &databaseState{transport: &sessiontools.AgentSQLTransport{}}}
		result, err := conn.Execute(context.Background(), Request{Command: `db.runCommand({"ping":1})`}, func(string, []byte) error { return nil })
		_ = conn.Close()
		if (err == nil) != (tc.err == nil) || !result.Dispatched || result.Completed != tc.completed || result.ContextLost != tc.lost {
			t.Fatalf("incorrect dispatch evidence: %+v %v", result, err)
		}
	}
}

func TestDatabaseCommandsRejectInteractiveStateBeforeDispatch(t *testing.T) {
	for protocol, command := range map[string]string{"redis": "MULTI", "mongodb": `db.runCommand({"getMore":1})`} {
		conn := &Connection{protocol: protocol}
		result, err := conn.Execute(context.Background(), Request{Command: command}, func(string, []byte) error { return nil })
		if !errors.Is(err, ErrUnsupported) || result.Dispatched {
			t.Fatalf("interactive state reached dispatch: %+v %v", result, err)
		}
	}
}
