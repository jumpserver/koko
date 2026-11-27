# JumpServer usql

This WebCLI is built from source as part of KoKo. See the [KoKo build instructions](../../README.md) or run `make usql` at the repository root. WebCLI is a separate CGO-free executable, built with the root go.mod and the same patched Oracle, Db2 and Dameng drivers as WebGUI.

When updating the xo dependency family, run `CGO_ENABLED=0 go test ./tools/usql/...`. dburl v0.25.1 registers Dameng itself, which conflicts with this CLI's registration and handles reserved characters in credentials differently. Building alone does not exercise driver registration.

## MySQL and PostgreSQL TLS

MySQL `tls=custom` validates the server certificate and hostname by default.
Use `ssl-ca=/path/ca.pem` for an explicit CA bundle and paired `ssl-cert` /
`ssl-key` for mutual TLS. `ssl-server-name=db.example.com` keeps the database's
certificate identity when the socket is an SSH forward. Only explicit
`ssl-insecure=true` disables verification; it does not permit plaintext fallback.
Each connection has its own TLS configuration rather than a global named entry.

For MySQL and MariaDB `tls=custom` connections, TLS 1.2 also supports
`TLS_RSA_WITH_AES_128_GCM_SHA256` and `TLS_RSA_WITH_AES_256_GCM_SHA384` for
servers that only offer static RSA key exchange. These compatibility suites
do not provide forward secrecy; configure servers for ECDHE or TLS 1.3 when
available. Certificate and hostname verification still apply, TLS versions
below 1.2 remain disabled, and RC4, 3DES and RSA-CBC suites are not enabled.
TLS 1.3 negotiation is unaffected. This does not change other drivers or the
MySQL driver's built-in `tls=true`, `tls=skip-verify` or `tls=preferred` modes.

PostgreSQL retains its metadata, COPY and notice support. `hostaddr=127.0.0.1`
selects a tunnel's dial address while the URL hostname remains the TLS identity.
`sslmode=verify-full` verifies CA and hostname; `verify-ca` verifies the chain
without requiring a matching hostname. `require` requires encryption and also
verifies the chain when `sslrootcert` is explicitly supplied. `prefer` retries
without encryption only after an explicit server SSLRequest refusal, not after
certificate or authentication failures. Explicit CA-only modes keep SNI and
require explicitly paired client certificate/key options when mutual TLS is used.
With no explicit CA, verification uses system roots. Cancellation uses the same
configured TLS/dial route. None of these options imply client-to-Magnus TLS;
that is configured separately from the database asset's backend SSL setting.

## MongoDB and Redis

MongoDB and Redis are native session modes in the normal `CGO_ENABLED=0` usql
binary. They do not launch `mongosh`, `redis-cli`, Node.js or another process.
Use a MongoDB/Redis URL when starting usql; `--drivers` advertises `mongodb`,
`mongodb+srv`, `redis` and `rediss` as well as the existing SQL drivers.

```sh
usql 'mongodb://USER@HOST:27017/test?authSource=admin' --password
usql 'redis://USER@HOST:6379/0' --password
usql 'rediss://USER@HOST:6380/0?tls-ca=/path/ca.pem' --password
```

`--password` reads a password without echoing it or putting it in process
arguments. URLs also accept percent-encoded credentials. `--no-password`
disables automatic interactive prompting. Both modes share usql's terminal,
history, completion, `-c`, `-f`, piped input, `-o`, `-q`, `-X` and `-J` entry
points. Commands execute on Enter without an SQL semicolon. MongoDB accepts
semicolons between commands and multiline documents; Redis treats semicolons
as argument bytes. `exit`, `quit`, `\q` and EOF close the session. `help` lists
the supported mode's commands. SQL transaction and SQL table-format options
are rejected for these sessions. Cross-mode `\c` switching and data masking
are not implemented; configured masking rules are rejected explicitly.

