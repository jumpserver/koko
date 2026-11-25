# Chen go-ora patch

Upstream: `github.com/sijms/go-ora/v2`, tag `v2.9.0`.
Repository: https://github.com/sijms/go-ora
License: MIT, preserved in `LICENSE`.

This copy includes the Go sources required to build the driver, its module
metadata and license. Upstream examples, issue reproductions, tests and binary
artifacts are excluded. The generated converter tables are required for Oracle
character set compatibility and are retained.

`value_setter.go`, `setLob`: a valid LOB locator with zero bytes represents an
empty LOB, not SQL NULL. The upstream zero-length early return incorrectly
converted `EMPTY_BLOB()` and `EMPTY_CLOB()` to NULL. Keep the existing null
locator check, normalize an empty byte result to a non-nil slice, and continue
through the normal type conversion. Chen uses `LOB FETCH=post`: upstream
inline/pre mode coerces LOB columns to LONG/LONG RAW and loses empty versus
NULL information before the normal conversion.

Regression evidence lives in Chen's opt-in `TestReadonlyDatabaseMatrix`, using
native empty/non-empty BLOB, CLOB and NCLOB expressions from a read-only Oracle
connection. No database tables or objects are created by that test.

`chen_transaction.go` exposes the driver's existing implicit auto-commit flag
through `ChenSetAutoCommit`. It changes no server transaction state. Chen calls
it under `sql.Conn.Raw` while holding its console lock to preserve an active
user transaction during grid-save and execution-plan savepoints, then restores
the previous flag. Deterministic driver regressions cover successful staging,
rollback and failed savepoint creation without committing the outer transaction.
