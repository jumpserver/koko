package sessiontools

import (
	"context"
	"reflect"
	"testing"
)

func TestOceanBaseExecutorTenantModes(t *testing.T) {
	for _, test := range []struct {
		mode     string
		protocol string
	}{
		{mode: "mysql", protocol: "mysql"},
		{mode: "oracle", protocol: "oracle"},
	} {
		t.Run(test.mode, func(t *testing.T) {
			executor, err := NewMySQLExecutor(context.Background(), DatabaseConfig{
				Protocol: "mysql", PlatformType: "oceanbase", TenantMode: test.mode,
				Host: "127.0.0.1", Port: 2881, Username: "tester",
			})
			if err != nil {
				t.Fatal(err)
			}
			defer executor.Close()
			if executor.protocol != test.protocol || executor.metadata.protocol != test.protocol {
				t.Fatalf("wrong OceanBase dialect: executor=%s metadata=%s", executor.protocol, executor.metadata.protocol)
			}
		})
	}
}

func TestOceanBaseOracleMetadataBinds(t *testing.T) {
	query, values, err := rewriteOracleSQLMetadataBinds(
		"SELECT * FROM ALL_TABLES WHERE OWNER=:2 AND TABLE_NAME=:1 AND OWNER=:2",
		[]any{"TABLE", "OWNER"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if query != "SELECT * FROM ALL_TABLES WHERE OWNER=? AND TABLE_NAME=? AND OWNER=?" ||
		!reflect.DeepEqual(values, []any{"OWNER", "TABLE", "OWNER"}) {
		t.Fatalf("incorrect Oracle metadata binds: %s %#v", query, values)
	}
	if _, _, err := rewriteOracleSQLMetadataBinds("SELECT :3 FROM DUAL", []any{"only one"}); err == nil {
		t.Fatal("out-of-range bind must fail")
	}
}

func TestOceanBaseExecutorRejectsMissingTenantMode(t *testing.T) {
	_, err := NewMySQLExecutor(context.Background(), DatabaseConfig{
		Protocol: "mysql", PlatformType: "oceanbase", Host: "127.0.0.1", Port: 2881,
	})
	if err == nil {
		t.Fatal("expected missing OceanBase tenant mode to fail closed")
	}
}
