package nosql

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bytebase/gomongo"
	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (s *mongoSession) activeDatabase() *mongoHandle {
	value := s.vm.Get("db")
	if object, ok := value.(*goja.Object); ok {
		if handle, ok := object.Export().(*mongoHandle); ok && handle.kind == "database" {
			return handle
		}
	}
	panic(s.vm.NewTypeError("db is not a database handle"))
}
func (s *mongoSession) initConnections() {
	s.must(s.vm.Set("Mongo", func(call goja.ConstructorCall) *goja.Object {
		raw := call.Argument(0).String()
		if !strings.HasPrefix(raw, "mongodb://") && !strings.HasPrefix(raw, "mongodb+srv://") {
			raw = "mongodb://" + raw
		}
		u, err := url.Parse(raw)
		s.must(err)
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		backend, err := openMongo(ctx, u, s.json)
		s.must(err)
		other := backend.(*mongoSession)
		s.others = append(s.others, other)
		return s.vm.NewDynamicObject(&mongoHandle{s: s, owner: other, database: other.database, kind: "connection", props: make(map[string]goja.Value)})
	}))
	s.must(s.vm.Set("connect", func(call goja.FunctionCall) goja.Value {
		raw := call.Argument(0).String()
		if !strings.HasPrefix(raw, "mongodb://") && !strings.HasPrefix(raw, "mongodb+srv://") {
			raw = "mongodb://" + raw
		}
		u, err := url.Parse(raw)
		s.must(err)
		if !goja.IsUndefined(call.Argument(1)) {
			u.User = url.UserPassword(call.Argument(1).String(), call.Argument(2).String())
		}
		backend, err := openMongo(s.ctx, u, s.json)
		s.must(err)
		other := backend.(*mongoSession)
		s.others = append(s.others, other)
		return s.vm.NewDynamicObject(&mongoHandle{s: s, owner: other, database: other.database, kind: "database", props: make(map[string]goja.Value)})
	}))
}

func (h *mongoHandle) connectionCall(method string, call goja.FunctionCall) goja.Value {
	s := h.s
	owner := h.connection()
	u, err := url.Parse(owner.uri)
	s.must(err)
	q := u.Query()
	switch method {
	case "getURI":
		return s.vm.ToValue(u.Redacted())
	case "getReadPrefMode":
		mode := q.Get("readPreference")
		if mode == "" {
			mode = "primary"
		}
		return s.vm.ToValue(mode)
	case "getReadPrefTagSet":
		sets := bson.A{}
		for _, raw := range q["readPreferenceTags"] {
			doc := bson.D{}
			for _, entry := range strings.Split(raw, ",") {
				key, value, ok := strings.Cut(entry, ":")
				if ok {
					doc = append(doc, bson.E{Key: key, Value: value})
				}
			}
			sets = append(sets, doc)
		}
		return s.fromBSON(sets)
	case "getReadConcern":
		if owner.readConcern == nil || owner.readConcern.Level == "" {
			return s.fromBSON(bson.D{})
		}
		return s.fromBSON(mapDocument("level", owner.readConcern.Level))
	case "getWriteConcern":
		doc := bson.D{}
		if wc := owner.writeConcern; wc != nil {
			if wc.W != nil {
				doc = append(doc, bson.E{Key: "w", Value: wc.W})
			}
			if wc.Journal != nil {
				doc = append(doc, bson.E{Key: "j", Value: *wc.Journal})
			}
		}
		if timeout := q.Get("wtimeoutMS"); timeout != "" {
			value, err := strconv.ParseInt(timeout, 10, 64)
			s.must(err)
			doc = append(doc, bson.E{Key: "wtimeout", Value: value})
		}
		return s.fromBSON(doc)
	case "setReadPref":
		q.Set("readPreference", call.Argument(0).String())
		if !goja.IsUndefined(call.Argument(1)) {
			sets, ok := s.toBSON(call.Argument(1)).(bson.A)
			if !ok {
				panic(s.vm.NewTypeError("read preference tags must be an array"))
			}
			q.Del("readPreferenceTags")
			for _, entry := range sets {
				parts := []string{}
				for _, tag := range asDocument(entry) {
					parts = append(parts, tag.Key+":"+stringValue(tag.Value))
				}
				q.Add("readPreferenceTags", strings.Join(parts, ","))
			}
		}
	case "setReadConcern":
		level := call.Argument(0).String()
		if object, ok := call.Argument(0).(*goja.Object); ok {
			field := object.Get("level")
			if field == nil || goja.IsUndefined(field) {
				panic(s.vm.NewTypeError("read concern requires level"))
			}
			level = field.String()
		}
		q.Set("readConcernLevel", level)
	case "setWriteConcern":
		for _, entry := range s.document(call.Argument(0)) {
			switch entry.Key {
			case "w":
				q.Set("w", fmt.Sprint(entry.Value))
			case "j":
				q.Set("journal", fmt.Sprint(entry.Value))
			case "wtimeout":
				q.Set("wtimeoutMS", fmt.Sprint(entry.Value))
			default:
				panic(s.vm.NewTypeError("unsupported write concern field " + entry.Key))
			}
		}
	case "auth":
		var user, password, mechanism string
		if _, ok := call.Argument(0).(*goja.Object); ok {
			doc := s.document(call.Argument(0))
			user = stringValue(docGet(doc, "user"))
			password = stringValue(docGet(doc, "pwd"))
			mechanism = stringValue(docGet(doc, "mechanism"))
		} else {
			user = call.Argument(0).String()
			password = call.Argument(1).String()
		}
		u.User = url.UserPassword(user, password)
		q.Set("authSource", h.database)
		if mechanism != "" {
			q.Set("authMechanism", mechanism)
		}
	case "logout":
		u.User = nil
		q.Del("authMechanism")
		q.Del("authMechanismProperties")
	default:
		panic(s.vm.NewTypeError("unsupported Mongo connection method " + method))
	}
	u.RawQuery = q.Encode()
	var replacement *mongo.Client
	if method == "logout" {
		clean := *u
		query := clean.Query()
		proxy := query.Get("proxy")
		query.Del("proxy")
		clean.RawQuery = query.Encode()
		opts := options.Client().ApplyURI(clean.String()).SetAppName("jumpserver-usql")
		if proxy != "" {
			dialer, err := newMongoProxy(proxy)
			s.must(err)
			opts.SetDialer(dialer)
		}
		replacement, err = mongo.Connect(opts)
		s.must(err)
	} else {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		backend, err := openMongo(ctx, u, s.json)
		s.must(err)
		replacement = backend.(*mongoSession).client
	}
	// Authentication and read/write concern changes invalidate the old logical
	// sessions and cursors, matching mongosh's connection-reset behavior.
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	previous := owner.client
	for cursor := range s.cursors {
		if cursor.h.client() == previous {
			s.must(cursor.close(ctx))
		}
	}
	for session := range s.sessions {
		if session.Client() == previous {
			session.EndSession(ctx)
			delete(s.sessions, session)
		}
	}
	owner.client, owner.shell, owner.uri = replacement, gomongo.NewClient(replacement), u.String()
	parsed := options.Client().ApplyURI(u.String())
	owner.readPreference, owner.readConcern, owner.writeConcern = parsed.ReadPreference, parsed.ReadConcern, parsed.WriteConcern
	_ = previous.Disconnect(ctx)
	if method == "auth" {
		return s.vm.ToValue(1)
	}
	if method == "logout" {
		return s.fromBSON(mapDocument("ok", int32(1)))
	}
	return goja.Undefined()
}

