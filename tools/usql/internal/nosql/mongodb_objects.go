package nosql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bytebase/gomongo"
	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type mongoHandle struct {
	s                          *mongoSession
	owner                      *mongoSession
	database, collection, kind string
	session                    *mongo.Session
	props                      map[string]goja.Value
}

func (h *mongoHandle) connection() *mongoSession {
	if h.owner != nil {
		return h.owner
	}
	return h.s
}
func (h *mongoHandle) client() *mongo.Client { return h.connection().client }

func (h *mongoHandle) name() string {
	if h.kind == "connection" {
		return "MongoDB connection"
	}
	if h.collection != "" {
		return h.database + "." + h.collection
	}
	return h.database
}
func (h *mongoHandle) context() context.Context {
	if h.session != nil {
		if _, ok := h.s.sessions[h.session]; !ok {
			panic(h.s.vm.NewTypeError("session has ended"))
		}
		return mongo.NewSessionContext(h.s.ctx, h.session)
	}
	return h.s.ctx
}
func (h *mongoHandle) object(kind, database, collection string) goja.Value {
	return h.s.vm.NewDynamicObject(&mongoHandle{s: h.s, owner: h.owner, kind: kind, database: database, collection: collection, session: h.session, props: make(map[string]goja.Value)})
}
func (h *mongoHandle) Set(key string, value goja.Value) bool { h.props[key] = value; return true }
func (h *mongoHandle) Delete(key string) bool                { delete(h.props, key); return true }
func (h *mongoHandle) Keys() []string {
	keys := []string{"getName", "getDB", "getSiblingDB", "getCollection", "runCommand", "adminCommand", "find", "aggregate", "insertOne", "bulkWrite", "startSession"}
	for key := range h.props {
		keys = append(keys, key)
	}
	return keys
}
func (h *mongoHandle) Has(key string) bool { return h.Get(key) != nil }
func (h *mongoHandle) Get(key string) goja.Value {
	if value, ok := h.props[key]; ok {
		return value
	}
	if key == "then" || key == "toJSON" || strings.HasPrefix(key, "_") {
		return nil
	}
	if key == "toString" || key == "getName" || key == "getFullName" {
		return h.s.function(func(goja.FunctionCall) goja.Value {
			if key == "getName" && h.collection != "" {
				return h.s.vm.ToValue(h.collection)
			}
			return h.s.vm.ToValue(h.name())
		})
	}
	if (h.kind == "database" || h.kind == "collection") && key == "getCollection" {
		return h.s.function(func(call goja.FunctionCall) goja.Value {
			name := call.Argument(0).String()
			if h.collection != "" {
				name = h.collection + "." + name
			}
			return h.object("collection", h.database, name)
		})
	}
	if h.kind == "connection" && (key == "getDB" || key == "getSiblingDB") || h.kind == "database" && key == "getSiblingDB" {
		return h.s.function(func(call goja.FunctionCall) goja.Value { return h.object("database", call.Argument(0).String(), "") })
	}
	if key == "getMongo" || key == "getDB" && h.kind == "collection" {
		return h.s.function(func(goja.FunctionCall) goja.Value {
			if key == "getDB" {
				return h.object("database", h.database, "")
			}
			return h.object("connection", h.database, "")
		})
	}
	if key == "startSession" {
		return h.s.function(func(call goja.FunctionCall) goja.Value { return h.s.newSession(h, call) })
	}
	if key == "getSession" {
		return h.s.function(func(goja.FunctionCall) goja.Value {
			if h.session == nil {
				return goja.Null()
			}
			return h.s.sessionObject(h.session)
		})
	}
	if h.kind == "connection" {
		switch key {
		case "getURI", "getReadPrefMode", "getReadPrefTagSet", "getReadConcern", "getWriteConcern", "setReadPref", "setReadConcern", "setWriteConcern":
			return h.s.function(func(call goja.FunctionCall) goja.Value { return h.connectionCall(key, call) })
		}
	}
	if h.kind == "collection" {
		if key == "find" || key == "aggregate" {
			return h.s.function(func(call goja.FunctionCall) goja.Value { return h.newCursor(key, call) })
		}
		if key == "watch" {
			return h.s.function(func(call goja.FunctionCall) goja.Value { return h.newWatch(call) })
		}
		if collectionMethods[key] || collectionExtraMethods[key] {
			return h.s.function(func(call goja.FunctionCall) goja.Value { return h.collectionCall(key, call) })
		}
		return h.object("collection", h.database, h.collection+"."+key)
	}
	if databaseMethods[key] || databaseExtraMethods[key] || h.kind == "connection" && key == "getDBNames" {
		return h.s.function(func(call goja.FunctionCall) goja.Value { return h.databaseCall(key, call) })
	}
	if h.kind == "database" {
		return h.object("collection", h.database, key)
	}
	return nil
}