MongoDB combines the pinned `github.com/bytebase/gomongo` helpers with the
MongoDB Go driver v2 and an embedded pure-Go Goja JavaScript engine. It supports
variables, functions, loops, BSON/EJSON, script files and `load()`, streaming
cursors with `it`, bulk writes, sessions/transactions, change streams,
users/roles, replica-set and sharding helpers. No external JavaScript runtime
is installed. Results use canonical Extended JSON to preserve BSON precision.
See [MongoDB shell compatibility](docs/mongodb-shell.md) for supported APIs,
script behavior, tested topologies and explicit compatibility limits.

SRV, replica-set and TLS options pass through to the Go driver. A
`proxy=http://HOST:PORT` (or HTTPS) query parameter tunnels every discovered
server connection through an HTTP CONNECT gateway.

Redis uses `github.com/redis/go-redis/v9`. Quoted arguments support empty
strings, Unicode, `\n`, `\r`, `\t` and binary `\xHH` escapes. A dedicated
connection preserves `SELECT`, `AUTH`, `WATCH` and `MULTI`/`EXEC`/`DISCARD`
state. `AUTH` and `SELECT` must be issued outside `MULTI`. Commands are not
automatically retried after connection errors. Ctrl-C cancels blocking commands;
the session reconnects using its selected database and credentials and discards
transaction/watch state. `SUBSCRIBE`, `PSUBSCRIBE`, `SSUBSCRIBE` and `MONITOR`
stream until Ctrl-C. MONITOR uses the explicitly connected node, or all masters
with `cluster=true` (each line identifies its source node). `HELLO`,
`RESET` and replication-stream commands are rejected because they change the
response protocol. `-J` emits JSON; the default output uses raw values.

For Cluster routing, set `cluster=true` in the Redis URL and use database 0.
The Go driver discovers nodes and follows MOVED/ASK redirects. Stateful
`AUTH`, `SELECT`, `WATCH` and transaction commands require a direct node
connection without `cluster=true`. Sentinel discovery is not implemented.
`DBSIZE`, `KEYS`, `SCAN`, and the keyspace section of `INFO` aggregate masters
only, not replicas. `\cluster info` shows per-master slots, key counts and the
total. `SCAN` returns opaque session-local cursors, valid for five minutes
(up to 1024 checkpoints); restart with `SCAN 0` after topology changes or
cursor expiry. Scans are not an atomic snapshot. `KEYS` uses bounded scans and
fails explicitly above 10000 keys or 8 MiB; use `SCAN` for larger databases.
Cluster MONITOR checks topology every five seconds and stops explicitly if a
master disconnects or topology changes; restart it instead of accepting partial
coverage. Ctrl-C closes only stream/discovery connections, not the command pool.
`rediss://` validates TLS by default and accepts `tls-ca`, paired `tls-cert`
and `tls-key`, and `tls-server-name`. `tls-insecure=true` explicitly disables
certificate verification. No TLS option is accepted on a plain `redis://` URL.

The gomongo/omni module graph also raises versions of shared SQL driver
dependencies (MySQL, SQL Server and Oracle). The existing SQL tests and Core
driver initialization checks are retained; these checks do not replace live
regression testing against every SQL engine.

Opt-in isolated integration tests create and remove their own temporary MongoDB
database and Redis keys. The two private files below contain test-only URLs:

```sh
USQL_MONGODB_TEST_DSN_FILE=/path/to/private-mongodb-dsn \
USQL_REDIS_TEST_DSN_FILE=/path/to/private-redis-dsn \
  GOMAXPROCS=2 go test -p 2 ./internal/nosql -run TestLive -v
```

`USQL_MONGODB_TLS_TEST_DSN_FILE` and `USQL_REDIS_TLS_TEST_DSN_FILE` additionally
enable TLS trust checks. The Redis TLS fixture requires a client certificate
and validates the `tls-cert` / `tls-key` pair as well as server trust.

## Db2 LUW

