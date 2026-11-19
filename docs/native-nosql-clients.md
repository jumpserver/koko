# Native MongoDB and Redis clients in usql

MongoDB and Redis terminal sessions start `usql` and use its native session
modes. Passwords use the existing PTY password exchange and are excluded from
process arguments. The password reader does not echo characters, so KoKo
preserves the first usql prompt instead of draining legacy password-mask bytes.
Session history is disabled in these child processes; KoKo retains its existing
command control and recording paths. MongoDB HTTP gateways, connection options,
authentication source and TLS parameters are retained. Redis supports its
database, ACL username, TLS files and Cluster routing flag through the usql URL.

The usql implementation uses gomongo, Go drivers and an embedded pure-Go Goja
JavaScript engine. MongoDB includes scripts, BSON/EJSON, streaming cursors, bulk
writes, transactions, change streams, user/role and replica-set/sharding helpers.
Node.js modules and client-side field encryption are not included; this is not
complete mongosh compatibility. Redis includes persistent SELECT/WATCH/transactions,
binary arguments, subscriptions, MONITOR and Cluster routing. Stateful Cluster
commands require a direct node connection. See the matching usql README for
the full supported scope.

## Release order

`Dockerfile-base` and the development installer select usql v0.2.8. Run the
existing base image workflow for the integration branch first. The workflow
publishes the new base image and updates the base tag in `Dockerfile`. Both
Dockerfiles require `usql --drivers` to advertise `mongodb` and `redis`.

Build the final KoKo image with that updated tag. The base tool bundle drops
mongosh and its crypt library, and the runtime image drops redis-tools.

## Verification

`pkg/srvconn/conn_nosql_test.go` checks credential-free command arguments,
IPv6 endpoints, TLS/proxy/Cluster options, and the first interactive prompt.
The opt-in `TestLiveNativeNoSQLPTY` uses `USQL_NOSQL_TEST_BINARY` and private
`USQL_MONGODB_TEST_DSN_FILE` / `USQL_REDIS_TEST_DSN_FILE` test URLs to exercise
the actual usql subprocess through KoKo's password exchange. This is a local
connection test, not a full Core/Web-terminal audit or a deployed image test.
