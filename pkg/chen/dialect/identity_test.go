package dialect

import (
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	for _, tt := range []struct {
		protocol, tenant string
		want             ID
		errCode          string
	}{
		{" PostgreSQL ", "", PostgreSQL, ""},
		{"PG", "", PostgreSQL, ""},
		{"postgres", "", PostgreSQL, ""},
		{"pgsql", "", PostgreSQL, ""},
		{"maria", "", MariaDB, ""},
		{"mssql", "", SQLServer, ""},
		{"DM", "", Dameng, ""},
		{"oceanbase", " MySQL ", OceanBaseMySQL, ""},
		{"OCEANBASE", "oracle", OceanBaseOracle, ""},
		{"oceanbase-oracle", "oracle", OceanBaseOracle, ""},
		{"oceanbase", "", "", "oceanbase_tenant_mode_required"},
		{"oceanbase", "pg", "", "oceanbase_tenant_mode_required"},
		{"oceanbase-mysql", "oracle", "", "tenant_mode_requires_oceanbase"},
		{"mysql", "mysql", "", "tenant_mode_requires_oceanbase"},
		{"unknown", "", "", "unsupported_database_for_go_v1"},
	} {
		d, err := Parse(tt.protocol, tt.tenant)
		if d != tt.want || (err != nil) != (tt.errCode != "") || err != nil && err.Error() != tt.errCode {
			t.Errorf("Parse(%q, %q) = (%q, %v), want (%q, %q)", tt.protocol, tt.tenant, d, err, tt.want, tt.errCode)
		}
	}
}

func TestAdvertisedProtocolsAndFamilies(t *testing.T) {
	want := []string{"mysql", "mariadb", "postgresql", "sqlserver", "oracle", "dameng", "clickhouse", "oceanbase", "oceanbase-mysql", "oceanbase-oracle", "db2"}
	if got := SupportedProtocols(); !slices.Equal(got, want) {
		t.Fatalf("advertised protocols = %v, want %v", got, want)
	}
	for _, protocol := range want {
		if protocol == "oceanbase" {
			continue // This protocol requires a tenant mode.
		}
		d, err := Parse(protocol, "")
		mysqlSyntax := d == MySQL || d == MariaDB || d == OceanBaseMySQL
		oracleSyntax := d == Oracle || d == Dameng || d == OceanBaseOracle
		mysqlWire := mysqlSyntax || d == OceanBaseOracle
		if err != nil || string(d) != protocol || !d.Supported() ||
			d.UsesMySQLSyntax() != mysqlSyntax || d.UsesOracleSyntax() != oracleSyntax || d.UsesMySQLWire() != mysqlWire {
			t.Errorf("unexpected identity or syntax/wire family for %q: %q, %v", protocol, d, err)
		}
	}
}
