package nosql

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func liveMongoShell(t *testing.T) (*mongoSession, context.Context) {
	return liveMongoShellAt(t, "USQL_MONGODB_REPLICA_TEST_DSN_FILE")
}

func liveMongoShellAt(t *testing.T, variable string) (*mongoSession, context.Context) {
	t.Helper()
	u := liveURL(t, variable)
	database := fmt.Sprintf("usql_completion_%d", time.Now().UnixNano())
	u.Path = "/" + database
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	backend, err := openMongo(ctx, u, false)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	s := backend.(*mongoSession)
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		client, err := mongo.Connect(options.Client().ApplyURI(u.String()))
		if err == nil {
			_ = client.Database(database).Drop(cleanup)
			_ = client.Disconnect(cleanup)
		}
		_ = s.Close()
		cancel()
	})
	return s, ctx
}
func runMongoScript(t *testing.T, s *mongoSession, ctx context.Context, source string) {
	t.Helper()
	if err := s.Execute(ctx, io.Discard, source); err != nil {
		t.Fatalf("MongoDB script failed: %v\n%s", err, source)
	}
}

func TestLiveMongoShellJavaScriptAndCursors(t *testing.T) {
	s, ctx := liveMongoShell(t)
	runMongoScript(t, s, ctx, `var documents=[]; for (let i=0;i<55;i++){documents.push({n:i,text:"中文🙂"})}; db.items.insertMany(documents);`)
	runMongoScript(t, s, ctx, `var cursor=db.items.find({n:{$gte:0}},{_id:0}).sort({n:1}).batchSize(7); assert(cursor.hasNext()); assert.eq(0,cursor.next().n); var rest=cursor.toArray(); assert.eq(54,rest.length); assert.eq(54,rest[53].n); assert(!cursor.hasNext());`)
	runMongoScript(t, s, ctx, `assert.eq(3,db.items.find({}).sort({n:1}).skip(2).limit(3).size()); assert.eq(55,db.items.find({}).limit(3).count()); assert.eq(6,db.items.find({n:{$lt:3}}).map(x=>x.n+1).reduce((a,b)=>a+b,0)); var sum=0;db.items.find({n:{$lt:3}}).forEach(x=>sum+=x.n);assert.eq(3,sum);`)
	runMongoScript(t, s, ctx, `var sameDB=db.getSiblingDB(db.getName()); assert.eq(55,sameDB.getCollection("items").countDocuments({})); assert(db.items.find({n:1}).explain().queryPlanner);`)
	var output bytes.Buffer
	if err := s.Execute(ctx, &output, `db.items.find({},{_id:0}).sort({n:1})`); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Type it for more") || !strings.Contains(output.String(), `19`) {
		t.Fatal("first cursor page was not bounded")
	}
	output.Reset()
	if err := s.Execute(ctx, &output, "it"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `39`) {
		t.Fatal("cursor did not continue from its saved position")
	}
	output.Reset()
	if err := s.Execute(ctx, &output, "it"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Type it for more") {
		t.Fatal("exhausted cursor advertised another page")
	}
}

