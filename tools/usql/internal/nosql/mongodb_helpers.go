package nosql

import (
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var databaseExtraMethods = map[string]bool{"serverBits": true, "serverCmdLineOpts": true, "rotateCertificates": true, "getProfilingStatus": true, "setProfilingLevel": true, "setLogLevel": true, "getLogComponents": true, "commandHelp": true, "getReplicationInfo": true, "printReplicationInfo": true, "printSecondaryReplicationInfo": true, "printShardingStatus": true, "printCollectionStats": true, "setSecondaryOk": true, "sql": true, "checkMetadataConsistency": true}
var collectionExtraMethods = map[string]bool{"checkMetadataConsistency": true, "createSearchIndex": true, "createSearchIndexes": true, "getSearchIndexes": true, "dropSearchIndex": true, "updateSearchIndex": true, "hideIndex": true, "unhideIndex": true, "getPlanCache": true, "mapReduce": true}

func (h *mongoHandle) extraDatabaseCall(method string, call goja.FunctionCall) (goja.Value, bool) {
	s := h.s
	arg := call.Argument
	var command bson.D
	database := h.database
	field := ""
	switch method {
	case "stats":
		command = bson.D{{Key: "dbStats", Value: int32(1)}}
		if value := s.toBSON(arg(0)); value != nil {
			if doc, ok := value.(bson.D); ok {
				command = append(command, doc...)
			} else {
				command = append(command, bson.E{Key: "scale", Value: value})
			}
		}
	case "serverStatus":
		database = "admin"
		command = append(bson.D{{Key: "serverStatus", Value: int32(1)}}, s.document(arg(0))...)
	case "serverBuildInfo", "serverBits", "version":
		database = "admin"
		command = bson.D{{Key: "buildInfo", Value: int32(1)}}
		if method == "serverBits" {
			field = "bits"
		}
		if method == "version" {
			field = "version"
		}
	case "hostInfo":
		database = "admin"
		command = bson.D{{Key: "hostInfo", Value: int32(1)}}
	case "serverCmdLineOpts":
		database = "admin"
		command = bson.D{{Key: "getCmdLineOpts", Value: int32(1)}}
	case "rotateCertificates":
		database = "admin"
		command = bson.D{{Key: "rotateCertificates", Value: int32(1)}}
		if !goja.IsUndefined(arg(0)) {
			command = append(command, bson.E{Key: "message", Value: arg(0).String()})
		}
	case "getProfilingStatus":
		command = bson.D{{Key: "profile", Value: int32(-1)}}
	case "setProfilingLevel":
		level := arg(0).ToInteger()
		if level < 0 || level > 2 {
			panic(s.vm.NewTypeError("profiling level must be 0..2"))
		}
		command = bson.D{{Key: "profile", Value: level}}
		if value := s.toBSON(arg(1)); value != nil {
			if doc, ok := value.(bson.D); ok {
				command = append(command, doc...)
			} else {
				command = append(command, bson.E{Key: "slowms", Value: value})
			}
		}
	case "getLogComponents":
		database = "admin"
		command = bson.D{{Key: "getParameter", Value: int32(1)}, {Key: "logComponentVerbosity", Value: int32(1)}}
		field = "logComponentVerbosity"
	case "setLogLevel":
		database = "admin"
		component := ""
		if !goja.IsUndefined(arg(1)) {
			component = arg(1).String()
		}
		verbosity := bson.D{{Key: "verbosity", Value: arg(0).ToInteger()}}
		if component != "" {
			parts := strings.Split(component, ".")
			for i := len(parts) - 1; i >= 0; i-- {
				verbosity = bson.D{{Key: parts[i], Value: verbosity}}
			}
		}
		command = bson.D{{Key: "setParameter", Value: int32(1)}, {Key: "logComponentVerbosity", Value: verbosity}}
	case "commandHelp":
		command = bson.D{{Key: arg(0).String(), Value: int32(1)}, {Key: "help", Value: true}}
	case "listCommands":
		command = bson.D{{Key: "listCommands", Value: int32(1)}}
	case "checkMetadataConsistency":
		return s.vm.NewDynamicObject(h.cursor(append(bson.D{{Key: "checkMetadataConsistency", Value: int32(1)}, {Key: "cursor", Value: bson.D{}}}, s.document(arg(0))...))), true
	case "sql":
		pipeline := bson.A{bson.D{{Key: "$sql", Value: bson.D{{Key: "statement", Value: arg(0).String()}, {Key: "format", Value: "jdbc"}, {Key: "dialect", Value: "mongosql"}, {Key: "formatVersion", Value: int32(1)}}}}}
		return h.newCursor("aggregate", goja.FunctionCall{Arguments: []goja.Value{s.fromBSON(pipeline), arg(1)}}), true
	case "getReplicationInfo", "printReplicationInfo":
		local := &mongoHandle{s: s, owner: h.owner, database: "local", collection: "oplog.rs", kind: "collection"}
		stats := local.command("local", bson.D{{Key: "collStats", Value: "oplog.rs"}})
		first := local.cursor(bson.D{{Key: "find", Value: "oplog.rs"}, {Key: "sort", Value: mapDocument("$natural", int32(1))}, {Key: "limit", Value: int32(1)}, {Key: "singleBatch", Value: true}})
		defer first.close(h.context())
		last := local.cursor(bson.D{{Key: "find", Value: "oplog.rs"}, {Key: "sort", Value: mapDocument("$natural", int32(-1))}, {Key: "limit", Value: int32(1)}, {Key: "singleBatch", Value: true}})
		defer last.close(h.context())
		if !first.hasNext() || !last.hasNext() {
			panic(s.vm.NewTypeError("replica-set oplog is empty"))
		}
		start, ok1 := docGet(first.next(), "ts").(bson.Timestamp)
		end, ok2 := docGet(last.next(), "ts").(bson.Timestamp)
		if !ok1 || !ok2 {
			panic(s.vm.NewTypeError("invalid oplog timestamps"))
		}
		diff := int64(end.T) - int64(start.T)
		result := bson.D{{Key: "logSizeMB", Value: float64(numeric(docGet(stats, "maxSize"))) / 1048576}, {Key: "usedMB", Value: float64(numeric(docGet(stats, "size"))) / 1048576}, {Key: "timeDiff", Value: diff}, {Key: "timeDiffHours", Value: float64(diff) / 3600}, {Key: "tFirst", Value: bson.NewDateTimeFromTime(time.Unix(int64(start.T), 0))}, {Key: "tLast", Value: bson.NewDateTimeFromTime(time.Unix(int64(end.T), 0))}, {Key: "now", Value: bson.NewDateTimeFromTime(time.Now())}}
		if method == "printReplicationInfo" {
			s.must(writeExtendedJSON(s.out, result, true))
			return goja.Undefined(), true
		}
		return s.fromBSON(result), true
	case "printSecondaryReplicationInfo":
		result := h.command("admin", bson.D{{Key: "replSetGetStatus", Value: int32(1)}})
		s.must(writeExtendedJSON(s.out, docGet(result, "members"), true))
		return goja.Undefined(), true
	case "setSecondaryOk":
		mode := "secondaryPreferred"
		if !goja.IsUndefined(arg(0)) && !arg(0).ToBoolean() {
			mode = "primary"
		}
		return h.connectionCall("setReadPref", goja.FunctionCall{Arguments: []goja.Value{s.vm.ToValue(mode)}}), true
	case "printShardingStatus":
		fn, _ := goja.AssertFunction(s.vm.Get("sh").ToObject(s.vm).Get("status"))
		value, err := fn(goja.Undefined(), call.Arguments...)
		s.must(err)
		s.must(writeExtendedJSON(s.out, s.toBSON(value), true))
		return goja.Undefined(), true
	case "printCollectionStats":
		names := h.databaseCall("getCollectionNames", goja.FunctionCall{}).ToObject(s.vm)
		for i := int64(0); i < names.Get("length").ToInteger(); i++ {
			name := names.Get(fmt.Sprint(i)).String()
			value := h.object("collection", h.database, name).ToObject(s.vm).Export().(*mongoHandle).helper("stats", call)
			s.must(writeExtendedJSON(s.out, s.toBSON(value), true))
		}
		return goja.Undefined(), true
	default:
		return nil, false
	}
	result := h.command(database, command)
	if field != "" {
		return s.fromBSON(docGet(result, field)), true
	}
	return s.fromBSON(result), true
}

func (h *mongoHandle) extraCollectionCall(method string, call goja.FunctionCall) (goja.Value, bool) {
	s := h.s
	arg := call.Argument
	switch method {
	case "checkMetadataConsistency":
		return s.vm.NewDynamicObject(h.cursor(append(bson.D{{Key: "checkMetadataConsistency", Value: h.collection}, {Key: "cursor", Value: bson.D{}}}, s.document(arg(0))...))), true
	case "hideIndex", "unhideIndex":
		index := s.toBSON(arg(0))
		spec := bson.D{{Key: "hidden", Value: method == "hideIndex"}}
		if _, ok := index.(string); ok {
			spec = append(spec, bson.E{Key: "name", Value: index})
		} else {
			spec = append(spec, bson.E{Key: "keyPattern", Value: index})
		}
		return s.fromBSON(h.command(h.database, bson.D{{Key: "collMod", Value: h.collection}, {Key: "index", Value: spec}})), true
	case "createSearchIndex", "createSearchIndexes":
		var indexes bson.A
		if method == "createSearchIndexes" {
			var ok bool
			indexes, ok = s.toBSON(arg(0)).(bson.A)
			if !ok || len(indexes) == 0 {
				panic(s.vm.NewTypeError("createSearchIndexes requires a nonempty array"))
			}
		} else {
			var spec bson.D
			if _, ok := arg(0).Export().(string); ok {
				definition := arg(1)
				spec = mapDocument("name", arg(0).String())
				if _, ok := arg(1).Export().(string); ok {
					spec = append(spec, bson.E{Key: "type", Value: arg(1).String()})
					definition = arg(2)
				}
				spec = append(spec, bson.E{Key: "definition", Value: s.requiredDocument(definition, "search index definition")})
			} else {
				spec = s.requiredDocument(arg(0), "search index definition")
				if docGet(spec, "definition") == nil {
					spec = bson.D{{Key: "name", Value: "default"}, {Key: "definition", Value: spec}}
					if !goja.IsUndefined(arg(1)) {
						spec = append(spec, bson.E{Key: "type", Value: arg(1).String()})
					}
				}
			}
			indexes = bson.A{spec}
		}
		for i, item := range indexes {
			spec := asDocument(item)
			if docGet(spec, "definition") == nil {
				panic(s.vm.NewTypeError("search index requires definition"))
			}
			if docGet(spec, "type") == "search" {
				spec = docWithout(spec, "type")
			}
			indexes[i] = spec
		}
		result := h.command(h.database, bson.D{{Key: "createSearchIndexes", Value: h.collection}, {Key: "indexes", Value: indexes}})
		created, _ := docGet(result, "indexesCreated").(bson.A)
		names := bson.A{}
		for _, item := range created {
			names = append(names, docGet(asDocument(item), "name"))
		}
		if method == "createSearchIndex" && len(names) > 0 {
			return s.fromBSON(names[0]), true
		}
		return s.fromBSON(names), true
	case "dropSearchIndex":
		return s.fromBSON(h.command(h.database, bson.D{{Key: "dropSearchIndex", Value: h.collection}, {Key: "name", Value: arg(0).String()}})), true
	case "updateSearchIndex":
		return s.fromBSON(h.command(h.database, bson.D{{Key: "updateSearchIndex", Value: h.collection}, {Key: "name", Value: arg(0).String()}, {Key: "definition", Value: s.document(arg(1))}})), true
	case "getSearchIndexes":
		filter := bson.D{}
		opts := arg(1)
		if _, ok := arg(0).(*goja.Object); ok {
			opts = arg(0)
		} else if !goja.IsUndefined(arg(0)) {
			filter = mapDocument("name", arg(0).String())
		}
		cursor := h.cursor(append(bson.D{{Key: "aggregate", Value: h.collection}, {Key: "pipeline", Value: bson.A{mapDocument("$listSearchIndexes", filter)}}, {Key: "cursor", Value: bson.D{}}}, s.document(opts)...))
		return cursor.array(), true
	case "mapReduce":
		return s.fromBSON(h.command(h.database, append(bson.D{{Key: "mapReduce", Value: h.collection}, {Key: "map", Value: s.toBSON(arg(0))}, {Key: "reduce", Value: s.toBSON(arg(1))}}, s.document(arg(2))...))), true
	case "getPlanCache":
		object := s.vm.NewObject()
		_ = object.Set("list", func(call goja.FunctionCall) goja.Value {
			pipeline := bson.A{mapDocument("$planCacheStats", bson.D{})}
			if !goja.IsUndefined(call.Argument(0)) {
				extra, ok := s.toBSON(call.Argument(0)).(bson.A)
				if !ok {
					panic(s.vm.NewTypeError("plan cache pipeline must be an array"))
				}
				pipeline = append(pipeline, extra...)
			}
			return h.cursor(bson.D{{Key: "aggregate", Value: h.collection}, {Key: "pipeline", Value: pipeline}, {Key: "cursor", Value: bson.D{}}}).array()
		})
		_ = object.Set("clear", func(goja.FunctionCall) goja.Value {
			return s.fromBSON(h.command(h.database, bson.D{{Key: "planCacheClear", Value: h.collection}}))
		})
		_ = object.Set("clearPlansByQuery", func(call goja.FunctionCall) goja.Value {
			return s.fromBSON(h.command(h.database, bson.D{{Key: "planCacheClear", Value: h.collection}, {Key: "query", Value: s.document(call.Argument(0))}, {Key: "projection", Value: s.document(call.Argument(1))}, {Key: "sort", Value: s.document(call.Argument(2))}}))
		})
		return object, true
	default:
		return nil, false
	}
}
