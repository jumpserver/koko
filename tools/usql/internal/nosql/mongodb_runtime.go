package nosql

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja/parser"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func (s *mongoSession) initRuntime() {
	s.vm = goja.New()
	s.vm.SetMaxCallStackSize(1024)
	s.rejections = make(map[*goja.Promise]goja.Value)
	s.vm.SetPromiseRejectionTracker(func(p *goja.Promise, operation goja.PromiseRejectionOperation) {
		if operation == goja.PromiseRejectionReject {
			s.rejections[p] = p.Result()
		} else {
			delete(s.rejections, p)
		}
	})
	s.ctx = context.Background()
	s.out = io.Discard
	s.initBSON()
	s.setDatabase(s.database)
	print := func(call goja.FunctionCall) goja.Value {
		for i, value := range call.Arguments {
			if i > 0 {
				_, err := io.WriteString(s.out, " ")
				s.must(err)
			}
			if object, ok := value.(*goja.Object); ok {
				if object.ExportType() == reflect.TypeOf((*mongoBSON)(nil)) {
					_, err := io.WriteString(s.out, object.Export().(*mongoBSON).text())
					s.must(err)
				} else {
					s.must(writeExtendedJSON(s.out, s.toBSON(value), false))
				}
			} else {
				_, err := io.WriteString(s.out, value.String())
				s.must(err)
			}
		}
		_, err := io.WriteString(s.out, "\n")
		s.must(err)
		return goja.Undefined()
	}
	s.must(s.vm.Set("print", print))
	s.must(s.vm.Set("printjson", func(call goja.FunctionCall) goja.Value {
		s.must(writeExtendedJSON(s.out, s.toBSON(call.Argument(0)), true))
		return goja.Undefined()
	}))
	console := s.vm.NewObject()
	_ = console.Set("log", print)
	_ = console.Set("error", print)
	s.must(s.vm.Set("console", console))
	quit := func(goja.FunctionCall) goja.Value { s.vm.Interrupt(errQuit); return goja.Undefined() }
	s.must(s.vm.Set("quit", quit))
	s.must(s.vm.Set("exit", quit))
	s.must(s.vm.Set("assert", func(call goja.FunctionCall) goja.Value {
		if !call.Argument(0).ToBoolean() {
			message := "assertion failed"
			if !goja.IsUndefined(call.Argument(1)) {
				message = call.Argument(1).String()
			}
			panic(s.vm.NewGoError(errors.New(message)))
		}
		return goja.Undefined()
	}))
	assert := s.vm.Get("assert").ToObject(s.vm)
	_ = assert.Set("eq", func(call goja.FunctionCall) goja.Value {
		a := s.toBSON(call.Argument(0))
		b := s.toBSON(call.Argument(1))
		if !reflect.DeepEqual(a, b) && !call.Argument(0).Equals(call.Argument(1)) {
			panic(s.vm.NewGoError(fmt.Errorf("assert.eq failed: %v != %v", a, b)))
		}
		return goja.Undefined()
	})
	_ = assert.Set("commandWorked", func(call goja.FunctionCall) goja.Value {
		object := call.Argument(0).ToObject(s.vm)
		ok := object.Get("ok")
		if ok == nil || ok.ToFloat() != 1 {
			panic(s.vm.NewGoError(errors.New("command did not succeed")))
		}
		return call.Argument(0)
	})
	s.must(s.vm.Set("sleep", func(call goja.FunctionCall) goja.Value {
		ms := call.Argument(0).ToInteger()
		if ms < 0 {
			panic(s.vm.NewTypeError("sleep duration must be nonnegative"))
		}
		timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-s.ctx.Done():
			s.must(s.ctx.Err())
		}
		return goja.Undefined()
	}))
	s.must(s.vm.Set("cat", func(call goja.FunctionCall) goja.Value {
		contents, err := s.readScript(call.Argument(0).String())
		s.must(err)
		return s.vm.ToValue(string(contents))
	}))
	s.must(s.vm.Set("load", func(call goja.FunctionCall) goja.Value {
		for _, value := range call.Arguments {
			s.must(s.load(value.String()))
		}
		return s.vm.ToValue(true)
	}))
	s.must(s.vm.Set("it", func(goja.FunctionCall) goja.Value {
		if s.current == nil {
			panic(s.vm.NewGoError(errors.New("no current cursor")))
		}
		s.must(s.printCursor(s.current))
		return goja.Undefined()
	}))
	s.must(s.vm.Set("__use", func(call goja.FunctionCall) goja.Value {
		name := call.Argument(0).String()
		if name == "" || strings.ContainsAny(name, " /\\\"$\x00\r\n\t") || len(name) > 63 {
			panic(s.vm.NewTypeError("invalid database name"))
		}
		s.setDatabase(name)
		return s.fromBSON(bson.D{{Key: "database", Value: name}})
	}))
	s.must(s.vm.Set("__show", func(call goja.FunctionCall) goja.Value { return s.show(call.Argument(0).String()) }))
	s.initAdmin()
	s.initConnections()
}