func TestLiveMongoShellBSONBulkAndIndexes(t *testing.T) {
	s, ctx := liveMongoShell(t)
	runMongoScript(t, s, ctx, `var payload={_id:ObjectId(),long:NumberLong("9007199254740993"),decimal:NumberDecimal("12.5"),binary:BinData(0,"AAE="),timestamp:Timestamp({t:12,i:2}),date:new Date("2020-01-02"),small:MinKey(),large:MaxKey()};db.types.insertOne(payload);var found=db.types.findOne({_id:payload._id});assert.eq("9007199254740993",found.long.toString());assert.eq("AAE=",found.binary.base64());assert.eq("12.5",found.decimal.toString());assert.eq(2020,found.date.getUTCFullYear());`)
	runMongoScript(t, s, ctx, `var result=db.types.bulkWrite([{insertOne:{document:{n:1}}},{updateOne:{filter:{n:1},update:{$set:{n:2}}}},{updateOne:{filter:{n:3},update:{$set:{present:true}},upsert:true}}]);assert.eq(1,result.insertedCount);assert.eq(1,result.upsertedCount);assert.eq(1,db.types.countDocuments({n:2}));`)
	runMongoScript(t, s, ctx, `var bulk=db.types.initializeUnorderedBulkOp();bulk.insert({n:10});bulk.find({n:10}).updateOne({$set:{changed:true}});bulk.execute();assert.eq(1,db.types.countDocuments({changed:true}));`)
	runMongoScript(t, s, ctx, `db.types.createIndex({n:1},{name:"partial_n",partialFilterExpression:{n:{$exists:true}}});db.types.createIndexes([{key:{long:1},name:"long_i",sparse:true},{key:{decimal:1},name:"decimal_i"}]);assert.eq(4,db.types.getIndexes().length);db.types.dropIndex("decimal_i");assert.eq(3,db.types.getIndexes().length);db.types.createIndex({date:1},{name:"date_i",writeConcern:{w:"majority"}},"majority");db.types.createIndexes([{a:1},{b:1}],{sparse:true,writeConcern:{w:"majority"}});assert.eq(2,db.types.getIndexes().filter(i=>i.sparse&&(i.name==="a_1"||i.name==="b_1")).length);`)
}

func TestLiveMongoShellTransactions(t *testing.T) {
	s, ctx := liveMongoShell(t)
	runMongoScript(t, s, ctx, `db.createCollection("transactions");var session=db.getMongo().startSession({defaultTransactionOptions:{writeConcern:{w:"majority"}}});var tdb=session.getDatabase(db.getName());session.startTransaction();tdb.transactions.insertOne({n:1});session.abortTransaction();assert.eq(0,db.transactions.countDocuments({}));session.startTransaction();tdb.transactions.insertOne({n:2});session.commitTransaction();assert.eq(1,db.transactions.countDocuments({n:2}));`)
	runMongoScript(t, s, ctx, `session.withTransaction(()=>tdb.transactions.insertOne({n:3}));assert.eq(1,db.transactions.countDocuments({n:3}));var failed=false;try{session.withTransaction(()=>{tdb.transactions.insertOne({n:4});throw new Error("rollback")})}catch(e){failed=true};assert(failed);assert.eq(0,db.transactions.countDocuments({n:4}));`)
	runMongoScript(t, s, ctx, `(async()=>{await session.withTransaction(async()=>{await tdb.transactions.insertOne({n:5})});assert.eq(1,db.transactions.countDocuments({n:5}));})()`)
	runMongoScript(t, s, ctx, `session.withTransaction(()=>{tdb.transactions.insertOne({n:6});assert(session.inTransaction());session.abortTransaction()});assert(!session.inTransaction());assert.eq(0,db.transactions.countDocuments({n:6}));assert(session.getClusterTime());assert(session.getOperationTime());session.advanceOperationTime(session.getOperationTime());session.advanceClusterTime(session.getClusterTime());`)
	runMongoScript(t, s, ctx, `session.endSession();var ended=false;try{tdb.transactions.countDocuments({})}catch(e){ended=true};assert(ended);`)
}

func TestLiveMongoShellUsersRolesAndReplicaSet(t *testing.T) {
	s, ctx := liveMongoShell(t)
	runMongoScript(t, s, ctx, `db.createRole({role:"local_reader",privileges:[{resource:{db:db.getName(),collection:""},actions:["find"]}],roles:[]});db.createUser({user:"local_user",pwd:"fixture-password",roles:[{role:"local_reader",db:db.getName()}]});assert.eq("local_user",db.getUser("local_user").user);assert.eq("local_reader",db.getRole("local_reader").role);db.grantRolesToUser("local_user",[{role:"readWrite",db:db.getName()}]);db.revokeRolesFromUser("local_user",[{role:"readWrite",db:db.getName()}]);db.changeUserPassword("local_user","new-fixture-password");db.dropUser("local_user");db.dropRole("local_reader");assert.eq(null,db.getUser("local_user"));`)
	runMongoScript(t, s, ctx, `assert.eq("usql_completion_rs",rs.status().set);assert.eq("usql_completion_rs",rs.conf()._id);assert(db.hello().isWritablePrimary);`)
}

