package dialect

import (
	"math"
	"testing"
)

func TestMySQLCompatibilityNumber(t *testing.T) {
	for _, tc := range []struct {
		reported string
		want     int
	}{
		{"8.0.31", 80031},
		{"8.2", 80200},
		{"5.5.5-10.11.6-MariaDB", 50505},
		{"5.7.25-OceanBase_CE-v4.3.5.2", 50725},
		{"OceanBase v4.3.5", 0},
	} {
		if got := MySQLCompatibilityNumber(tc.reported); got != tc.want {
			t.Errorf("%q: got %d, want %d", tc.reported, got, tc.want)
		}
	}
}

func TestNumericVersion(t *testing.T) {
	for _, tc := range []struct {
		banner string
		want   Version
		suffix string
		ok     bool
	}{
		{"21.6.1", Version{21, 6, 1}, "", true},
		{"26.1.1.4 (official build)", Version{26, 1, 1}, ".4 (official build)", true},
		{"22.1.99999999999999999999999", Version{22, 1, math.MaxInt}, "", true},
		{" 21.6.1", Version{}, "", false},
		{"22.1", Version{}, "", false},
	} {
		if got, suffix, ok := ParseNumericVersion(tc.banner); got != tc.want || suffix != tc.suffix || ok != tc.ok {
			t.Errorf("%q: got %v, %q, %t", tc.banner, got, suffix, ok)
		}
	}
}

func TestMariaDBVersion(t *testing.T) {
	for _, tc := range []struct {
		banner string
		want   Version
		suffix string
		ok     bool
	}{
		{"10.11.2-MariaDB", Version{10, 11, 2}, "-MariaDB", true},
		{"5.5.5-11.8.1extra-mariadb", Version{11, 8, 1}, "extra-mariadb", true},
		{"5.5.5-11.8-MariaDB", Version{}, "", false},
		{"8.0.37", Version{}, "", false},
	} {
		if got, suffix, ok := ParseMariaDBVersion(tc.banner); got != tc.want || suffix != tc.suffix || ok != tc.ok {
			t.Errorf("%q: got %v, %q, %t", tc.banner, got, suffix, ok)
		}
	}
}

func TestOceanBaseVersionFormats(t *testing.T) {
	for _, tc := range []struct {
		banner  string
		want    Version
		direct  bool
		release bool
	}{
		{"5.7.25-OceanBase_CE-v4.3.5.2", Version{4, 3, 5}, true, true},
		{"OceanBase_CE v4.2.4", Version{4, 2, 4}, false, true},
		{"5.7.25-OceanBase-v4.3.5preview", Version{4, 3, 5}, false, true},
		{"5.7.25", Version{}, false, false},
		{"OceanBase-v4.3", Version{}, false, false},
		{"OceanBase-v99999999999999999999999.2.1", Version{}, false, false},
	} {
		if v, ok := ParseOceanBaseVersion(tc.banner); ok != tc.release || ok && v != tc.want {
			t.Errorf("product release %q: got %v, %t", tc.banner, v, ok)
		}
		if v, ok := ParseOceanBaseMySQLBanner(tc.banner); ok != tc.direct || ok && v != tc.want {
			t.Errorf("MySQL banner %q: got %v, %t", tc.banner, v, ok)
		}
	}
}

func TestSQLServerMajorAndDB2ServiceLevel(t *testing.T) {
	for _, tc := range []struct {
		version string
		major   int
		ok      bool
	}{{"13.0.1000", 13, true}, {"9.", 9, true}, {"13", 0, false}} {
		if major, ok := ParseSQLServerMajor(tc.version); ok != tc.ok || ok && major != tc.major {
			t.Errorf("SQL Server %q: got %d, %t", tc.version, major, ok)
		}
	}
	for _, tc := range []struct {
		level string
		want  Version
		ok    bool
	}{
		{" db2 v9.7.1 ", Version{Major: 9, Minor: 7}, true},
		{"11.5suffix", Version{Major: 11, Minor: 5}, true},
		{"DB2 V11", Version{}, false},
	} {
		if v, ok := ParseDB2ServiceLevel(tc.level); ok != tc.ok || ok && v != tc.want {
			t.Errorf("Db2 %q: got %v, %t", tc.level, v, ok)
		}
	}
}