func (s *mongoSession) setDatabase(name string) {
	s.database = name
	var owner *mongoSession
	if object, ok := s.vm.Get("db").(*goja.Object); ok {
		if current, ok := object.Export().(*mongoHandle); ok {
			owner = current.owner
		}
	}
	s.must(s.vm.Set("db", s.vm.NewDynamicObject(&mongoHandle{s: s, owner: owner, database: name, kind: "database", props: make(map[string]goja.Value)})))
}

func (s *mongoSession) executeProgram(ctx context.Context, w io.Writer, filename, source string, printResult bool) (resultErr error) {
	// Lazy cursor execution and BSON rendering can fail after RunProgram has
	// returned. Convert their JS exceptions at the same command boundary.
	defer func() {
		if reason := recover(); reason != nil {
			switch reason := reason.(type) {
			case *goja.Object:
				resultErr = fmt.Errorf("%s", reason.String())
			case *goja.Exception:
				resultErr = reason
			default:
				panic(reason)
			}
		}
	}()
	if s.vm == nil {
		s.initRuntime()
	}
	source = normalizeMongoSource(source)
	program, err := goja.Compile(filename, source, false)
	if err != nil {
		return err
	}
	s.ctx, s.out = ctx, w
	clear(s.rejections)
	finished := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { s.vm.Interrupt(ctx.Err()); close(finished) })
	defer func() {
		if !stop() {
			<-finished
		}
		s.vm.ClearInterrupt()
		s.ctx = context.Background()
		s.out = io.Discard
	}()
	value, err := s.vm.RunProgram(program)
	if err != nil {
		var interrupted *goja.InterruptedError
		if errors.As(err, &interrupted) {
			if cause, ok := interrupted.Value().(error); ok {
				return cause
			}
		}
		return err
	}
	if object, ok := s.vm.Get("db").(*goja.Object); ok && object.ExportType() == reflect.TypeOf((*mongoHandle)(nil)) {
		handle := object.Export().(*mongoHandle)
		if handle.kind == "database" {
			s.database = handle.database
		}
	}
	for _, reason := range s.rejections {
		return fmt.Errorf("unhandled JavaScript promise rejection: %s", reason.String())
	}
	if value != nil {
		if promise, ok := value.Export().(*goja.Promise); ok {
			switch promise.State() {
			case goja.PromiseStateFulfilled:
				value = promise.Result()
			case goja.PromiseStateRejected:
				return fmt.Errorf("JavaScript promise rejected: %s", promise.Result().String())
			default:
				return errors.New("pending asynchronous work has no event source")
			}
		}
	}
	if !printResult || value == nil || goja.IsUndefined(value) {
		return nil
	}
	if object, ok := value.(*goja.Object); ok {
		switch object.ExportType() {
		case reflect.TypeOf((*mongoCursor)(nil)):
			cursor := object.Export().(*mongoCursor)
			s.current = cursor
			return s.printCursor(cursor)
		case reflect.TypeOf((*mongoHandle)(nil)):
			handle := object.Export().(*mongoHandle)
			if s.json {
				return writeExtendedJSON(w, handle.name(), false)
			}
			_, err = fmt.Fprintln(w, handle.name())
			return err
		}
	}
	result := s.toBSON(value)
	if !s.json {
		if text, ok := result.(string); ok {
			_, err = fmt.Fprintln(w, text)
			return err
		}
	}
	// Preserve the existing -J contract of one result array per command.
	if s.json {
		if _, ok := result.(bson.A); !ok {
			result = bson.A{result}
		}
	}
	return writeExtendedJSON(w, result, !s.json)
}