Db2 is compiled into the normal `CGO_ENABLED=0` binary on every supported target.
No IBM CLI, ODBC installation, native library bundle or KoKo dependency version
change is required to build this usql change.

The adapter uses the independent, MIT-licensed
[`github.com/go-db2/go-db2`](https://github.com/go-db2/go-db2) DRDA driver, pinned
to `v0.0.0-20261015010257-944faf6c05dc`. It is a community driver, not IBM's
official Go driver. This initial integration has been tested against Db2 LUW
11.5.7.0; it does not establish compatibility with Db2 for z/OS or IBM i.

```sh
CGO_ENABLED=0 go build -o build/usql .
build/usql --drivers
build/usql 'db2://USER:PERCENT_ENCODED_PASSWORD@HOST:50000/DATABASE' --no-password
```

The URL schemes are `db2` (alias `db`) and `db2s` (TLS required). Username and
password must be URL-encoded, including `@`, `:`, `/`, `?`, `#` and `%` when they
occur in a credential. The database name is required. The default TCP port is
50000, including for TLS; specify the server's actual TLS port explicitly.

Supported query options:

| Option | Meaning |
| --- | --- |
| `ssl=true` | Enable TLS 1.2 or later, validating the certificate and server name |
| `ssl-ca=/path/ca.pem` | Root CA file, with `ssl=true` |
| `ssl-cert=/path/client.pem`, `ssl-key=/path/client.key` | Paired client certificate/key files, with `ssl=true` |
| `ssl-server-name=db.example.com` | Expected TLS server name, useful with a tunnel |
| `timeout=30s` | Positive TCP dial timeout; this is not a SQL execution deadline |
| `secmec=3` or `secmec=9` | Requested DRDA authentication mechanism; the server can negotiate the mechanism |

Unsupported or duplicate query options fail explicitly. `db2s` cannot disable
TLS. There is no option to bypass certificate verification. A database name
over 18 bytes is rejected rather than silently truncated by the pinned driver.

SQL queries, positional `?` parameters and ordinary transactions use
`database/sql`. `VALUES`, `SELECT`, `WITH` and `TABLE` return tabular results.
The adapter also supplies Db2 `SYSCAT` metadata for `\dn`, `\dt`, `\dv` and
`\d`; use `S` to include system objects, for example `\dS SYSIBM.SYSDUMMY1`.
Unquoted metadata patterns are folded to uppercase; quoted names retain case.
Indexes, constraints, sequences and stored procedure metadata are not supplied
by this initial adapter. Existing `\c database` behavior remains PostgreSQL-only;
start a new usql process with another Db2 URL to change databases.

The pinned driver buffers query results in memory. Large-result export and
full stored procedure/LOB compatibility need separate validation; this change
does not claim streaming behavior or complete Db2 feature coverage.

## Validation

```sh
GOMAXPROCS=2 go test -p 2 ./...
GOMAXPROCS=2 go test -race -p 2 ./...
USQL_DB2_TEST_DSN_FILE=/path/to/private-dsn-file \
  GOMAXPROCS=2 CGO_ENABLED=0 go test -p 2 ./drivers/db2 -run TestLiveDB2 -v
```

The private file contains a Db2 URL and must remain outside this repository.
The opt-in integration test performs only read-only queries and rolls back its
read-only SQL transaction. It checks authentication, scalar values (including
Unicode and NULL), parameter binding and schema/table/column metadata. Normal
tests use local sockets for IPv4, IPv6, cancellation and TLS certificate checks.
TLS fixtures do not establish compatibility with a real TLS-enabled Db2 server.

For KoKo's Db2 connection settings and PTY integration test, see the
[Db2 Web CLI guide](../../docs/db2-usql.md).

The [Core SQL driver coverage review](docs/core-sql-driver-coverage.md) records
the nine current SQL database types, their actual driver packages, vendor versus
community provenance and the distinction between initialization and live tests.
