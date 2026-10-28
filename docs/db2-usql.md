# Db2 Web CLI

KoKo uses the published usql v0.2.7 binary for Db2 LUW connections. It embeds a
community pure Go DRDA driver, so no IBM CLI installation or libdb2.so is needed.
Core must also advertise Db2 as a KoKo Web CLI protocol; its existing Chen Web GUI
connection remains available.

Configure the asset's Db2 service address, actual TCP port, default database name
and managed account. A database name is required for Db2; an empty name fails
before launching usql. Existing assets and other protocols are not migrated.
The asset's configured port is used as-is (the QA instance uses 50000).

The connection uses the existing authorization, gateway tunnel, password input,
session recording, command audit and result masking paths. KoKo passes URL-encoded
credentials directly to usql with `--no-password`; it does not start an extra
password prompt. Multiline pasted SQL uses the same audit boundary as the other
SQL protocols.

When `use_ssl` is enabled, KoKo forwards the configured CA and optional client
certificate/key. The certificate hostname remains the original asset address
even if a gateway tunnel changes the connection endpoint to localhost.
The usql Db2 driver always verifies server certificates; allowing invalid
certificates is unsupported. Real-server TLS/mTLS was not validated by the
plain-TCP QA test.

`Dockerfile-base`, the development installer and `Dockerfile` all select v0.2.7.
`Dockerfile` also downloads and verifies the selected release archive before
replacing the binary inherited from its pinned base image, so an older base
image cannot silently retain v0.2.6.

The essential protocol/DSN/audit tests run with `go test ./pkg/srvconn ./pkg/proxy`.
For the opt-in read-only KoKo PTY integration test, put a plain Db2 URL with an
explicit port in a private file and run with the published binary on PATH:

```sh
USQL_BIN_DIR="$(./utils/setup-usql.sh)"
PATH="$USQL_BIN_DIR:$PATH" KOKO_DB2_TEST_DSN_FILE=/path/to/private-dsn \
  GOMAXPROCS=2 go test -p 2 ./pkg/proxy -run '^TestLiveDB2USQL$' -v
```

This adds interactive Web CLI support, not a detached database executor or
Magnus native-client proxy. Existing enterprise license checks remain in place.
Compatibility evidence is limited to Db2 LUW 11.5.7.0; it does not establish
support for Db2 for z/OS, IBM i or every authentication mode.
