package sqlservice

import "github.com/jumpserver/koko/pkg/chen/dialect"

// Dialect and the product constants retain the original Go API.
// New callers can use dialect.ID and its syntax and wire predicates directly.
type Dialect = dialect.ID

const (
	MySQL           = dialect.MySQL
	MariaDB         = dialect.MariaDB
	PostgreSQL      = dialect.PostgreSQL
	SQLServer       = dialect.SQLServer
	Oracle          = dialect.Oracle
	Dameng          = dialect.Dameng
	DB2             = dialect.DB2
	ClickHouse      = dialect.ClickHouse
	OceanBaseMySQL  = dialect.OceanBaseMySQL
	OceanBaseOracle = dialect.OceanBaseOracle
)

func IsMySQLDialect(d dialect.ID) bool  { return d.UsesMySQLSyntax() }
func IsOracleDialect(d dialect.ID) bool { return d.UsesOracleSyntax() }