var collectionMethods = map[string]bool{
	"runCommand": true,
	"findOne":    true, "insertOne": true, "insertMany": true, "insert": true, "updateOne": true, "updateMany": true, "replaceOne": true, "update": true, "deleteOne": true, "deleteMany": true, "remove": true, "findOneAndUpdate": true, "findOneAndReplace": true, "findOneAndDelete": true, "findAndModify": true, "bulkWrite": true,
	"countDocuments": true, "estimatedDocumentCount": true, "count": true, "distinct": true, "createIndex": true, "createIndexes": true, "ensureIndex": true, "getIndexes": true, "dropIndex": true, "dropIndexes": true, "drop": true, "renameCollection": true, "stats": true, "dataSize": true, "storageSize": true, "totalSize": true, "totalIndexSize": true, "isCapped": true, "validate": true, "latencyStats": true, "explain": true,
	"getShardDistribution": true, "getShardVersion": true, "initializeOrderedBulkOp": true, "initializeUnorderedBulkOp": true,
}
var databaseMethods = map[string]bool{
	"runCommand": true, "adminCommand": true, "runReadCommand": true, "runCommandCursor": true, "aggregate": true, "watch": true, "createCollection": true, "createView": true, "dropDatabase": true, "getCollectionNames": true, "getCollectionInfos": true, "stats": true, "serverStatus": true, "serverBuildInfo": true, "version": true, "hostInfo": true, "listCommands": true, "hello": true, "isMaster": true, "currentOp": true, "killOp": true, "shutdownServer": true, "fsyncLock": true, "fsyncUnlock": true,
	"createUser": true, "updateUser": true, "changeUserPassword": true, "dropUser": true, "dropAllUsers": true, "getUser": true, "getUsers": true, "grantRolesToUser": true, "revokeRolesFromUser": true, "createRole": true, "updateRole": true, "dropRole": true, "dropAllRoles": true, "getRole": true, "getRoles": true, "grantRolesToRole": true, "revokeRolesFromRole": true, "grantPrivilegesToRole": true, "revokePrivilegesFromRole": true,
	"auth": true, "logout": true, "getLastError": true, "getLastErrorObj": true,
}