// Parsing whole JavaScript programs handles division, regexes, template
// strings, functions and semicolons using the same grammar as execution.
func (s *mongoSession) Statements(source string) ([]string, bool, error) {
	_, err := parser.ParseFile(nil, "<shell>", normalizeMongoSource(source), 0)
	if err != nil {
		var list parser.ErrorList
		if errors.As(err, &list) {
			for _, item := range list {
				if strings.Contains(item.Message, "Unexpected end of input") {
					return nil, true, nil
				}
			}
		}
		return nil, false, err
	}
	if strings.TrimSpace(source) == "" {
		return nil, false, nil
	}
	return []string{source}, false, nil
}

// Shell-only commands are accepted as standalone lines or statements. Normal
// JavaScript is never interpolated into a server command by this rewrite.
func normalizeMongoSource(source string) string {
	trimmed := strings.TrimSpace(source)
	if strings.HasPrefix(trimmed, "await ") {
		return "(async()=>{return (" + strings.TrimSuffix(strings.TrimSpace(trimmed[6:]), ";") + ");})()"
	}
	first, rest := trimmed, ""
	if index := strings.IndexAny(trimmed, ";\n\r"); index >= 0 {
		first, rest = strings.TrimSpace(trimmed[:index]), trimmed[index+1:]
	}
	if trimmed == "it" || trimmed == "it;" {
		return "it()"
	}
	if first == "show dbs" || first == "show databases" || first == "show collections" || first == "show tables" || first == "show users" || first == "show roles" || first == "show profile" {
		return "__show(" + strconvQuote(strings.TrimSpace(first[4:])) + ");\n" + normalizeMongoSource(rest)
	}
	if strings.HasPrefix(first, "use ") || strings.HasPrefix(first, "use\t") {
		return "__use(" + strconvQuote(strings.TrimSpace(first[3:])) + ");\n" + normalizeMongoSource(rest)
	}
	return source
}

func (s *mongoSession) readScript(name string) ([]byte, error) {
	if !filepath.IsAbs(name) && s.scriptDir != "" {
		name = filepath.Join(s.scriptDir, name)
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, 4*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > 4*1024*1024 {
		return nil, errors.New("MongoDB script exceeds 4 MiB")
	}
	return contents, nil
}
func (s *mongoSession) load(name string) error {
	if !filepath.IsAbs(name) && s.scriptDir != "" {
		name = filepath.Join(s.scriptDir, name)
	}
	absolute, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	name = absolute
	contents, err := s.readScript(name)
	if err != nil {
		return err
	}
	previous := s.scriptDir
	s.scriptDir = filepath.Dir(name)
	defer func() { s.scriptDir = previous }()
	_, err = s.vm.RunScript(name, normalizeMongoSource(string(contents)))
	return err
}
func (s *mongoSession) ExecuteFile(ctx context.Context, w io.Writer, name string) error {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	contents, err := s.readScript(absolute)
	if err != nil {
		return err
	}
	previous := s.scriptDir
	s.scriptDir = filepath.Dir(absolute)
	defer func() { s.scriptDir = previous }()
	return s.executeProgram(ctx, w, absolute, string(contents), false)
}