func TestLiveMongoShellChangeStream(t *testing.T) {
	s, ctx := liveMongoShell(t)
	runMongoScript(t, s, ctx, `db.createCollection("events");var changes=db.events.watch([], {maxAwaitTimeMS:50});assert.eq(null,changes.tryNext());`)
	if _, err := s.client.Database(s.database).Collection("events").InsertOne(ctx, bson.D{{Key: "event", Value: "probe"}}); err != nil {
		t.Fatal(err)
	}
	runMongoScript(t, s, ctx, `var event=changes.next();assert.eq("insert",event.operationType);assert.eq("probe",event.fullDocument.event);assert(changes.getResumeToken());changes.close();assert(changes.isClosed());`)
}

func TestLiveMongoShellWriteValidation(t *testing.T) {
	s, ctx := liveMongoShell(t)
	runMongoScript(t, s, ctx, `db.safety.insertOne({_id:1,n:1});`)
	for _, source := range []string{`db.safety.deleteMany()`, `db.safety.deleteOne(null)`, `db.safety.updateMany(undefined,{$set:{n:2}})`, `db.safety.replaceOne({},{$set:{n:2}})`, `db.safety.findOneAndDelete()`, `db.safety.findOneAndUpdate({},null)`, `db.safety.bulkWrite([{deleteMany:{}}])`, `db.safety.insertOne({_id:2},{insert:"other"})`, `db.safety.find({n:{$invalid:1}})`, `db.safety.dropIndex()`, `db.getMongo().setReadConcern({})`, `db.safety.findOneAndReplace({},{$set:{n:2}})`, `db.safety.findOneAndUpdate({},{n:2})`} {
		if err := s.Execute(ctx, io.Discard, source); err == nil {
			t.Fatalf("missing validation: %s", source)
		}
	}
	runMongoScript(t, s, ctx, `assert.eq(1,db.safety.countDocuments({n:1}));var duplicate=false;try{db.safety.insertOne({_id:1})}catch(e){duplicate=e.code===11000;assert(typeof e.hasErrorLabel==="function")};assert(duplicate);`)
	runMongoScript(t, s, ctx, `var b=db.safety.initializeOrderedBulkOp();var finder=b.find({_id:1});finder.updateOne({$set:{n:2}});b.execute();var repeat=false;try{finder.remove()}catch(e){repeat=true};assert(repeat);assert.eq(1,db.safety.countDocuments({n:2}));`)
	runMongoScript(t, s, ctx, `assert(db.safety.insertMany([{n:3},{n:4}],{ordered:false}).acknowledged);db.nested.collection.insertOne({n:1});assert.eq(1,db.getCollection("nested.collection").countDocuments({}));assert.commandWorked(db.safety.runCommand("collStats"));assert.eq(2,db.safety.remove({n:{$gte:3}},{justOne:false}).deletedCount);db.safety.insertMany([{n:7},{n:7}]);assert.eq(1,db.safety.remove({n:7},true).deletedCount);assert.eq(1,db.safety.remove({n:7},{justOne:true}).deletedCount);`)
	runMongoScript(t, s, ctx, `assert(!db.safety.insertOne({n:5},{writeConcern:{w:0}}).acknowledged);assert(!db.safety.updateOne({n:5},{$set:{n:6}},{writeConcern:{w:0}}).acknowledged);assert(!db.safety.deleteOne({n:6},{writeConcern:{w:0}}).acknowledged);`)
}

