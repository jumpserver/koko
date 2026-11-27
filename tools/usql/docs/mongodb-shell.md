# MongoDB shell in usql

The MongoDB mode extends gomongo rather than rewriting the MongoDB wire
protocol. The official MongoDB Go driver handles connections and commands;
Goja executes JavaScript inside the statically built Go executable. The pinned
gomongo executor remains the fallback for ordinary collection helpers.
No Node.js installation, mongosh process or native encryption library is used.

## Supported interfaces

| Area | Interfaces |
| --- | --- |
| JavaScript | Persistent variables, functions, loops, arrays, objects, regular expressions, template strings, BigInt within int64 range, Promise/async functions supported by the pinned Goja version |
| Shell | `db`, `use`, `show dbs/collections/users/roles/profile`, `print`, `printjson`, `console.log/error`, `assert`, `assert.eq`, `assert.commandWorked`, cancellable `sleep`, `load`, `cat`, `quit/exit` |
| BSON | `ObjectId`, `NumberLong/Long`, `NumberInt/Int32`, `NumberDecimal/Decimal128`, `Double`, `BinData`, `UUID`, `Timestamp`, `MinKey`, `MaxKey`, `ISODate`, `Date`, `BSONRegExp`, `EJSON.parse/stringify/serialize/deserialize` |
| Queries | `find`, `findOne`, aggregation pipelines, count/countDocuments/estimatedDocumentCount, distinct, explain, nested collection names |
| Cursors | Lazy server cursor, `hasNext/next/tryNext`, `toArray`, `forEach/map`, `count/size/itcount`, sort/skip/limit/batchSize/hint/collation/maxTimeMS and other find modifiers, close, `it` pagination |
| Writes | insert/update/replace/delete families, findAndModify/findOneAndUpdate/Replace/Delete, bulkWrite, ordered/unordered bulk builders, upsert, arrayFilters, collation and hints |
| Collections | Create/drop/rename collections and views, create/drop/list/hide/unhide indexes, plan cache, statistics, validation, mapReduce command forwarding |
| Database/server | runCommand/runReadCommand/adminCommand/runCommandCursor, database/server/build/host statistics, profiling, log levels, replication information, metadata consistency |
| Connections | `Mongo`, `connect`, getDB/getSiblingDB/getCollection/getMongo, auth/logout, get/set read preference, read concern and write concern |
| Sessions | startSession, getDatabase/getSessionId, start/commit/abortTransaction, withTransaction, endSession |
| Change streams | Collection/database watch, next/tryNext/hasNext/forEach, resumeAfter/startAfter/startAtOperationTime, fullDocument options, getResumeToken, close |
| Users and roles | Create/update/get/drop users and roles, password changes, grant/revoke roles and privileges |
| Replica sets | rs.status/config/conf/hello/isMaster/initiate/reconfig/reconfigForPSASet/add/addArb/remove/stepDown/freeze/syncFrom, secondaryOk and replication reporting |
| Sharding | sh.enableSharding/shardCollection/reshardCollection, refine key, add/remove/list shards, splitAt/splitFind/moveChunk/moveRange/movePrimary, zones and tag aliases, balancing/migration/auto-merger controls, collection move/unshard/abort helpers, metadata consistency and distribution, status |
| Server-dependent helpers | Atlas search/vector-search index commands, `$sql`, mapReduce and newer administration commands are forwarded to the server; availability and privileges depend on that server |

Read and write concerns, collation, partial/sparse/TTL indexes and BSON values
are preserved rather than flattened into JSON strings. Ordinary write
commands select a primary even when the connection prefers secondary reads.
Duplicate-key errors expose their code and transaction error labels to scripts.
Missing filters on destructive CRUD helpers are rejected; an explicit `{}`
still means all matching documents.

`withTransaction` supports synchronous and async callbacks. It retries the
callback for `TransientTransactionError`, and only the commit for
`UnknownTransactionCommitResult`, with a 120-second retry window bounded by
command cancellation. Callbacks can run more than once. Database handles from
`session.getDatabase(...)` carry the session; ordinary global `db` handles do
not join that transaction. Cursor creation is lazy, so consume query cursors
inside a transaction before committing it.

