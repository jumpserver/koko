# Chen Dameng driver patch

Upstream: `gitee.com/chunanyong/dm`, tag `v1.8.23` (Dameng 8.1.4.200).
Source: https://gitee.com/chunanyong/dm

The complete published Go module is retained, including README, CHANGELOG,
VERSION, module metadata and original copyright headers. This upstream module
does not include a standalone LICENSE file; this copy adds no license claim.

`za.go`: `addDetail` and `throw` use value receivers
and return a new error instance. Upstream modifies package-level error templates
in place, racing across independent connections and potentially mixing private
endpoint diagnostics. Copying before setting details/stack makes those templates
immutable without serializing network operations.

`zc.go`: a connector's filter chain reads its own `logLevel` and `statEnable`
fields instead of global settings which another connector can concurrently
overwrite. Chen disables both logging and statistics; it does not configure or
start the vendor's process-global log/statistics writers.

`security/zzd.go`: initialize the fixed DH prime/generator once at package load,
instead of reassigning shared pointers on every login. Their values and the
key exchange algorithm are unchanged. Concurrent live logins exposed this race
after the filter initialization race was corrected.

Chen's `internal/engine/database_dameng.go` separately isolates the unsafe vendor
context watcher at the connector boundary, preserves statement/row metadata and
uses `RegisterDialContext` for actual per-I/O deadlines. This driver release's
`socketTimeout` only controls its default dial, not socket reads/writes. In-flight
cancellation waits for the current driver call to return safely, discards the
connection and reports unknown outcome; it does not claim server cancellation.

Run `go build ./...` within this module for build validation.
Connection deadlines and cancellation require separate live Dameng validation.
