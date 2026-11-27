package nosql

import (
	"fmt"
	"strings"

	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

func (s *mongoSession) show(target string) goja.Value {
	h := s.activeDatabase()
	switch target {
	case "dbs", "databases":
		return h.databaseCall("getDBNames", goja.FunctionCall{})
	case "collections", "tables":
		return h.databaseCall("getCollectionNames", goja.FunctionCall{})
	case "users":
		return h.databaseCall("getUsers", goja.FunctionCall{})
	case "roles":
		return h.databaseCall("getRoles", goja.FunctionCall{})
	case "profile":
		return h.object("collection", s.database, "system.profile").ToObject(s.vm).Export().(*mongoHandle).newCursor("find", goja.FunctionCall{})
	default:
		panic(s.vm.NewTypeError("unsupported show target " + target))
	}
}

func (h *mongoHandle) userRoleCall(method string, call goja.FunctionCall) (goja.Value, bool) {
	s := h.s
	arg := call.Argument
	var command bson.D
	field := ""
	switch method {
	case "createUser", "updateUser", "createRole", "updateRole":
		doc := s.document(arg(0))
		field = "user"
		if strings.HasSuffix(method, "Role") {
			field = "role"
		}
		name := docGet(doc, field)
		if name == nil {
			panic(s.vm.NewTypeError(method + " requires " + field))
		}
		command = append(bson.D{{Key: method, Value: name}}, docWithout(doc, field)...)
	case "changeUserPassword":
		command = bson.D{{Key: "updateUser", Value: arg(0).String()}, {Key: "pwd", Value: arg(1).String()}}
	case "dropUser", "dropRole":
		command = bson.D{{Key: method, Value: arg(0).String()}}
	case "dropAllUsers":
		command = bson.D{{Key: "dropAllUsersFromDatabase", Value: int32(1)}}
	case "dropAllRoles":
		command = bson.D{{Key: "dropAllRolesFromDatabase", Value: int32(1)}}
	case "getUser":
		command = append(bson.D{{Key: "usersInfo", Value: arg(0).String()}}, s.document(arg(1))...)
		field = "users"
	case "getUsers":
		command = append(bson.D{{Key: "usersInfo", Value: int32(1)}}, s.document(arg(0))...)
		field = "users"
	case "getRole":
		command = append(bson.D{{Key: "rolesInfo", Value: arg(0).String()}}, s.document(arg(1))...)
		field = "roles"
	case "getRoles":
		command = append(bson.D{{Key: "rolesInfo", Value: int32(1)}}, s.document(arg(0))...)
		field = "roles"
	case "grantRolesToUser", "revokeRolesFromUser", "grantRolesToRole", "revokeRolesFromRole":
		command = bson.D{{Key: method, Value: arg(0).String()}, {Key: "roles", Value: s.toBSON(arg(1))}}
	case "grantPrivilegesToRole", "revokePrivilegesFromRole":
		command = bson.D{{Key: method, Value: arg(0).String()}, {Key: "privileges", Value: s.toBSON(arg(1))}}
	default:
		return nil, false
	}

	wcIndex := -1
	switch method {
	case "createUser", "updateUser", "createRole", "updateRole", "dropUser", "dropRole":
		wcIndex = 1
	case "dropAllUsers", "dropAllRoles":
		wcIndex = 0
	case "changeUserPassword", "grantRolesToUser", "revokeRolesFromUser", "grantRolesToRole", "revokeRolesFromRole", "grantPrivilegesToRole", "revokePrivilegesFromRole":
		wcIndex = 2
	}
	if wcIndex >= 0 && !goja.IsUndefined(arg(wcIndex)) {
		command = append(command, bson.E{Key: "writeConcern", Value: s.document(arg(wcIndex))})
	}
	result := h.command(h.database, command)
	if field == "users" || field == "roles" {
		values, _ := docGet(result, field).(bson.A)
		if method == "getUser" || method == "getRole" {
			if len(values) == 0 {
				return goja.Null(), true
			}
			return s.fromBSON(values[0]), true
		}
		if values == nil {
			values = bson.A{}
		}
		return s.fromBSON(values), true
	}
	if method == "dropUser" || method == "dropRole" {
		return s.vm.ToValue(true), true
	}
	return s.fromBSON(result), true
}

func (s *mongoSession) initAdmin() {
	admin := func(command bson.D) bson.D { return s.activeDatabase().command("admin", command) }
	rs := s.vm.NewObject()
	for name, command := range map[string]string{"status": "replSetGetStatus", "config": "replSetGetConfig", "conf": "replSetGetConfig", "isMaster": "hello", "hello": "hello"} {
		name, command := name, command
		_ = rs.Set(name, func(goja.FunctionCall) goja.Value {
			result := admin(bson.D{{Key: command, Value: int32(1)}})
			if name == "config" || name == "conf" {
				return s.fromBSON(docGet(result, "config"))
			}
			return s.fromBSON(result)
		})
	}
	_ = rs.Set("initiate", func(call goja.FunctionCall) goja.Value {
		return s.fromBSON(admin(bson.D{{Key: "replSetInitiate", Value: s.document(call.Argument(0))}}))
	})
	reconfig := func(config bson.D, opts bson.D) goja.Value {
		current := asDocument(docGet(admin(bson.D{{Key: "replSetGetConfig", Value: int32(1)}}), "config"))
		config = docSet(config, "version", numeric(docGet(current, "version"))+1)
		return s.fromBSON(admin(append(bson.D{{Key: "replSetReconfig", Value: config}}, opts...)))
	}
	_ = rs.Set("reconfig", func(call goja.FunctionCall) goja.Value {
		return reconfig(s.document(call.Argument(0)), s.document(call.Argument(1)))
	})
	for _, name := range []string{"add", "addArb", "remove"} {
		name := name
		_ = rs.Set(name, func(call goja.FunctionCall) goja.Value {
			config := asDocument(docGet(admin(bson.D{{Key: "replSetGetConfig", Value: int32(1)}}), "config"))
			members, _ := docGet(config, "members").(bson.A)
			next := bson.A{}
			maxID := int64(-1)
			for _, member := range members {
				doc := asDocument(member)
				if id := numeric(docGet(doc, "_id")); id > maxID {
					maxID = id
				}
				if name != "remove" || docGet(doc, "host") != call.Argument(0).String() {
					next = append(next, member)
				}
			}
			if name == "remove" && len(next) == len(members) {
				panic(s.vm.NewTypeError("replica-set member not found"))
			}
			if name != "remove" {
				var member bson.D
				if object, ok := call.Argument(0).(*goja.Object); ok {
					member = s.document(object)
				} else {
					member = bson.D{{Key: "host", Value: call.Argument(0).String()}}
				}
				if docGet(member, "_id") == nil {
					member = docSet(member, "_id", maxID+1)
				}
				if name == "addArb" || call.Argument(1).ToBoolean() {
					member = docSet(member, "arbiterOnly", true)
				}
				next = append(next, member)
			}
			config = docSet(config, "members", next)
			return reconfig(config, bson.D{})
		})
	}
	for name, spec := range map[string][]string{"stepDown": {"replSetStepDown", "seconds", "catchUpSeconds"}, "freeze": {"replSetFreeze", "seconds"}, "syncFrom": {"replSetSyncFrom", "host"}} {
		name, spec := name, spec
		_ = rs.Set(name, func(call goja.FunctionCall) goja.Value {
			first := s.toBSON(call.Argument(0))
			if first == nil && name == "stepDown" {
				first = int32(60)
			}
			cmd := bson.D{{Key: spec[0], Value: first}}
			if len(spec) > 2 && !goja.IsUndefined(call.Argument(1)) {
				cmd = append(cmd, bson.E{Key: "secondaryCatchUpPeriodSecs", Value: s.toBSON(call.Argument(1))})
			}
			return s.fromBSON(admin(cmd))
		})
	}

	_ = rs.Set("reconfigForPSASet", func(call goja.FunctionCall) goja.Value {
		index := call.Argument(0).ToInteger()
		config := s.requiredDocument(call.Argument(1), "replica-set config")
		members, _ := docGet(config, "members").(bson.A)
		if index < 0 || index >= int64(len(members)) {
			panic(s.vm.NewTypeError("new member index is outside config.members"))
		}
		member := asDocument(members[index])
		if numeric(docGet(member, "votes")) != 1 {
			panic(s.vm.NewTypeError("new member must have votes: 1"))
		}
		current := asDocument(docGet(admin(bson.D{{Key: "replSetGetConfig", Value: int32(1)}}), "config"))
		oldMembers, _ := docGet(current, "members").(bson.A)
		for _, item := range oldMembers {
			old := asDocument(item)
			if numeric(docGet(old, "_id")) == numeric(docGet(member, "_id")) && docGet(old, "votes") != int32(0) && docGet(old, "votes") != int64(0) {
				panic(s.vm.NewTypeError("existing member must have votes: 0 before PSA reconfiguration"))
			}
		}
		priority := docGet(member, "priority")
		if priority == nil {
			priority = int32(1)
		}
		members[index] = docSet(member, "priority", int32(0))
		config = docSet(config, "members", members)
		opts := s.document(call.Argument(2))
		first := reconfig(config, opts)
		if priority == int32(0) || priority == int64(0) || priority == float64(0) {
			return first
		}
		members[index] = docSet(asDocument(members[index]), "priority", priority)
		return reconfig(docSet(config, "members", members), opts)
	})
	for _, method := range []string{"printReplicationInfo", "printSecondaryReplicationInfo"} {
		method := method
		_ = rs.Set(method, func(call goja.FunctionCall) goja.Value { return s.activeDatabase().databaseCall(method, call) })
	}
	_ = rs.Set("secondaryOk", func(call goja.FunctionCall) goja.Value {
		return s.activeDatabase().databaseCall("setSecondaryOk", call)
	})
	s.must(s.vm.Set("rs", rs))
	s.initSharding()
}

func transactionOptions(s *mongoSession, document bson.D) *options.TransactionOptionsBuilder {
	opts := options.Transaction()
	for _, e := range document {
		switch e.Key {
		case "readConcern":
			opts.SetReadConcern(&readconcern.ReadConcern{Level: fmt.Sprint(docGet(asDocument(e.Value), "level"))})
		case "writeConcern":
			doc := asDocument(e.Value)
			wc := &writeconcern.WriteConcern{W: docGet(doc, "w")}
			if journal, ok := docGet(doc, "j").(bool); ok {
				wc.Journal = &journal
			}
			if docGet(doc, "wtimeout") != nil {
				panic(s.vm.NewTypeError("transaction wtimeout is not supported by the Go driver"))
			}
			opts.SetWriteConcern(wc)
		case "readPreference":
			mode, err := readpref.ModeFromString(fmt.Sprint(e.Value))
			s.must(err)
			pref, err := readpref.New(mode)
			s.must(err)
			opts.SetReadPreference(pref)
		default:
			panic(s.vm.NewTypeError("unsupported transaction option " + e.Key))
		}
	}
	return opts
}

type mongoSessionState struct{ inTransaction bool }

func (s *mongoSession) newSession(handle *mongoHandle, call goja.FunctionCall) goja.Value {
	opts := options.Session()
	for _, e := range s.document(call.Argument(0)) {
		switch e.Key {
		case "causalConsistency":
			opts.SetCausalConsistency(e.Value == true)
		case "snapshot":
			opts.SetSnapshot(e.Value == true)
		case "defaultTransactionOptions":
			opts.SetDefaultTransactionOptions(transactionOptions(s, asDocument(e.Value)))
		default:
			panic(s.vm.NewTypeError("unsupported session option " + e.Key))
		}
	}
	session, err := handle.client().StartSession(opts)
	s.must(err)
	s.sessions[session] = &mongoSessionState{}
	return s.sessionObject(session)
}
func (s *mongoSession) sessionObject(session *mongo.Session) goja.Value {
	object := s.vm.NewObject()
	state := s.sessions[session]
	check := func() {
		if _, ok := s.sessions[session]; !ok {
			panic(s.vm.NewTypeError("session has ended"))
		}
	}
	_ = object.Set("getDatabase", func(call goja.FunctionCall) goja.Value {
		check()
		var owner *mongoSession
		if session.Client() != s.client {
			for _, candidate := range s.others {
				if candidate.client == session.Client() {
					owner = candidate
					break
				}
			}
		}
		return s.vm.NewDynamicObject(&mongoHandle{s: s, owner: owner, database: call.Argument(0).String(), kind: "database", session: session, props: make(map[string]goja.Value)})
	})
	_ = object.Set("getSessionId", func(goja.FunctionCall) goja.Value {
		check()
		var id bson.D
		s.must(bson.Unmarshal(session.ID(), &id))
		return s.fromBSON(id)
	})
	_ = object.Set("getClusterTime", func(goja.FunctionCall) goja.Value {
		check()
		raw := session.ClusterTime()
		if len(raw) == 0 {
			return goja.Null()
		}
		var doc bson.D
		s.must(bson.Unmarshal(raw, &doc))
		return s.fromBSON(doc)
	})
	_ = object.Set("advanceClusterTime", func(call goja.FunctionCall) goja.Value {
		check()
		raw, err := bson.Marshal(s.requiredDocument(call.Argument(0), "cluster time"))
		s.must(err)
		s.must(session.AdvanceClusterTime(raw))
		return goja.Undefined()
	})
	_ = object.Set("getOperationTime", func(goja.FunctionCall) goja.Value {
		check()
		timestamp := session.OperationTime()
		if timestamp == nil {
			return goja.Null()
		}
		return s.fromBSON(*timestamp)
	})
	_ = object.Set("advanceOperationTime", func(call goja.FunctionCall) goja.Value {
		check()
		timestamp, ok := s.toBSON(call.Argument(0)).(bson.Timestamp)
		if !ok {
			panic(s.vm.NewTypeError("operation time must be a Timestamp"))
		}
		s.must(session.AdvanceOperationTime(&timestamp))
		return goja.Undefined()
	})
	_ = object.Set("inTransaction", func(goja.FunctionCall) goja.Value { check(); return s.vm.ToValue(state.inTransaction) })
	_ = object.Set("startTransaction", func(call goja.FunctionCall) goja.Value {
		check()
		s.must(session.StartTransaction(transactionOptions(s, s.document(call.Argument(0)))))
		state.inTransaction = true
		return goja.Undefined()
	})
	_ = object.Set("commitTransaction", func(goja.FunctionCall) goja.Value {
		check()
		s.must(session.CommitTransaction(s.ctx))
		state.inTransaction = false
		return goja.Undefined()
	})
	_ = object.Set("abortTransaction", func(goja.FunctionCall) goja.Value {
		check()
		s.must(session.AbortTransaction(s.ctx))
		state.inTransaction = false
		return goja.Undefined()
	})
	_ = object.Set("endSession", func(goja.FunctionCall) goja.Value {
		check()
		session.EndSession(s.ctx)
		state.inTransaction = false
		delete(s.sessions, session)
		return goja.Undefined()
	})
	factory, err := s.vm.RunProgram(mongoTransactionFactory)
	s.must(err)
	fn, _ := goja.AssertFunction(factory)
	wrapped, err := fn(goja.Undefined(), object)
	s.must(err)
	_ = object.Set("withTransaction", wrapped)
	return object
}

// A callback returning a Promise must commit after that Promise resolves. It
// cannot be awaited while a Go driver callback is still on the JS call stack.
// The same wrapper also preserves immediate completion for synchronous calls.
var mongoTransactionFactory = goja.MustCompile("<withTransaction>", `(function(session) {
 return function(callback, options) {
  if (typeof callback !== "function") throw new TypeError("withTransaction requires a function");
  const started = Date.now();
  function retry(error, name) { return Date.now()-started < 120000 && error && typeof error.hasErrorLabel === "function" && error.hasErrorLabel(name); }
  function abort() { try { session.abortTransaction(); } catch (_) {} }
  function callbackFailed(error) {
   abort();
   if (retry(error, "TransientTransactionError")) { sleep(10); return attempt(); }
   throw error;
  }
  function commit(value) {
   if (typeof session.inTransaction === "function" && !session.inTransaction()) return value;
   for (;;) {
    try { session.commitTransaction(); return value; }
    catch (error) {
     if (error.code !== 50 && retry(error, "UnknownTransactionCommitResult")) { sleep(10); continue; }
     throw error;
    }
   }
  }
  function committed(value) {
   try { return commit(value); }
   catch (error) {
    if (retry(error, "TransientTransactionError")) { sleep(10); return attempt(); }
    throw error;
   }
  }
  function attempt() {
   for (;;) {
    session.startTransaction(options);
    let result;
    try { result = callback(session); }
    catch (error) {
     abort();
     if (retry(error, "TransientTransactionError")) { sleep(10); continue; }
     throw error;
    }
    if (result && typeof result.then === "function") return result.then(committed, callbackFailed);
    try { return commit(result); }
    catch (error) {
     if (retry(error, "TransientTransactionError")) { sleep(10); continue; }
     throw error;
    }
   }
  }
  return attempt();
 };
})`, false)
