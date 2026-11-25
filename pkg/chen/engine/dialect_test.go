package engine

import (
	"testing"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

func TestOceanBaseTenantSQLAndTransport(t *testing.T) {
	for _, tc := range []struct {
		d           dialect.ID
		quoted      string
		placeholder string
		interrupt   bool
	}{
		{dialect.OceanBaseMySQL, "`Order`", "?", true},
		{dialect.OceanBaseOracle, `"Order"`, "?", true},
		{dialect.Oracle, `"Order"`, ":2", false},
	} {
		t.Run(string(tc.d), func(t *testing.T) {
			if got := quoteDatabaseIdentifier("Order", tc.d); got != tc.quoted {
				t.Errorf("quoted identifier = %q, want %q", got, tc.quoted)
			}
			if got := editPlaceholder(tc.d, 2); got != tc.placeholder {
				t.Errorf("placeholder = %q, want %q", got, tc.placeholder)
			}
			if got := supportsBackendInterrupt(tc.d); got != tc.interrupt {
				t.Errorf("backend interrupt = %t, want %t", got, tc.interrupt)
			}
		})
	}
}
