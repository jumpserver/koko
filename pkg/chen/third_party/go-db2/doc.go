// Package db2 provides a pure Go driver for IBM Db2 databases implementing the database/sql/driver interface.
//
// The driver communicates directly with IBM Db2 database servers over TCP/IP using the Distributed
// Relational Database Architecture (DRDA) and Distributed Data Management (DDM) wire protocol.
// It requires zero C dependencies (CGO_ENABLED=0) and does not depend on IBM CLI/ODBC drivers (clidriver).
//
// # Connection String Format (DSN)
//
// Connections are configured using URL-style connection strings:
//
//	db2://username:password@host:port/database?param1=value1&param2=value2
//
// # Key Parameters
//
//   - ssl: Enable TLS/SSL transport encryption (true/false, default: true).
//   - ssl_insecure: Skip TLS server certificate verification (default: false).
//   - allow_plaintext_auth: Allow unencrypted authentication fallback over plaintext TCP (default: false).
//   - security: Authentication mechanism (e.g. 9 for Diffie-Hellman encrypted password, 7/11 for Kerberos).
//   - client_applname, client_wrkstnname, client_acctg: Client accounting and workload attribution registers.
//
// # Basic Usage
//
//	import (
//	    "database/sql"
//	    _ "github.com/go-db2/go-db2"
//	)
//
//	func main() {
//	    db, err := sql.Open("db2", "db2://usr:pwd@localhost:50000/MYDB?ssl=false&allow_plaintext_auth=true")
//	    if err != nil {
//	        log.Fatal(err)
//	    }
//	    defer db.Close()
//
//	    var val int
//	    if err := db.QueryRow("SELECT 1 FROM SYSIBM.SYSDUMMY1").Scan(&val); err != nil {
//	        log.Fatal(err)
//	    }
//	}
//
// # Error Inspection
//
// Structured database errors can be inspected via errors.As and *db2.Error:
//
//	var dbErr *db2.Error
//	if errors.As(err, &dbErr) {
//	    fmt.Printf("SQLCODE: %d, SQLSTATE: %s\n", dbErr.SQLCode, dbErr.SQLState)
//	}
package db2
