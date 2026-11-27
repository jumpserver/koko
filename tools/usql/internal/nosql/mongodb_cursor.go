package nosql

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mongoCursor struct {
	h                      *mongoHandle
	command                bson.D
	cursor                 *mongo.Cursor
	stream                 *mongo.ChangeStream
	watch                  bool
	watchPipeline          any
	watchOptions           bson.D
	pending                bson.D
	ready, started, closed bool
}

func (h *mongoHandle) cursor(command bson.D) *mongoCursor {
	cursor := &mongoCursor{h: h, command: command}
	h.s.cursors[cursor] = struct{}{}
	return cursor
}
func (h *mongoHandle) newCursor(method string, call goja.FunctionCall) goja.Value {
	s := h.s
	var command bson.D
	if method == "find" {
		command = bson.D{{Key: "find", Value: h.collection}, {Key: "filter", Value: s.document(call.Argument(0))}}
		if !goja.IsUndefined(call.Argument(1)) {
			command = append(command, bson.E{Key: "projection", Value: s.document(call.Argument(1))})
		}
		command = append(command, s.document(call.Argument(2))...)
	} else {
		pipeline := s.toBSON(call.Argument(0))
		if pipeline == nil {
			pipeline = bson.A{}
		}
		if _, ok := pipeline.(bson.A); !ok {
			panic(s.vm.NewTypeError("aggregate pipeline must be an array"))
		}
		var collection any = h.collection
		if h.collection == "" {
			collection = int32(1)
		}
		opts := s.document(call.Argument(1))
		cursor := asDocument(docGet(opts, "cursor"))
		if cursor == nil {
			cursor = bson.D{}
		}
		if batch := docGet(opts, "batchSize"); batch != nil {
			cursor = docSet(cursor, "batchSize", batch)
		}
		command = append(bson.D{{Key: "aggregate", Value: collection}, {Key: "pipeline", Value: pipeline}, {Key: "cursor", Value: cursor}}, docWithout(opts, "cursor", "batchSize")...)
	}
	return s.vm.NewDynamicObject(h.cursor(command))
}
func (h *mongoHandle) newWatch(call goja.FunctionCall) goja.Value {
	pipeline := h.s.toBSON(call.Argument(0))
	if pipeline == nil {
		pipeline = bson.A{}
	}
	if _, ok := pipeline.(bson.A); !ok {
		panic(h.s.vm.NewTypeError("watch pipeline must be an array"))
	}
	cursor := h.cursor(nil)
	cursor.watch = true
	cursor.watchPipeline = pipeline
	cursor.watchOptions = h.s.document(call.Argument(1))
	return h.s.vm.NewDynamicObject(cursor)
}
func (c *mongoCursor) Set(string, goja.Value) bool { return false }
func (c *mongoCursor) Delete(string) bool          { return false }
func (c *mongoCursor) Has(key string) bool         { return c.Get(key) != nil }
func (*mongoCursor) Keys() []string {
	return []string{"hasNext", "next", "tryNext", "toArray", "forEach", "map", "sort", "limit", "skip", "batchSize", "close", "explain"}
}
func (c *mongoCursor) Get(key string) goja.Value {
	s := c.h.s
	if key == "then" || key == "toJSON" {
		return nil
	}
	if index, err := strconv.Atoi(key); err == nil && index >= 0 {
		for i := 0; i <= index; i++ {
			if !c.hasNext() {
				return goja.Undefined()
			}
			doc := c.next()
			if i == index {
				return s.fromBSON(doc)
			}
		}
	}
	switch key {
	case "hasNext":
		return s.function(func(goja.FunctionCall) goja.Value { return s.vm.ToValue(c.hasNext()) })
	case "next":
		return s.function(func(goja.FunctionCall) goja.Value {
			if !c.hasNext() {
				panic(s.vm.NewGoError(errors.New("cursor is exhausted")))
			}
			return s.fromBSON(c.next())
		})
	case "tryNext":
		return s.function(func(goja.FunctionCall) goja.Value {
			c.open()
			if c.closed {
				return goja.Null()
			}
			if c.ready {
				return s.fromBSON(c.next())
			}
			var doc bson.D
			var ok bool
			if c.watch {
				ok = c.stream.TryNext(c.h.context())
				if ok {
					s.must(c.stream.Decode(&doc))
				}
				s.must(c.stream.Err())
			} else {
				ok = c.cursor.TryNext(c.h.context())
				if ok {
					s.must(c.cursor.Decode(&doc))
				}
				s.must(c.cursor.Err())
			}
			if !ok {
				return goja.Null()
			}
			return s.fromBSON(doc)
		})
	case "toArray":
		return s.function(func(goja.FunctionCall) goja.Value { return c.array() })
	case "forEach", "map":
		return s.function(func(call goja.FunctionCall) goja.Value {
			fn, ok := goja.AssertFunction(call.Argument(0))
			if !ok {
				panic(s.vm.NewTypeError("callback must be a function"))
			}
			var values []any
			for c.hasNext() {
				value, err := fn(goja.Undefined(), s.fromBSON(c.next()))
				s.must(err)
				if key == "map" {
					if len(values) >= 1_000_000 {
						panic(s.vm.NewTypeError("map result exceeds one million elements"))
					}
					values = append(values, value)
				}
			}
			if key == "map" {
				return s.vm.NewArray(values...)
			}
			return goja.Undefined()
		})
	case "close":
		return s.function(func(goja.FunctionCall) goja.Value { s.must(c.close(c.h.context())); return goja.Undefined() })
	case "isClosed", "isExhausted":
		return s.function(func(goja.FunctionCall) goja.Value { return s.vm.ToValue(c.closed) })
	case "objsLeftInBatch":
		return s.function(func(goja.FunctionCall) goja.Value {
			c.open()
			if c.closed {
				return s.vm.ToValue(0)
			}
			n := 0
			if c.cursor != nil {
				n = c.cursor.RemainingBatchLength()
			}
			if c.ready {
				n++
			}
			return s.vm.ToValue(n)
		})
	case "getResumeToken":
		return s.function(func(goja.FunctionCall) goja.Value {
			c.open()
			if c.stream == nil || len(c.stream.ResumeToken()) == 0 {
				return goja.Null()
			}
			var doc bson.D
			s.must(bson.Unmarshal(c.stream.ResumeToken(), &doc))
			return s.fromBSON(doc)
		})
	case "pretty":
		return s.function(func(goja.FunctionCall) goja.Value { return s.vm.NewDynamicObject(c) })
	case "count", "size", "itcount":
		return s.function(func(call goja.FunctionCall) goja.Value {
			if key == "itcount" {
				n := int64(0)
				for c.hasNext() {
					c.next()
					n++
				}
				return s.fromBSON(n)
			}
			if c.watch || docGet(c.command, "find") == nil {
				panic(s.vm.NewTypeError("count requires a find cursor"))
			}
			cmd := bson.D{{Key: "count", Value: docGet(c.command, "find")}, {Key: "query", Value: docGet(c.command, "filter")}}
			if key == "size" || call.Argument(0).ToBoolean() {
				for _, name := range []string{"skip", "limit"} {
					if value := docGet(c.command, name); value != nil {
						cmd = append(cmd, bson.E{Key: name, Value: value})
					}
				}
			}
			return s.fromBSON(docGet(c.h.command(c.h.database, cmd), "n"))
		})
	case "explain":
		return s.function(func(call goja.FunctionCall) goja.Value {
			verbosity := "queryPlanner"
			if !goja.IsUndefined(call.Argument(0)) {
				verbosity = call.Argument(0).String()
			}
			return s.fromBSON(c.h.command(c.h.database, bson.D{{Key: "explain", Value: c.command}, {Key: "verbosity", Value: verbosity}}))
		})
	case "sort", "limit", "skip", "batchSize", "hint", "min", "max", "maxTimeMS", "collation", "comment", "allowDiskUse", "returnKey", "showRecordId", "noCursorTimeout", "tailable", "awaitData", "projection", "project":
		return s.function(func(call goja.FunctionCall) goja.Value {
			if c.started {
				panic(s.vm.NewTypeError("cannot modify an executed cursor"))
			}
			name := key
			if key == "project" {
				name = "projection"
			}
			value := s.toBSON(call.Argument(0))
			if goja.IsUndefined(call.Argument(0)) {
				switch key {
				case "allowDiskUse", "returnKey", "showRecordId", "noCursorTimeout", "tailable", "awaitData":
					value = true
				default:
					panic(s.vm.NewTypeError(key + " requires an argument"))
				}
			}
			if key == "batchSize" && docGet(c.command, "aggregate") != nil {
				cursor := asDocument(docGet(c.command, "cursor"))
				c.command = docSet(c.command, "cursor", docSet(cursor, "batchSize", value))
			} else {
				c.command = docSet(c.command, name, value)
			}
			return s.vm.NewDynamicObject(c)
		})
	}
	return nil
}
func (c *mongoCursor) open() {
	if c.started || c.closed {
		return
	}
	c.started = true
	succeeded := false
	defer func() {
		if !succeeded {
			_ = c.close(c.h.context())
		}
	}()
	s := c.h.s
	if c.watch {
		opts := options.ChangeStream()
		for _, e := range c.watchOptions {
			switch e.Key {
			case "fullDocument":
				opts.SetFullDocument(options.FullDocument(fmt.Sprint(e.Value)))
			case "fullDocumentBeforeChange":
				opts.SetFullDocumentBeforeChange(options.FullDocument(fmt.Sprint(e.Value)))
			case "resumeAfter":
				opts.SetResumeAfter(e.Value)
			case "startAfter":
				opts.SetStartAfter(e.Value)
			case "startAtOperationTime":
				ts, ok := e.Value.(bson.Timestamp)
				if !ok {
					panic(s.vm.NewTypeError("startAtOperationTime must be a Timestamp"))
				}
				opts.SetStartAtOperationTime(&ts)
			case "batchSize":
				opts.SetBatchSize(int32(numeric(e.Value)))
			case "maxAwaitTimeMS":
				opts.SetMaxAwaitTime(time.Duration(numeric(e.Value)) * time.Millisecond)
			case "showExpandedEvents":
				opts.SetShowExpandedEvents(e.Value == true)
			case "comment":
				opts.SetComment(e.Value)
			default:
				panic(s.vm.NewTypeError("unsupported change-stream option " + e.Key))
			}
		}
		var err error
		if c.h.collection != "" {
			c.stream, err = c.h.client().Database(c.h.database).Collection(c.h.collection).Watch(c.h.context(), c.watchPipeline, opts)
		} else {
			c.stream, err = c.h.client().Database(c.h.database).Watch(c.h.context(), c.watchPipeline, opts)
		}
		s.must(err)
		succeeded = true
		return
	}
	var err error
	opts := options.RunCmd()
	if mongoReadCommand(c.command) && c.h.session == nil && c.h.connection().readPreference != nil {
		opts.SetReadPreference(c.h.connection().readPreference)
	}
	c.cursor, err = c.h.client().Database(c.h.database).RunCommandCursor(c.h.context(), c.h.prepareCommand(c.command), opts)
	s.must(err)
	succeeded = true
}
func (c *mongoCursor) hasNext() bool {
	if c.closed {
		return false
	}
	if c.ready {
		return true
	}
	c.open()
	s := c.h.s
	var ok bool
	if c.watch {
		ok = c.stream.Next(c.h.context())
		if ok {
			s.must(c.stream.Decode(&c.pending))
		}
		s.must(c.stream.Err())
	} else {
		ok = c.cursor.Next(c.h.context())
		if ok {
			s.must(c.cursor.Decode(&c.pending))
		}
		s.must(c.cursor.Err())
	}
	c.ready = ok
	if !ok {
		s.must(c.close(c.h.context()))
	}
	return ok
}
func (c *mongoCursor) next() bson.D {
	if !c.ready && !c.hasNext() {
		panic(c.h.s.vm.NewGoError(errors.New("cursor is exhausted")))
	}
	doc := c.pending
	c.pending = nil
	c.ready = false
	return doc
}
func (c *mongoCursor) array() goja.Value {
	defer c.close(c.h.context())
	var values []any
	for c.hasNext() {
		values = append(values, c.h.s.fromBSON(c.next()))
		if len(values) > 1_000_000 {
			panic(c.h.s.vm.NewTypeError("toArray result exceeds one million documents"))
		}
	}
	return c.h.s.vm.NewArray(values...)
}
func (c *mongoCursor) close(ctx context.Context) error {
	if c.closed {
		return nil
	}
	c.closed = true
	c.pending = nil
	c.ready = false
	delete(c.h.s.cursors, c)
	if c.cursor != nil {
		return c.cursor.Close(ctx)
	}
	if c.stream != nil {
		return c.stream.Close(ctx)
	}
	return nil
}
func (s *mongoSession) printCursor(cursor *mongoCursor) error {
	if cursor.watch {
		_, err := fmt.Fprintln(s.out, "ChangeStream: use next(), tryNext(), hasNext() or forEach().")
		return err
	}
	if s.json {
		return writeExtendedJSON(s.out, s.toBSON(cursor.array()), false)
	}
	count := 0
	for count < s.batchSize && cursor.hasNext() {
		if err := writeExtendedJSON(s.out, cursor.next(), true); err != nil {
			return err
		}
		count++
	}
	if count == 0 {
		_, err := io.WriteString(s.out, "[]\n")
		return err
	}
	if cursor.hasNext() {
		_, err := io.WriteString(s.out, "Type it for more\n")
		return err
	}
	return nil
}
func (h *mongoHandle) explainObject(call goja.FunctionCall) goja.Value {
	s := h.s
	verbosity := "queryPlanner"
	if !goja.IsUndefined(call.Argument(0)) {
		verbosity = call.Argument(0).String()
	}
	object := s.vm.NewObject()
	for _, method := range []string{"find", "aggregate"} {
		method := method
		_ = object.Set(method, func(call goja.FunctionCall) goja.Value {
			cursor := h.newCursor(method, call).ToObject(s.vm).Export().(*mongoCursor)
			defer cursor.close(h.context())
			return s.fromBSON(h.command(h.database, bson.D{{Key: "explain", Value: cursor.command}, {Key: "verbosity", Value: verbosity}}))
		})
	}
	return object
}
