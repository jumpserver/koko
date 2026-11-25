package core

import "github.com/jumpserver/koko/pkg/chen/dialect"

// CanonicalDialect keeps transport identity separate from SQL compatibility.
// OceanBase Oracle tenants still use OceanBase's MySQL-family wire protocol.
func CanonicalDialect(protocol, tenantMode string) (string, error) {
	d, err := dialect.Parse(protocol, tenantMode)
	return string(d), err
}

func IsMySQLDialect(d string) bool {
	return dialect.ID(d).UsesMySQLSyntax()
}
func IsMySQLWire(d string) bool {
	return dialect.ID(d).UsesMySQLWire()
}