func TestLiveMongoShellConnectionsAndHelpers(t *testing.T) {
	s, ctx := liveMongoShell(t)
	if err := s.vm.Set("fixtureURI", s.uri); err != nil {
		t.Fatal(err)
	}
	runMongoScript(t, s, ctx, `var additional=new Mongo(fixtureURI);var adb=additional.getDB(db.getName());adb.options.insertOne({n:1});assert.eq(1,db.options.countDocuments({}));var connected=connect(fixtureURI);assert.eq(db.getName(),connected.getName());assert.commandWorked(db.runCommand("ping"));`)
	runMongoScript(t, s, ctx, `var oldCursor=db.options.find({});db.getMongo().setReadPref("secondaryPreferred");assert(oldCursor.isClosed());assert.eq("secondaryPreferred",db.getMongo().getReadPrefMode());db.options.insertOne({n:2});assert.eq(2,db.options.countDocuments({}));db.getMongo().setReadConcern("majority");assert.eq("majority",db.getMongo().getReadConcern().level);db.getMongo().setWriteConcern({w:"majority"});assert.eq("majority",db.getMongo().getWriteConcern().w);db.options.updateOne({n:2},{$set:{updated:true}});assert.eq(1,db.options.countDocuments({updated:true}));`)
	runMongoScript(t, s, ctx, `db.getMongo().setReadConcern("local");assert.commandWorked(db.stats());assert(db.serverBits()>=32);assert(db.serverCmdLineOpts());assert(db.getProfilingStatus());db.setProfilingLevel(0);assert(db.getReplicationInfo().logSizeMB>0);db.options.createIndex({n:1},{name:"n_index"});db.options.hideIndex("n_index");assert(db.options.getIndexes().some(i=>i.name==="n_index"&&i.hidden));db.options.unhideIndex("n_index");assert(Array.isArray(db.options.getPlanCache().list()));db.options.getPlanCache().clearPlansByQuery({n:1});`)
}

func TestLiveMongoShellSharding(t *testing.T) {
	s, ctx := liveMongoShellAt(t, "USQL_MONGODB_SHARDED_TEST_DSN_FILE")
	runMongoScript(t, s, ctx, `var shards=sh.listShards().shards;assert(shards.length>=2);var ns=db.getName()+".items";sh.enableSharding(db.getName(),shards[0]._id);sh.shardCollection(ns,{n:1});db.items.insertMany(Array.from({length:20},(_,n)=>({n:n})));sh.disableBalancing(ns);sh.splitAt(ns,{n:10});sh.moveChunk(ns,{n:0},shards[1]._id);assert.eq(20,db.items.countDocuments({}));`)
	runMongoScript(t, s, ctx, `var zone=db.getName()+"_zone";sh.addShardToZone(shards[1]._id,zone);sh.updateZoneKeyRange(ns,{n:MinKey()},{n:10},zone);assert(sh.status().tags.some(t=>t.tag===zone));sh.removeRangeFromZone(ns,{n:MinKey()},{n:10});sh.removeShardFromZone(shards[1]._id,zone);sh.enableBalancing(ns);sh.disableMigrations(ns);sh.enableMigrations(ns);assert.commandWorked(sh.isBalancerRunning());assert(Array.isArray(sh.status(true).chunks));assert(Array.isArray(sh.getShardedDataDistribution().toArray()));assert(Array.isArray(db.checkMetadataConsistency().toArray()));`)
}

func TestLiveMongoShellAuthentication(t *testing.T) {
	s, ctx := liveMongoShellAt(t, "USQL_MONGODB_AUTH_TEST_DSN_FILE")
	runMongoScript(t, s, ctx, `db.createUser({user:"shell_user",pwd:"disposable-test-password",roles:[{role:"readWrite",db:db.getName()}]});db.auth("shell_user","disposable-test-password");db.privateData.insertOne({n:1});var rejected=false;try{db.auth("shell_user","incorrect-test-password")}catch(e){rejected=true};assert(rejected);assert.eq(1,db.privateData.countDocuments({}));db.logout();var unauthorized=false;try{db.privateData.countDocuments({})}catch(e){unauthorized=e.code===13};assert(unauthorized);`)
}