## Input, output and limits

Interactive input and `-c` compile complete JavaScript programs. Variables
persist between commands. Multiline functions and documents continue at the
prompt until the parser has a complete program. A standalone `await expression`
is accepted. Async functions are also supported; full mongosh source rewriting
and arbitrary top-level await declarations are not implemented.

`-f` executes the whole JavaScript file in the same session; it prints explicit
`print` calls, not the file's final expression. `load()` resolves relative
paths against the loading file. Files are limited to 4 MiB, interactive input
to 1 MiB, BSON nesting to 100 levels and explicit arrays to one million
elements. Use normal JavaScript and `db.getSiblingDB()` inside script files;
the `use`/`show` shell syntax is intended for separate interactive commands.

Normal query output consumes at most 20 documents per page and keeps the
server cursor for `it`. `toArray()`, `map()` and JSON output consume the cursor;
large results should use limits or streaming iteration. `-J` prints canonical
Extended JSON: a cursor produces one array, and a scalar result is wrapped in
an array. Standard output is structured EJSON rather than mongosh's colored
inspector. `sh.status()` returns structured configuration metadata; passing
`true` also includes chunk documents. Change-stream results are consumed
explicitly rather than waiting indefinitely at the prompt.

Ctrl-C cancels JavaScript loops and driver operations. The runtime can accept
another command after cancellation. Auth/logout and concern/preference setters
replace the underlying Go client and invalidate that connection's existing
cursors and sessions. Failed authentication preserves the old connection.

## Compatibility boundaries

This is not a complete mongosh distribution. Goja is not Node.js: `require`,
Node built-ins/packages, npm imports, Node event-loop APIs and mongosh's
extension/snippet system are unavailable. JavaScript compatibility is limited
to the pinned Goja engine, not every V8 language feature. Unhandled Promise
rejections fail the command; a pending final Promise with no event source is
an error. EJSON preserves values, but its stringify wrapper does not implement
JavaScript replacer/indentation callbacks or every BSON class method.

Client-side field encryption (CSFLE/Queryable Encryption), key-vault helpers,
automatic encryption and their native libraries are not built in. This keeps
the current client pure Go and requires a separate design if encryption is
needed. Deprecated getLastError helpers fail explicitly. Unsupported helper
names and server commands report errors; no successful placeholder is returned.

This shell does not add automatic retries to raw write-command helpers;
`retryWrites=true` is not a promise of driver CRUD retry behavior on those
helpers. Driver-backed bulk writes and change streams retain driver behavior.
Transactions have the explicit retry handling described above.

## Verification

Opt-in tests require dedicated, disposable MongoDB fixtures. They create unique
databases and delete those databases afterwards. The files contain test-only
DSNs and must be kept private:

- `USQL_MONGODB_REPLICA_TEST_DSN_FILE`: writable replica set with user/role and
  replica-set inspection privileges; tests transactions, change streams,
  BSON, indexes, cursors and connection settings.
- `USQL_MONGODB_SHARDED_TEST_DSN_FILE`: mongos with at least two preconfigured
  shards and administration privileges; tests sharding, chunk migration,
  zones, collection balancing, migrations and consistency checks.
- `USQL_MONGODB_AUTH_TEST_DSN_FILE`: authenticated MongoDB administrator;
  tests user authentication, failed login preservation and logout.

Run `GOMAXPROCS=2 go test -p 2 ./internal/nosql -run 'TestMongo|TestLiveMongoShell'`.
Race checks use the same fixtures with `-race`. Ordinary tests skip absent
fixtures. Local verification uses MongoDB 8.0.10; Atlas search, encrypted
collections, live PSA membership transitions and every server version are not
covered by that fixture suite. Existing SQL and Redis regressions remain
separate checks.

API semantics are checked against the [mongosh shell API source](https://github.com/mongodb-js/mongosh/tree/main/packages/shell-api/src)
and the [MongoDB shell methods reference](https://www.mongodb.com/docs/mongodb-shell/reference/methods/).
