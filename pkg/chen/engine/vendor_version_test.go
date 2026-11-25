package engine

import (
	"testing"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

func TestBrowseVendorVersionSupported(t *testing.T) {
	for _, tc := range []struct {
		d       dialect.ID
		product string
		source  sqlservice.MySQLVersionSource
		want    bool
	}{
		{dialect.MariaDB, "5.5.5-10.10.1-MariaDB", sqlservice.MySQLVersionFunction, true},
		{dialect.MariaDB, "11.8.1MariaDB", sqlservice.MySQLVersionFunction, false},
		{dialect.MariaDB, "0.1.2-MariaDB", sqlservice.MySQLVersionFunction, false},
		{dialect.MariaDB, "10.11.1", sqlservice.MySQLVersionFunction, false},
		{dialect.OceanBaseMySQL, "5.7.25-OceanBase_CE-v4.3.5.1", sqlservice.MySQLVersionFunction, true},
		{dialect.OceanBaseMySQL, "OceanBase_CE 4.3.5", sqlservice.MySQLVersionFunction, false},
		{dialect.OceanBaseMySQL, "OceanBase_CE 4.3.5", sqlservice.MySQLVersionComment, true},
		{dialect.OceanBaseMySQL, "5.7.25", sqlservice.MySQLVersionFunction, false},
		{dialect.OceanBaseOracle, "OceanBase_CE 4.3.5", sqlservice.MySQLVersionComment, false},
	} {
		version := sqlservice.MySQLServerVersion{Product: tc.product, Source: tc.source}
		if got := browseVendorVersionSupported(tc.d, version); got != tc.want {
			t.Errorf("browseVendorVersionSupported(%q, %+v) = %t, want %t", tc.d, version, got, tc.want)
		}
	}
}

func TestSupportedPlanVersion(t *testing.T) {
	for _, tc := range []struct {
		d       dialect.ID
		version string
		want    bool
	}{
		{dialect.MariaDB, "10.10.1-MariaDB", false},
		{dialect.MariaDB, "5.5.5-10.11.1-MariaDB", true},
		{dialect.MariaDB, "11.0.0-MariaDB", true},
		{dialect.MariaDB, "11.8.1MariaDB", true},
		{dialect.MariaDB, "11.9.0-MariaDB", false},
		{dialect.MariaDB, "10.11.1", false},
		{dialect.OceanBaseMySQL, "OceanBase_CE 4.1.9", false},
		{dialect.OceanBaseMySQL, "OceanBase_CE 4.2.0", true},
		{dialect.OceanBaseOracle, "OceanBase_CE 4.3.0", true},
		{dialect.OceanBaseMySQL, "OceanBase_CE 4.4.0", true},
		{dialect.OceanBaseOracle, "OceanBase_CE 4.4.0", false},
		{dialect.OceanBaseMySQL, "OceanBase_CE 4.5.0", false},
		{dialect.ClickHouse, "21.5.9", false},
		{dialect.ClickHouse, "21.6.0", true},
		{dialect.ClickHouse, "26.1.0.1", true},
		{dialect.ClickHouse, "27.0.0", false},
		{dialect.ClickHouse, "26.1", false},
		{dialect.MySQL, "10.11.1-MariaDB", false},
	} {
		if got := supportedPlanVersion(tc.d, tc.version); got != tc.want {
			t.Errorf("supportedPlanVersion(%q, %q) = %t, want %t", tc.d, tc.version, got, tc.want)
		}
	}
}
