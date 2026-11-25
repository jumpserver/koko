package sqlanalysis

import "github.com/jumpserver/koko/pkg/chen/dialect"

// Dialect and the product constants retain the original Go API for Profile.
type Dialect = dialect.ID

const (
	Oracle          = dialect.Oracle
	SQLServer       = dialect.SQLServer
	Dameng          = dialect.Dameng
	OceanBaseOracle = dialect.OceanBaseOracle
)
