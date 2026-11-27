package dameng

import (
	"testing"

	"github.com/xo/dburl"
	"github.com/xo/usql/drivers"
)

func TestDamengRegistrationAndURL(t *testing.T) {
	parsed, err := dburl.Parse("dameng://user:p%40ss@127.0.0.1:5525/APP?loginEncrypt=false")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Driver != "dameng" || parsed.GoDriver != "dm" {
		t.Fatalf("unexpected driver mapping: driver=%q go_driver=%q", parsed.Driver, parsed.GoDriver)
	}
	if parsed.DSN != "dm://user:p@ss@127.0.0.1:5525/APP?loginEncrypt=false" {
		t.Fatalf("unexpected Dameng DSN: %q", parsed.DSN)
	}
	if !drivers.Registered("dameng") {
		t.Fatal("Dameng usql driver was not registered")
	}
}

func TestDamengPasswordWithReservedCharacters(t *testing.T) {
	for _, test := range []struct {
		name, input, want string
	}{
		{"at", "dameng://user:p%40ss@db.example:5236", "dm://user:p@ss@db.example:5236"},
		{"percent", "dameng://user:p%25ss@db.example:5236", "dm://user:p%ss@db.example:5236"},
		{"question", "dameng://user:p%3Fss@db.example:5236", "dm://user:p?ss@db.example:5236?"},
		{"question_with_options", "dameng://user:p%3Fss@db.example:5236?loginEncrypt=false", "dm://user:p?ss@db.example:5236?loginEncrypt=false"},
		{"question_with_short_host_and_options", "dameng://user:p%3Fss@x:1?x=1", "dm://user:p?ss@x:1?x=1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			parsed, err := dburl.Parse(test.input)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.DSN != test.want {
				t.Fatalf("DSN = %q, want %q", parsed.DSN, test.want)
			}
		})
	}
}

func TestDamengAlias(t *testing.T) {
	parsed, err := dburl.Parse("dm://user:password@db.example:5236")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Driver != "dameng" || parsed.GoDriver != "dm" {
		t.Fatalf("unexpected alias mapping: driver=%q go_driver=%q", parsed.Driver, parsed.GoDriver)
	}
}
