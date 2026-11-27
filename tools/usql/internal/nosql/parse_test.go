package nosql

import (
	"reflect"
	"testing"
)

func TestStatementBoundaries(t *testing.T) {
	for _, test := range []struct {
		input               string
		mongo               bool
		want                []string
		incomplete, invalid bool
	}{
		{input: `use test; db.c.find({text:"a;b", regex:/[;/]/}).sort({_id:1})`, mongo: true, want: []string{"use test", `db.c.find({text:"a;b", regex:/[;/]/}).sort({_id:1})`}},
		{input: "// note\ndb.c.find({\n x:1, /* ; } */ y:'中文'\n});\nshow collections", mongo: true, want: []string{"db.c.find({\n x:1,   y:'中文'\n})", "show collections"}},
		{input: "db.c.find({\n x:1", mongo: true, incomplete: true},
		{input: "db.c.find({])", mongo: true, invalid: true},
		{input: "/* comment", mongo: true, incomplete: true},
		{input: "// comment", mongo: true},
		{input: "SET name \"a;b\"\nGET name", want: []string{`SET name "a;b"`, "GET name"}},
		{input: "SET key value;part", want: []string{"SET key value;part"}},
	} {
		t.Run(test.input, func(t *testing.T) {
			got, incomplete, err := splitStatements(test.input, test.mongo)
			if (err != nil) != test.invalid || incomplete != test.incomplete || !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %#v, incomplete=%v, err=%v; want %#v incomplete=%v invalid=%v", got, incomplete, err, test.want, test.incomplete, test.invalid)
			}
		})
	}
}

func TestRedisArguments(t *testing.T) {
	got, err := redisArgs(`SET "空 键" "a\n\t\x00\"b" '' 'it\'s' café`)
	want := []any{"SET", "空 键", "a\n\t\x00\"b", "", "it's", "café"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v %v, want %#v", got, err, want)
	}
	for _, input := range []string{`SET "x`, `SET "x"tail`, `SET "\xZZ"`, `SET "\x1"`} {
		if _, err := redisArgs(input); err == nil {
			t.Errorf("accepted malformed arguments %q", input)
		}
	}
}
