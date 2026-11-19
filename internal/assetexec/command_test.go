package assetexec

import (
	"context"
	"errors"
	"io"
	"testing"

	dm "gitee.com/chunanyong/dm"
	db2 "github.com/go-db2/go-db2"
	"github.com/jumpserver/koko/internal/sessiontools"
	"go.mongodb.org/mongo-driver/mongo"
)

func TestDB2DamengStatementRejectionEvidence(t *testing.T) {
	for _, tc := range []struct {
		protocol string
		err      error
		rejected bool
	}{
		{"db2", &db2.Db2Error{SQLCode: -204, SQLState: "42704"}, true},
		{"db2", errors.New("db2: SQLCODE=-204 SQLSTATE=42704 missing table"), true},
		{"db2", errors.New("db2: SQLCODE=-30081 SQLSTATE=08001 communication failure"), false},
		{"db2", errors.New("db2: SQLCODE=-913 SQLSTATE=40003 completion unknown"), false},
		{"db2", io.ErrUnexpectedEOF, false},
		{"dameng", &dm.DmError{ErrCode: -2106}, true},
		{"dameng", &dm.DmError{ErrCode: 6001}, false},
		{"dameng", context.Canceled, false},
	} {
		if rejected := db2DamengStatementRejected(tc.protocol, tc.err); rejected != tc.rejected {
			t.Fatalf("%s misclassified server rejection: %v", tc.protocol, tc.err)
		}
	}
}

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
