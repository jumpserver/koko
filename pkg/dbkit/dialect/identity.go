// Package dialect identifies the database products used by the database access adapters.
package dialect

// ID identifies a SQL dialect, including the tenant mode for OceanBase.
type ID string

const (
	MySQL           ID = "mysql"
	MariaDB         ID = "mariadb"
	PostgreSQL      ID = "postgresql"
	SQLServer       ID = "sqlserver"
	Oracle          ID = "oracle"
	Dameng          ID = "dameng"
	ClickHouse      ID = "clickhouse"
	OceanBaseMySQL  ID = "oceanbase-mysql"
	OceanBaseOracle ID = "oceanbase-oracle"
	DB2             ID = "db2"
)

// Supported means a known identity, not that a particular entry point supports it.
func (d ID) Supported() bool {
	switch d {
	case MySQL, MariaDB, PostgreSQL, SQLServer, Oracle, Dameng, ClickHouse, OceanBaseMySQL, OceanBaseOracle, DB2:
		return true
	}
	return false
}

// UsesMySQLSyntax reports whether the dialect uses MySQL syntax.
func (d ID) UsesMySQLSyntax() bool {
	return d == MySQL || d == MariaDB || d == OceanBaseMySQL
}

// UsesOracleSyntax reports whether the dialect uses Oracle syntax.
func (d ID) UsesOracleSyntax() bool {
	return d == Oracle || d == Dameng || d == OceanBaseOracle
}

// UsesMySQLWire includes the Oracle SQL mode of OceanBase.
func (d ID) UsesMySQLWire() bool {
	return d.UsesMySQLSyntax() || d == OceanBaseOracle
}
