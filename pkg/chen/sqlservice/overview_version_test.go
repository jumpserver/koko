package sqlservice

import "testing"

func TestOverviewOBStorageVersion(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    bool
	}{
		{"OceanBase_CE 4.1.9", false},
		{"OceanBase_CE 4.2.3", false},
		{"5.7.25-OceanBase_CE-v4.2.4.1", true},
		{"OceanBase_CE 4.3.4", false},
		{"OceanBase_CE 4.3.5", true},
		{"OceanBase_CE 4.4.0", true},
		{"OceanBase_CE 4.5.0", true},
		{"OceanBase_CE 5.0.0", false},
		{"4.3.5", false},
		{"OceanBase_CE 4.3", false},
	} {
		if got := overviewOBStorageVersion(tc.version); got != tc.want {
			t.Errorf("overviewOBStorageVersion(%q) = %t, want %t", tc.version, got, tc.want)
		}
	}
}