func strconvQuote(text string) string { raw, _ := json.Marshal(text); return string(raw) }
func docGet(doc bson.D, key string) any {
	for _, e := range doc {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}
func docSet(doc bson.D, key string, value any) bson.D {
	for i := range doc {
		if doc[i].Key == key {
			doc[i].Value = value
			return doc
		}
	}
	return append(doc, bson.E{Key: key, Value: value})
}
func docWithout(doc bson.D, keys ...string) bson.D {
	result := bson.D{}
	for _, e := range doc {
		skip := false
		for _, key := range keys {
			if e.Key == key {
				skip = true
				break
			}
		}
		if !skip {
			result = append(result, e)
		}
	}
	return result
}
func asDocument(value any) bson.D {
	if doc, ok := value.(bson.D); ok {
		return doc
	}
	return nil
}
func numeric(value any) int64 {
	switch v := value.(type) {
	case int32:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

func (h *mongoHandle) command(database string, command bson.D) bson.D {
	return h.runCommand(database, command, mongoReadCommand(command))
}

// Writes and unknown commands always select a primary, regardless of a
// connection's read preference. Explicit runReadCommand can opt into reads.
func (h *mongoHandle) runCommand(database string, command bson.D, read bool) bson.D {
	keys := make(map[string]bool, len(command))
	for _, e := range command {
		if keys[e.Key] {
			panic(h.s.vm.NewTypeError("duplicate command field " + e.Key))
		}
		keys[e.Key] = true
	}
	var result bson.D
	opts := options.RunCmd()
	if read && h.session == nil && h.connection().readPreference != nil {
		opts.SetReadPreference(h.connection().readPreference)
	}
	h.s.must(h.client().Database(database).RunCommand(h.context(), h.prepareCommand(command), opts).Decode(&result))
	return result
}
func (h *mongoHandle) write(command bson.D) bson.D {
	result := h.command(h.database, command)
	var exception mongo.WriteException
	if labels, ok := docGet(result, "errorLabels").(bson.A); ok {
		for _, label := range labels {
			exception.Labels = append(exception.Labels, fmt.Sprint(label))
		}
	}
	if failures, ok := docGet(result, "writeErrors").(bson.A); ok && len(failures) > 0 {
		for _, item := range failures {
			failure := asDocument(item)
			exception.WriteErrors = append(exception.WriteErrors, mongo.WriteError{Index: int(numeric(docGet(failure, "index"))), Code: int(numeric(docGet(failure, "code"))), Message: stringValue(docGet(failure, "errmsg"))})
		}
	}
	if failure := asDocument(docGet(result, "writeConcernError")); len(failure) > 0 {
		exception.WriteConcernError = &mongo.WriteConcernError{Code: int(numeric(docGet(failure, "code"))), Message: stringValue(docGet(failure, "errmsg"))}
	}
	if len(exception.WriteErrors) > 0 || exception.WriteConcernError != nil {
		raw, err := bson.Marshal(result)
		h.s.must(err)
		exception.Raw = raw
		h.s.must(exception)
	}
	return result
}

func (h *mongoHandle) acknowledged(command bson.D) bool {
	w := docGet(asDocument(docGet(h.prepareCommand(command), "writeConcern")), "w")
	return w != int32(0) && w != int64(0) && w != float64(0)
}

func (s *mongoSession) requiredDocument(value goja.Value, name string) bson.D {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		panic(s.vm.NewTypeError(name + " requires a document"))
	}
	return s.document(value)
}
func (h *mongoHandle) helper(method string, call goja.FunctionCall) goja.Value {
	arguments := make([]string, len(call.Arguments))
	for i, value := range call.Arguments {
		literal, err := shellLiteral(h.s.toBSON(value))
		h.s.must(err)
		arguments[i] = literal
	}
	prefix := "db"
	if h.collection != "" {
		prefix += ".getCollection(" + strconvQuote(h.collection) + ")"
	}
	result, err := gomongo.NewClient(h.client()).Execute(h.context(), h.database, prefix+"."+method+"("+strings.Join(arguments, ",")+")")
	h.s.must(err)
	if len(result.Value) == 0 {
		return goja.Null()
	}
	if len(result.Value) == 1 {
		return h.s.fromBSON(result.Value[0])
	}
	return h.s.fromBSON(result.Value)
}

func (h *mongoHandle) collectionCall(method string, call goja.FunctionCall) goja.Value {
	s := h.s
	arg := call.Argument
	switch method {
	case "runCommand":
		return s.fromBSON(h.command(h.database, append(bson.D{{Key: arg(0).String(), Value: h.collection}}, s.document(arg(1))...)))
	case "findOne":
		cursor := h.newCursor("find", goja.FunctionCall{Arguments: call.Arguments}).ToObject(s.vm).Export().(*mongoCursor)
		cursor.command = docSet(cursor.command, "limit", int64(1))
		cursor.command = docSet(cursor.command, "singleBatch", true)
		defer cursor.close(h.context())
		if !cursor.hasNext() {
			return goja.Null()
		}
		return s.fromBSON(cursor.next())
	case "insertOne", "insertMany", "insert":
		var documents bson.A
		if method == "insertMany" || method == "insert" && arg(0).ToObject(s.vm).ClassName() == "Array" {
			value, ok := s.toBSON(arg(0)).(bson.A)
			if !ok {
				panic(s.vm.NewTypeError("insertMany expects an array"))
			}
			documents = value
		} else {
			documents = bson.A{s.requiredDocument(arg(0), method)}
		}
		ids := bson.D{}
		for i, item := range documents {
			doc := asDocument(item)
			if doc == nil {
				panic(s.vm.NewTypeError("insert documents must be objects"))
			}
			id := docGet(doc, "_id")
			if id == nil {
				id = bson.NewObjectID()
				doc = append(bson.D{{Key: "_id", Value: id}}, doc...)
			}
			documents[i] = doc
			ids = append(ids, bson.E{Key: fmt.Sprint(i), Value: id})
		}
		opts := s.document(arg(1))
		ordered := docGet(opts, "ordered")
		if ordered == nil {
			ordered = true
		}
		cmd := append(bson.D{{Key: "insert", Value: h.collection}, {Key: "documents", Value: documents}, {Key: "ordered", Value: ordered}}, docWithout(opts, "ordered")...)
		h.write(cmd)
		ack := h.acknowledged(cmd)
		out := bson.D{{Key: "acknowledged", Value: ack}}
		if method == "insertOne" {
			out = append(out, bson.E{Key: "insertedId", Value: ids[0].Value})
		} else {
			out = append(out, bson.E{Key: "insertedIds", Value: ids})
		}
		return s.fromBSON(out)
	case "updateOne", "updateMany", "replaceOne", "update":
		opts := s.document(arg(2))
		multi := method == "updateMany"
		if method == "update" {
			multi, _ = docGet(opts, "multi").(bool)
		}
		filter := s.requiredDocument(arg(0), method+" filter")
		value := s.toBSON(arg(1))
		if method == "replaceOne" {
			replacement := s.requiredDocument(arg(1), method+" replacement")
			for _, field := range replacement {
				if strings.HasPrefix(field.Key, "$") {
					panic(s.vm.NewTypeError("replacement documents cannot contain update operators"))
				}
			}
			value = replacement
		} else if method != "update" {
			if doc, ok := value.(bson.D); ok {
				if len(doc) == 0 || !strings.HasPrefix(doc[0].Key, "$") {
					panic(s.vm.NewTypeError("update requires update operators or a pipeline"))
				}
			} else if _, ok := value.(bson.A); !ok {
				panic(s.vm.NewTypeError("update requires a document or pipeline"))
			}
		}
		update := bson.D{{Key: "q", Value: filter}, {Key: "u", Value: value}, {Key: "multi", Value: multi}}
		for _, key := range []string{"upsert", "arrayFilters", "hint", "collation", "sort"} {
			if value := docGet(opts, key); value != nil {
				update = append(update, bson.E{Key: key, Value: value})
			}
		}
		cmd := append(bson.D{{Key: "update", Value: h.collection}, {Key: "updates", Value: bson.A{update}}}, docWithout(opts, "multi", "upsert", "arrayFilters", "hint", "collation", "sort")...)
		result := h.write(cmd)
		upserted := int64(0)
		if !h.acknowledged(cmd) {
			return s.fromBSON(mapDocument("acknowledged", false))
		}
		var id any
		if entries, ok := docGet(result, "upserted").(bson.A); ok && len(entries) > 0 {
			upserted = int64(len(entries))
			id = docGet(asDocument(entries[0]), "_id")
		}
		return s.fromBSON(bson.D{{Key: "acknowledged", Value: true}, {Key: "matchedCount", Value: numeric(docGet(result, "n")) - upserted}, {Key: "modifiedCount", Value: docGet(result, "nModified")}, {Key: "upsertedCount", Value: upserted}, {Key: "upsertedId", Value: id}})
	case "deleteOne", "deleteMany", "remove":
		limit := int32(0)
		if method == "deleteOne" {
			limit = 1
		}
		var opts bson.D
		if method == "remove" {
			if value, ok := arg(1).Export().(bool); ok {
				opts = s.document(arg(2))
				if value {
					limit = 1
				}
			} else {
				opts = s.document(arg(1))
				if docGet(opts, "justOne") == true {
					limit = 1
				}
			}
			opts = docWithout(opts, "justOne")
		} else {
			opts = s.document(arg(1))
		}
		entry := bson.D{{Key: "q", Value: s.requiredDocument(arg(0), method+" filter")}, {Key: "limit", Value: limit}}
		for _, key := range []string{"hint", "collation"} {
			if value := docGet(opts, key); value != nil {
				entry = append(entry, bson.E{Key: key, Value: value})
			}
		}
		cmd := append(bson.D{{Key: "delete", Value: h.collection}, {Key: "deletes", Value: bson.A{entry}}}, docWithout(opts, "hint", "collation")...)
		result := h.write(cmd)
		if !h.acknowledged(cmd) {
			return s.fromBSON(mapDocument("acknowledged", false))
		}
		return s.fromBSON(bson.D{{Key: "acknowledged", Value: true}, {Key: "deletedCount", Value: docGet(result, "n")}})
	case "findOneAndUpdate", "findOneAndReplace", "findOneAndDelete", "findAndModify":
		cmd := bson.D{{Key: "findAndModify", Value: h.collection}}
		if method == "findAndModify" {
			cmd = append(cmd, s.requiredDocument(arg(0), method+" options")...)
		} else {
			opts := s.document(arg(2))
			if method == "findOneAndDelete" {
				opts = s.document(arg(1))
				cmd = append(cmd, bson.E{Key: "remove", Value: true})
			} else {
				if goja.IsUndefined(arg(1)) || goja.IsNull(arg(1)) {
					panic(s.vm.NewTypeError(method + " requires an update or replacement"))
				}
				value := s.toBSON(arg(1))
				if method == "findOneAndReplace" {
					value = s.requiredDocument(arg(1), method+" replacement")
					for _, field := range value.(bson.D) {
						if strings.HasPrefix(field.Key, "$") {
							panic(s.vm.NewTypeError("replacement documents cannot contain update operators"))
						}
					}
				} else if doc, ok := value.(bson.D); ok {
					if len(doc) == 0 || !strings.HasPrefix(doc[0].Key, "$") {
						panic(s.vm.NewTypeError("update requires update operators or a pipeline"))
					}
				} else if _, ok := value.(bson.A); !ok {
					panic(s.vm.NewTypeError("update requires a document or pipeline"))
				}
				cmd = append(cmd, bson.E{Key: "update", Value: value})
			}
			cmd = append(cmd, bson.E{Key: "query", Value: s.requiredDocument(arg(0), method+" filter")})
			for _, e := range opts {
				switch e.Key {
				case "query", "update", "remove", "findAndModify":
					panic(s.vm.NewTypeError("reserved findAndModify option " + e.Key))
				case "returnDocument":
					if e.Value != "after" && e.Value != "before" {
						panic(s.vm.NewTypeError("returnDocument must be before or after"))
					}
					cmd = docSet(cmd, "new", e.Value == "after")
				case "projection":
					cmd = docSet(cmd, "fields", e.Value)
				case "returnNewDocument":
					cmd = docSet(cmd, "new", e.Value)
				default:
					cmd = docSet(cmd, e.Key, e.Value)
				}
			}
		}
		return s.fromBSON(docGet(h.write(cmd), "value"))
	case "bulkWrite":
		return h.bulkWrite(call)
	case "initializeOrderedBulkOp", "initializeUnorderedBulkOp":
		return h.bulkBuilder(method == "initializeOrderedBulkOp")
	case "countDocuments":
		pipeline := bson.A{bson.D{{Key: "$match", Value: s.document(arg(0))}}}
		opts := s.document(arg(1))
		for _, key := range []string{"skip", "limit"} {
			if value := docGet(opts, key); value != nil && numeric(value) > 0 {
				pipeline = append(pipeline, bson.D{{Key: "$" + key, Value: value}})
			}
		}
		pipeline = append(pipeline, bson.D{{Key: "$count", Value: "n"}})
		cursor := h.cursor(bson.D{{Key: "aggregate", Value: h.collection}, {Key: "pipeline", Value: pipeline}, {Key: "cursor", Value: bson.D{}}})
		cursor.command = append(cursor.command, docWithout(opts, "skip", "limit")...)
		defer cursor.close(h.context())
		if !cursor.hasNext() {
			return s.fromBSON(int64(0))
		}
		return s.fromBSON(numeric(docGet(cursor.next(), "n")))
	case "count", "estimatedDocumentCount":
		cmd := bson.D{{Key: "count", Value: h.collection}}
		if method == "count" {
			cmd = append(cmd, bson.E{Key: "query", Value: s.document(arg(0))})
			cmd = append(cmd, s.document(arg(1))...)
		} else {
			cmd = append(cmd, s.document(arg(0))...)
		}
		return s.fromBSON(docGet(h.command(h.database, cmd), "n"))
	case "distinct":
		return s.fromBSON(docGet(h.command(h.database, append(bson.D{{Key: "distinct", Value: h.collection}, {Key: "key", Value: arg(0).String()}, {Key: "query", Value: s.document(arg(1))}}, s.document(arg(2))...)), "values"))
	case "createIndex", "ensureIndex", "createIndexes":
		var indexes bson.A
		opts := s.document(arg(1))
		commandOpts := bson.D{}
		for _, key := range []string{"writeConcern", "maxTimeMS", "commitQuorum", "comment"} {
			if value := docGet(opts, key); value != nil {
				commandOpts = append(commandOpts, bson.E{Key: key, Value: value})
			}
		}
		if !goja.IsUndefined(arg(2)) {
			commandOpts = docSet(commandOpts, "commitQuorum", s.toBSON(arg(2)))
		}
		indexOpts := docWithout(opts, "writeConcern", "maxTimeMS", "commitQuorum", "comment")
		if method == "createIndexes" {
			items, ok := s.toBSON(arg(0)).(bson.A)
			if !ok {
				panic(s.vm.NewTypeError("createIndexes requires an array"))
			}
			for _, item := range items {
				doc := asDocument(item)
				if docGet(doc, "key") == nil {
					doc = bson.D{{Key: "key", Value: doc}}
				}
				for _, option := range indexOpts {
					doc = docSet(doc, option.Key, option.Value)
				}
				indexes = append(indexes, doc)
			}
		} else {
			indexes = bson.A{append(bson.D{{Key: "key", Value: s.requiredDocument(arg(0), "index keys")}}, indexOpts...)}
		}
		names := bson.A{}
		for i, item := range indexes {
			spec := asDocument(item)
			name, _ := docGet(spec, "name").(string)
			if name == "" {
				parts := []string{}
				for _, key := range asDocument(docGet(spec, "key")) {
					parts = append(parts, key.Key+"_"+fmt.Sprint(key.Value))
				}
				name = strings.Join(parts, "_")
				spec = docSet(spec, "name", name)
			}
			indexes[i] = spec
			names = append(names, name)
		}
		command := bson.D{{Key: "createIndexes", Value: h.collection}, {Key: "indexes", Value: indexes}}
		command = append(command, commandOpts...)
		h.write(command)
		if method != "createIndexes" {
			return s.fromBSON(names[0])
		}
		return s.fromBSON(names)
	case "getIndexes":
		cursor := h.cursor(bson.D{{Key: "listIndexes", Value: h.collection}, {Key: "cursor", Value: bson.D{}}})
		defer cursor.close(h.context())
		return cursor.array()
	case "dropIndex", "dropIndexes":
		index := s.toBSON(arg(0))
		if index == nil {
			if method == "dropIndex" {
				panic(s.vm.NewTypeError("dropIndex requires an index name or key"))
			}
			index = "*"
		}
		if list, ok := index.(bson.A); ok {
			var result bson.D
			for _, name := range list {
				result = h.write(bson.D{{Key: "dropIndexes", Value: h.collection}, {Key: "index", Value: name}})
			}
			return s.fromBSON(result)
		}
		return s.fromBSON(h.write(append(bson.D{{Key: "dropIndexes", Value: h.collection}, {Key: "index", Value: index}}, s.document(arg(1))...)))
	case "drop":
		h.write(append(bson.D{{Key: "drop", Value: h.collection}}, s.document(arg(0))...))
		return s.vm.ToValue(true)
	case "renameCollection":
		return s.fromBSON(h.command("admin", bson.D{{Key: "renameCollection", Value: h.name()}, {Key: "to", Value: h.database + "." + arg(0).String()}, {Key: "dropTarget", Value: arg(1).ToBoolean()}}))
	case "explain":
		return h.explainObject(call)
	case "getShardVersion":
		return s.fromBSON(h.command("admin", bson.D{{Key: "getShardVersion", Value: h.name()}, {Key: "fullMetadata", Value: true}}))
	case "getShardDistribution":
		return s.fromBSON(h.command(h.database, bson.D{{Key: "aggregate", Value: h.collection}, {Key: "pipeline", Value: bson.A{bson.D{{Key: "$collStats", Value: bson.D{{Key: "storageStats", Value: bson.D{}}}}}}}, {Key: "cursor", Value: bson.D{}}}))
	default:
		if value, ok := h.extraCollectionCall(method, call); ok {
			return value
		}
		return h.helper(method, call)
	}
}

func (h *mongoHandle) databaseCall(method string, call goja.FunctionCall) goja.Value {
	s := h.s
	arg := call.Argument
	switch method {
	case "runCommand", "runReadCommand", "adminCommand":
		database := h.database
		if method == "adminCommand" {
			database = "admin"
		}
		var command bson.D
		if _, ok := arg(0).Export().(string); ok {
			command = bson.D{{Key: arg(0).String(), Value: int32(1)}}
		} else {
			command = s.document(arg(0))
		}
		if len(command) == 0 {
			panic(s.vm.NewTypeError("command document cannot be empty"))
		}
		return s.fromBSON(h.runCommand(database, command, method == "runReadCommand"))
	case "runCommandCursor":
		return s.vm.NewDynamicObject(h.cursor(s.document(arg(0))))
	case "aggregate":
		copy := *h
		copy.collection = ""
		return copy.newCursor("aggregate", call)
	case "watch":
		return h.newWatch(call)
	case "createCollection":
		return s.fromBSON(h.command(h.database, append(bson.D{{Key: "create", Value: arg(0).String()}}, s.document(arg(1))...)))
	case "createView":
		return s.fromBSON(h.command(h.database, append(bson.D{{Key: "create", Value: arg(0).String()}, {Key: "viewOn", Value: arg(1).String()}, {Key: "pipeline", Value: s.toBSON(arg(2))}}, s.document(arg(3))...)))
	case "dropDatabase":
		return s.fromBSON(h.command(h.database, bson.D{{Key: "dropDatabase", Value: int32(1)}}))
	case "getCollectionNames", "getCollectionInfos":
		cursor := h.cursor(bson.D{{Key: "listCollections", Value: int32(1)}, {Key: "filter", Value: s.document(arg(0))}, {Key: "cursor", Value: bson.D{}}})
		defer cursor.close(h.context())
		if method == "getCollectionInfos" {
			return cursor.array()
		}
		names := bson.A{}
		for cursor.hasNext() {
			names = append(names, docGet(cursor.next(), "name"))
		}
		return s.fromBSON(names)
	case "getDBNames":
		result := h.command("admin", bson.D{{Key: "listDatabases", Value: int32(1)}, {Key: "nameOnly", Value: true}})
		names := bson.A{}
		if databases, ok := docGet(result, "databases").(bson.A); ok {
			for _, database := range databases {
				names = append(names, docGet(asDocument(database), "name"))
			}
		}
		return s.fromBSON(names)
	case "hello", "isMaster":
		name := "hello"
		if method == "isMaster" {
			name = "isMaster"
		}
		return s.fromBSON(h.command(h.database, bson.D{{Key: name, Value: int32(1)}}))
	case "currentOp":
		return s.fromBSON(h.command("admin", append(bson.D{{Key: "currentOp", Value: int32(1)}}, s.document(arg(0))...)))
	case "killOp":
		return s.fromBSON(h.command("admin", bson.D{{Key: "killOp", Value: int32(1)}, {Key: "op", Value: s.toBSON(arg(0))}}))
	case "shutdownServer":
		return s.fromBSON(h.command("admin", append(bson.D{{Key: "shutdown", Value: int32(1)}}, s.document(arg(0))...)))
	case "fsyncLock":
		return s.fromBSON(h.command("admin", bson.D{{Key: "fsync", Value: int32(1)}, {Key: "lock", Value: true}}))
	case "fsyncUnlock":
		return s.fromBSON(h.command("admin", bson.D{{Key: "fsyncUnlock", Value: int32(1)}}))
	case "getLastError", "getLastErrorObj":
		panic(s.vm.NewTypeError("legacy getLastError is unavailable; use write results and exceptions"))
	case "auth", "logout":
		return h.connectionCall(method, call)
	default:
		if value, ok := h.extraDatabaseCall(method, call); ok {
			return value
		}
		if value, ok := h.userRoleCall(method, call); ok {
			return value
		}
		return h.helper(method, call)
	}
}
