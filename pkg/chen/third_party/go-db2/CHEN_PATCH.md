# Chen go-db2 source pin

Upstream: https://github.com/go-db2/go-db2
Commit: 31eae67a759d02a4109525af1181ee35f0114c79 (2026-11-03).
Module version: v0.0.0-20261103115748-31eae67a759d.
License: MIT; the original LICENSE is retained.

Only runtime Go source and the module declaration are included. The native IBM
CLI dependency is completely removed. The local module replacement is required
for the following correctness fixes observed against Db2 LUW 11.5.8:

- Recognize both nullable and non-nullable LOBCDBCS wire types (0xCC/0xCD),
  and distinguish LOBCSBCS from LOBCMIXED. Missing non-nullable DBCLOB support
  previously assigned its EXTDTA payload to the following BLOB column.
- Retain non-empty external-LOB placeholders while decoding a row; zero-length
  and NULL LOBs do not consume EXTDTA objects. Resolve placeholders before
  exposing rows and fail explicitly when required external data is missing.
- Strip the EXTDTA null indicator only for nullable fields, preserving leading
  zero bytes in non-nullable binary LOBs.

The relevant DRDA field IDs, null indicators and empty-LOB handling are also
specified by Apache Derby's DRDAConstants and NetCursor implementations:
https://github.com/apache/derby/blob/trunk/java/org.apache.derby.commons/org/apache/derby/shared/common/reference/DRDAConstants.java
https://github.com/apache/derby/blob/trunk/java/org.apache.derby.client/org/apache/derby/client/net/NetCursor.java

Connection-context cancellation in this pin interrupts socket I/O and marks the
connection broken. Chen exposes connection cancellation with an unknown outcome
unless an original server diagnostic confirms cancellation. It does not claim
that the unused upstream Interrupt method acknowledges server termination.
