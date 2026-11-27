package nosql

import (
	"github.com/dop251/goja"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func (s *mongoSession) mongosDatabase() *mongoHandle {
	h := s.activeDatabase()
	if docGet(h.command("admin", bson.D{{Key: "hello", Value: int32(1)}}), "msg") != "isdbgrid" {
		panic(s.vm.NewTypeError("sharding helpers require a mongos connection"))
	}
	return h
}

func (s *mongoSession) initSharding() {
	sh := s.vm.NewObject()
	admin := func(cmd bson.D) goja.Value { return s.fromBSON(s.mongosDatabase().command("admin", cmd)) }
	for name, spec := range map[string][]string{
		"addShard": {"addShard"}, "removeShard": {"removeShard"}, "enableSharding": {"enableSharding", "primaryShard"},
		"moveChunk": {"moveChunk", "find", "to"}, "moveRange": {"moveRange", "toShard", "min", "max"},
		"splitAt": {"split", "middle"}, "splitFind": {"split", "find"}, "movePrimary": {"movePrimary", "to"},
		"refineCollectionShardKey": {"refineCollectionShardKey", "key"}, "moveCollection": {"moveCollection", "toShard"},
		"unshardCollection": {"unshardCollection", "toShard"}, "abortMoveCollection": {"abortMoveCollection"}, "abortUnshardCollection": {"abortUnshardCollection"},
		"commitReshardCollection": {"commitReshardCollection"}, "abortReshardCollection": {"abortReshardCollection"},
		"balancerCollectionStatus": {"balancerCollectionStatus"}, "addShardToZone": {"addShardToZone", "zone"}, "removeShardFromZone": {"removeShardFromZone", "zone"},
		"updateZoneKeyRange": {"updateZoneKeyRange", "min", "max", "zone"},
	} {
		name, spec := name, spec
		_ = sh.Set(name, func(call goja.FunctionCall) goja.Value {
			if goja.IsUndefined(call.Argument(0)) {
				panic(s.vm.NewTypeError(name + " requires an argument"))
			}
			cmd := bson.D{{Key: spec[0], Value: s.toBSON(call.Argument(0))}}
			for i, key := range spec[1:] {
				if !goja.IsUndefined(call.Argument(i + 1)) {
					cmd = append(cmd, bson.E{Key: key, Value: s.toBSON(call.Argument(i + 1))})
				}
			}
			return admin(cmd)
		})
	}
	for alias, target := range map[string]string{"addShardTag": "addShardToZone", "removeShardTag": "removeShardFromZone", "addTagRange": "updateZoneKeyRange"} {
		_ = sh.Set(alias, sh.Get(target))
	}
	removeZone := func(call goja.FunctionCall) goja.Value {
		return admin(bson.D{{Key: "updateZoneKeyRange", Value: call.Argument(0).String()}, {Key: "min", Value: s.requiredDocument(call.Argument(1), "zone minimum")}, {Key: "max", Value: s.requiredDocument(call.Argument(2), "zone maximum")}, {Key: "zone", Value: nil}})
	}
	_ = sh.Set("removeRangeFromZone", removeZone)
	_ = sh.Set("removeTagRange", removeZone)
	for _, method := range []string{"shardCollection", "reshardCollection"} {
		method := method
		_ = sh.Set(method, func(call goja.FunctionCall) goja.Value {
			cmd := bson.D{{Key: method, Value: call.Argument(0).String()}, {Key: "key", Value: s.requiredDocument(call.Argument(1), "shard key")}}
			optionIndex := 2
			if method == "shardCollection" {
				if _, ok := call.Argument(2).Export().(bool); ok {
					cmd = append(cmd, bson.E{Key: "unique", Value: call.Argument(2).ToBoolean()})
					optionIndex = 3
				}
			}
			return admin(append(cmd, s.document(call.Argument(optionIndex))...))
		})
	}
	for _, method := range []string{"enableMigrations", "disableMigrations"} {
		method := method
		_ = sh.Set(method, func(call goja.FunctionCall) goja.Value {
			return admin(bson.D{{Key: "setAllowMigrations", Value: call.Argument(0).String()}, {Key: "allowMigrations", Value: method == "enableMigrations"}})
		})
	}
	configUpdate := func(collection, id, field string, value any, unset, upsert bool) goja.Value {
		h := s.mongosDatabase()
		config := &mongoHandle{s: s, owner: h.connection(), database: "config", collection: collection, kind: "collection"}
		op := "$set"
		if unset {
			op = "$unset"
		}
		return config.collectionCall("updateOne", goja.FunctionCall{Arguments: []goja.Value{
			s.fromBSON(mapDocument("_id", id)), s.fromBSON(mapDocument(op, mapDocument(field, value))),
			s.fromBSON(bson.D{{Key: "upsert", Value: upsert}, {Key: "writeConcern", Value: bson.D{{Key: "w", Value: "majority"}, {Key: "wtimeout", Value: int32(60000)}}}}),
		}})
	}
	for _, method := range []string{"enableBalancing", "disableBalancing", "enableAutoMerger", "disableAutoMerger"} {
		method := method
		_ = sh.Set(method, func(call goja.FunctionCall) goja.Value {
			field, value, unset := "noBalance", any(method == "disableBalancing"), false
			if method == "enableAutoMerger" || method == "disableAutoMerger" {
				field, value, unset = "enableAutoMerge", false, method == "enableAutoMerger"
			}
			return configUpdate("collections", call.Argument(0).String(), field, value, unset, false)
		})
	}
	for _, method := range []string{"startAutoMerger", "stopAutoMerger", "enableAutoSplit", "disableAutoSplit"} {
		method := method
		_ = sh.Set(method, func(goja.FunctionCall) goja.Value {
			id := "automerge"
			if method == "enableAutoSplit" || method == "disableAutoSplit" {
				id = "autosplit"
				_, err := s.out.Write([]byte("Automatic chunk splitting has no effect on MongoDB 6.0.3 and later.\n"))
				s.must(err)
			}
			return configUpdate("settings", id, "enabled", method == "startAutoMerger" || method == "enableAutoSplit", false, true)
		})
	}
	_ = sh.Set("isAutoMergerEnabled", func(goja.FunctionCall) goja.Value {
		h := s.mongosDatabase()
		cursor := (&mongoHandle{s: s, owner: h.connection(), database: "config", collection: "settings", kind: "collection"}).cursor(bson.D{{Key: "find", Value: "settings"}, {Key: "filter", Value: mapDocument("_id", "automerge")}, {Key: "limit", Value: int32(1)}})
		defer cursor.close(h.context())
		return s.vm.ToValue(!cursor.hasNext() || docGet(cursor.next(), "enabled") == true)
	})
	for _, method := range []string{"startBalancer", "stopBalancer", "setBalancerState"} {
		method := method
		_ = sh.Set(method, func(call goja.FunctionCall) goja.Value {
			command, timeout := "balancerStart", int64(60000)
			if method == "stopBalancer" || method == "setBalancerState" && !call.Argument(0).ToBoolean() {
				command = "balancerStop"
			}
			if method != "setBalancerState" && !goja.IsUndefined(call.Argument(0)) {
				timeout = call.Argument(0).ToInteger()
			}
			return admin(bson.D{{Key: command, Value: int32(1)}, {Key: "maxTimeMS", Value: timeout}})
		})
	}
	_ = sh.Set("getBalancerState", func(goja.FunctionCall) goja.Value {
		result := s.mongosDatabase().command("admin", bson.D{{Key: "balancerStatus", Value: int32(1)}})
		return s.vm.ToValue(docGet(result, "mode") != "off")
	})
	_ = sh.Set("isBalancerRunning", func(goja.FunctionCall) goja.Value { return admin(bson.D{{Key: "balancerStatus", Value: int32(1)}}) })
	_ = sh.Set("listShards", func(goja.FunctionCall) goja.Value { return admin(bson.D{{Key: "listShards", Value: int32(1)}}) })
	_ = sh.Set("checkMetadataConsistency", func(call goja.FunctionCall) goja.Value {
		h := *s.mongosDatabase()
		h.database = "admin"
		return s.vm.NewDynamicObject(h.cursor(append(bson.D{{Key: "checkMetadataConsistency", Value: int32(1)}}, s.document(call.Argument(0))...)))
	})
	_ = sh.Set("getShardedDataDistribution", func(call goja.FunctionCall) goja.Value {
		h := *s.mongosDatabase()
		h.database = "admin"
		h.collection = ""
		return s.vm.NewDynamicObject(h.cursor(bson.D{{Key: "aggregate", Value: int32(1)}, {Key: "pipeline", Value: bson.A{mapDocument("$shardedDataDistribution", s.document(call.Argument(0)))}}, {Key: "cursor", Value: bson.D{}}}))
	})
	_ = sh.Set("status", func(call goja.FunctionCall) goja.Value {
		h := s.mongosDatabase()
		status := h.command("admin", bson.D{{Key: "listShards", Value: int32(1)}})
		for _, collection := range []string{"version", "settings", "databases", "collections", "tags"} {
			config := &mongoHandle{s: s, owner: h.connection(), database: "config", collection: collection, kind: "collection"}
			cursor := config.cursor(bson.D{{Key: "find", Value: collection}})
			value := cursor.array()
			status = append(status, bson.E{Key: collection, Value: s.toBSON(value)})
		}
		if call.Argument(0).ToBoolean() {
			config := &mongoHandle{s: s, owner: h.connection(), database: "config", collection: "chunks", kind: "collection"}
			status = append(status, bson.E{Key: "chunks", Value: s.toBSON(config.cursor(bson.D{{Key: "find", Value: "chunks"}}).array())})
		}
		return s.fromBSON(status)
	})
	s.must(s.vm.Set("sh", sh))
}