func mapDocument(key string, value any) bson.D { return bson.D{{Key: key, Value: value}} }

func (h *mongoHandle) prepareCommand(command bson.D) bson.D {
	if h.session != nil || len(command) == 0 {
		return command
	}
	owner := h.connection()
	name := command[0].Key
	if owner.readConcern != nil && owner.readConcern.Level != "" && docGet(command, "readConcern") == nil {
		switch name {
		case "find", "aggregate", "count", "distinct":
			command = append(command, bson.E{Key: "readConcern", Value: mapDocument("level", owner.readConcern.Level)})
		}
	}
	if owner.writeConcern != nil && docGet(command, "writeConcern") == nil {
		switch name {
		case "insert", "update", "delete", "findAndModify", "create", "drop", "createIndexes", "dropIndexes", "dropDatabase", "renameCollection", "createUser", "updateUser", "dropUser", "createRole", "updateRole", "dropRole":
			wc := bson.D{}
			if owner.writeConcern.W != nil {
				wc = append(wc, bson.E{Key: "w", Value: owner.writeConcern.W})
			}
			if owner.writeConcern.Journal != nil {
				wc = append(wc, bson.E{Key: "j", Value: *owner.writeConcern.Journal})
			}
			if u, err := url.Parse(owner.uri); err == nil {
				if raw := u.Query().Get("wtimeoutMS"); raw != "" {
					value, err := strconv.ParseInt(raw, 10, 64)
					h.s.must(err)
					wc = append(wc, bson.E{Key: "wtimeout", Value: value})
				}
			}
			if len(wc) > 0 {
				command = append(command, bson.E{Key: "writeConcern", Value: wc})
			}
		}
	}
	return command
}

func mongoReadCommand(command bson.D) bool {
	if len(command) == 0 {
		return false
	}
	switch command[0].Key {
	case "find", "count", "distinct", "explain", "listCollections", "listIndexes", "listDatabases", "dbStats", "collStats", "hello", "isMaster", "buildInfo", "serverStatus", "hostInfo", "getCmdLineOpts", "getParameter", "getLog", "listCommands", "usersInfo", "rolesInfo", "replSetGetConfig", "replSetGetStatus", "balancerStatus", "balancerCollectionStatus", "listShards":
		return true
	case "aggregate":
		pipeline, _ := docGet(command, "pipeline").(bson.A)
		for _, stage := range pipeline {
			doc := asDocument(stage)
			if docGet(doc, "$out") != nil || docGet(doc, "$merge") != nil {
				return false
			}
		}
		return true
	}
	return false
}
