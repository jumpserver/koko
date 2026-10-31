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
		{mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000, Message: "duplicate key"}}}, true, false},
		{mongo.WriteException{WriteConcernError: &mongo.WriteConcernError{Code: 64, Message: "write concern timed out"}}, false, true},
		{mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000}}, WriteConcernError: &mongo.WriteConcernError{Code: 64}}, false, true},
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
	for _, tc := range []struct{ protocol, command string }{
		{"redis", "MULTI"},
		{"redis", "CLIENT REPLY OFF"},
		{"redis", "CLIENT TRACKING ON"},
		{"redis", "READONLY"},
		{"redis", "BLMPOP 0 1 queue LEFT"},
		{"redis", "WAIT 1 0"},
		{"mongodb", `db.runCommand({"getMore":1})`},
		{"mongodb", `db.runCommand({"find":"items","lsid":{"id":"session"}})`},
		{"mongodb", `db.runCommand({"insert":"items","txnNumber":1})`},
		{"mongodb", `db.runCommand({"find":"items","startTransaction":true})`},
		{"mongodb", `db.runCommand({"find":"items","autocommit":false})`},
	} {
		conn := &Connection{protocol: tc.protocol}
		result, err := conn.Execute(context.Background(), Request{Command: tc.command}, func(string, []byte) error { return nil })
		if !errors.Is(err, ErrUnsupported) || result.Dispatched {
			t.Fatalf("%s reached dispatch: %+v %v", tc.command, result, err)
		}
	}
}
