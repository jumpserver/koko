package nosql

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func testMongoRuntime() *mongoSession {
	s := &mongoSession{database: "unit", batchSize: 20, cursors: make(map[*mongoCursor]struct{}), sessions: make(map[*mongo.Session]*mongoSessionState)}
	s.initRuntime()
	return s
}

func TestMongoJavaScriptAndBSON(t *testing.T) {
	s := testMongoRuntime()
	for _, program := range []string{
		`let persistent = 12 / 3; function add(x) {return x + persistent}; assert.eq(7, add(3));`,
		"assert.eq('value=4', `value=${persistent}`); assert.eq(3, [1,2,3].map(x => x*2).length);",
		`const oid = new ObjectId("0123456789abcdef01234567"); assert.eq(oid.toHexString(),"0123456789abcdef01234567"); assert(oid.equals(ObjectId(oid.str)));`,
		`var longValue = NumberLong("9007199254740993"); assert.eq(longValue.toString(),"9007199254740993"); assert.eq(new NumberInt(42).valueOf(),42);`,
		`var document = {long:longValue, decimal:NumberDecimal("123.45"), id:oid, binary:BinData(0,"AAE="), uuid:UUID("00112233-4455-6677-8899-aabbccddeeff"), time:ISODate("2020-01-02T00:00:00Z"),stamp:Timestamp({t:42,i:3}),min:MinKey(),max:MaxKey()}; var restored = EJSON.parse(EJSON.stringify(document)); assert.eq(restored.long.toString(),"9007199254740993"); assert.eq(restored.binary.base64(),"AAE="); assert.eq(restored.time.getUTCFullYear(),2020);`,
		`assert.eq(typeof require,"undefined"); assert.eq(typeof process,"undefined");`,
		`assert.eq(0,Double().valueOf()); assert.eq("ix",EJSON.parse(EJSON.stringify(BSONRegExp("a.*","ix"))).options); assert.eq("i",EJSON.parse(EJSON.stringify(/hello/gi)).options);`,
		`await Promise.resolve(42)`,
	} {
		if err := s.Execute(context.Background(), io.Discard, program); err != nil {
			t.Fatalf("program %s: %v", program, err)
		}
	}
	var output bytes.Buffer
	if err := s.Execute(context.Background(), &output, `document`); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output.Bytes(), []byte(`9007199254740993`)) {
		t.Fatal("lost BSON integer precision")
	}
}

func TestMongoJavaScriptErrors(t *testing.T) {
	s := testMongoRuntime()
	for _, source := range []string{`Promise.reject(new Error("unhandled")); 1`, `(async()=>{throw new Error("async")})()`, `var cycle={};cycle.self=cycle;cycle`} {
		if err := s.Execute(context.Background(), io.Discard, source); err == nil {
			t.Fatalf("expected error for %s", source)
		}
	}
	if err := s.Execute(context.Background(), io.Discard, `Promise.reject(new Error("handled")).catch(()=>{});assert.eq(2,1+1)`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "reject.js")
	if err := os.WriteFile(path, []byte(`(async()=>{throw new Error("file failed")})()`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.ExecuteFile(context.Background(), io.Discard, path); err == nil {
		t.Fatal("file swallowed async rejection")
	}
}

func TestMongoTransactionRetryBoundaries(t *testing.T) {
	s := testMongoRuntime()
	if err := s.Execute(context.Background(), io.Discard, `var attempts=0,commits=0,aborts=0;var fakeSession={startTransaction(){},abortTransaction(){aborts++},commitTransaction(){commits++;if(commits===1)throw {hasErrorLabel:n=>n==="UnknownTransactionCommitResult"}}};`); err != nil {
		t.Fatal(err)
	}
	factory, err := s.vm.RunProgram(mongoTransactionFactory)
	if err != nil {
		t.Fatal(err)
	}
	fn, _ := goja.AssertFunction(factory)
	wrapped, err := fn(goja.Undefined(), s.vm.Get("fakeSession"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.vm.Set("transaction", wrapped); err != nil {
		t.Fatal(err)
	}
	if err = s.Execute(context.Background(), io.Discard, `assert.eq(7,transaction(()=>{attempts++;return 7}));assert.eq(1,attempts);assert.eq(2,commits);assert.eq(0,aborts);`); err != nil {
		t.Fatal(err)
	}
	if err = s.Execute(context.Background(), io.Discard, `attempts=0;transaction(()=>{attempts++;if(attempts===1)throw {hasErrorLabel:n=>n==="TransientTransactionError"};return 9});assert.eq(2,attempts);assert.eq(1,aborts);`); err != nil {
		t.Fatal(err)
	}
}

func TestMongoJavaScriptParsingAndCancellation(t *testing.T) {
	s := testMongoRuntime()
	for _, source := range []string{`let n = 10 / 2; n`, "const template = `a;${1+1}`; template", `db.c.find({pattern:/[;/]/})`, "use another; db.getName()"} {
		statements, incomplete, err := s.Statements(source)
		if err != nil || incomplete || len(statements) != 1 {
			t.Fatalf("parse %q: %v %v %v", source, statements, incomplete, err)
		}
	}
	if _, incomplete, err := s.Statements("function work() {\n"); err != nil || !incomplete {
		t.Fatalf("multiline function: incomplete=%v err=%v", incomplete, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := s.Execute(ctx, io.Discard, `while(true){}`); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unbounded loop was not interrupted: %v", err)
	}
	if err := s.Execute(context.Background(), io.Discard, `assert.eq(2,1+1)`); err != nil {
		t.Fatal("runtime did not recover after cancellation", err)
	}
	if err := s.Execute(context.Background(), io.Discard, `quit()`); !errors.Is(err, errQuit) {
		t.Fatalf("quit: %v", err)
	}
}

func TestMongoLoadAndFileScope(t *testing.T) {
	s := testMongoRuntime()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "inner.js"), []byte("globalThis.loaded = 41;"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outer.js"), []byte("load('inner.js'); assert.eq(42, loaded+1);"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.ExecuteFile(context.Background(), io.Discard, filepath.Join(dir, "outer.js")); err != nil {
		t.Fatal(err)
	}
	if err := s.Execute(context.Background(), io.Discard, `assert.eq(41,loaded)`); err != nil {
		t.Fatal("script state was not retained", err)
	}
}
