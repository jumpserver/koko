package oceanbase

import (
	"reflect"
	"testing"

	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
)

func TestOceanBaseURLRegistration(t *testing.T) {
	for _, scheme := range []string{"oceanbase", "oboracle"} {
		parsed, err := dburl.Parse(scheme + "://user:p%40ss@127.0.0.1:2881/app")
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Driver != scheme || parsed.GoDriver != "oceanbase" {
			t.Fatalf("%s: unexpected driver mapping %q / %q", scheme, parsed.Driver, parsed.GoDriver)
		}
		if !drivers.Registered(scheme) {
			t.Fatalf("%s driver is not registered", scheme)
		}
	}
}

func TestRewriteOracleMetadataBinds(t *testing.T) {
	query, args, err := rewriteOracleMetadataBinds(
		"SELECT * FROM ALL_OBJECTS WHERE OWNER=:2 AND OBJECT_NAME=:1 AND OWNER=:2",
		[]interface{}{"TABLE", "OWNER"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if query != "SELECT * FROM ALL_OBJECTS WHERE OWNER=? AND OBJECT_NAME=? AND OWNER=?" ||
		!reflect.DeepEqual(args, []interface{}{"OWNER", "TABLE", "OWNER"}) {
		t.Fatalf("wrong bind translation: %q %#v", query, args)
	}
	if _, _, err = rewriteOracleMetadataBinds("SELECT :3 FROM DUAL", []interface{}{"only one"}); err == nil {
		t.Fatal("out-of-range metadata bind must fail")
	}
}
