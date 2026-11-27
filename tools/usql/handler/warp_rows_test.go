package handler

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jumpserver/koko/tools/usql/feature"
	"github.com/jumpserver/koko/tools/usql/metacmd"
	"github.com/xo/dburl"
	"github.com/xo/tblfmt"
)

func TestMaskedRowsPreserveUnicodeInTerminalOutput(t *testing.T) {
	tests := []struct {
		name, value, method, pattern, want string
	}{
		{"Chinese middle", "管理员", feature.MaskingMethodHideMiddle, "######", "管*员"},
		{"Chinese prefix", "管理员账号", feature.MaskingMethodKeepPrefix, "######", "管理***"},
		{"Chinese suffix", "管理员账号", feature.MaskingMethodKeepSuffix, "######", "***账号"},
		{"Chinese fixed", "管理员", feature.MaskingMethodFixedChar, "######", "######"},
		{"ASCII middle", "rootg", feature.MaskingMethodHideMiddle, "######", "r***g"},
		{"mixed emoji", "管🙂理员", feature.MaskingMethodHideMiddle, "######", "管**员"},
		{"short middle", "管理", feature.MaskingMethodHideMiddle, "######", "######"},
		{"short prefix", "管理", feature.MaskingMethodKeepPrefix, "######", "####"},
		{"short suffix", "管理", feature.MaskingMethodKeepSuffix, "######", "####"},
	}
	db := sql.OpenDB(maskingRowsConnector{})
	t.Cleanup(func() { _ = db.Close() })
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := db.QueryContext(context.Background(), "SELECT name, plain FROM users", tt.value)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			policy, err := feature.CompilePolicy([]feature.DataMaskingRule{{FieldsPattern: "name", MaskingMethod: tt.method, MaskPattern: tt.pattern}})
			if err != nil {
				t.Fatal(err)
			}
			masked, err := rowsWithPolicy(rows, policy)
			if err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := tblfmt.EncodeTable(&output, masked, tblfmt.WithUseColumnTypes(true)); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); !utf8.ValidString(got) || !strings.Contains(got, tt.want) ||
				strings.Contains(got, `\x`) || !strings.Contains(got, "unchanged") {
				t.Fatalf("terminal output = %q, want masked text %q and unchanged plain column", got, tt.want)
			}
		})
	}
}

// MySQL returns text columns as []byte. Feed that database/sql representation
// through WarpRows and the real terminal formatter without a live database.
type maskingRowsConnector struct{}

func (maskingRowsConnector) Driver() driver.Driver { return maskingRowsDriver{} }
func (maskingRowsConnector) Connect(context.Context) (driver.Conn, error) {
	return maskingRowsConn{}, nil
}

type maskingRowsDriver struct{}

func (maskingRowsDriver) Open(string) (driver.Conn, error) { return maskingRowsConn{}, nil }

type maskingRowsConn struct{}

func (maskingRowsConn) Prepare(string) (driver.Stmt, error) { return nil, fmt.Errorf("unsupported") }
func (maskingRowsConn) Close() error                        { return nil }
func (maskingRowsConn) Begin() (driver.Tx, error)           { return nil, fmt.Errorf("unsupported") }
func (maskingRowsConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	first := rowSet{[]string{"name", "plain"}, []driver.Value{"first-secret", "first-public"}}
	next := rowSet{[]string{"plain", "NAME", "group"}, []driver.Value{"next-public", "next-secret", "category"}}
	switch query {
	case "multi":
		return &maskingRows{sets: []rowSet{first, next}}, nil
	case "pivot":
		return &maskingRows{sets: []rowSet{next}}, nil
	default:
		first.values = []driver.Value{[]byte(args[0].Value.(string)), []byte("unchanged")}
		return &maskingRows{sets: []rowSet{first}}, nil
	}
}

type rowSet struct {
	names  []string
	values []driver.Value
}
type maskingRows struct {
	sets []rowSet
	done bool
}

func (r *maskingRows) Columns() []string { return r.sets[0].names }
func (*maskingRows) Close() error        { return nil }
func (r *maskingRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	copy(values, r.sets[0].values)
	r.done = true
	return nil
}
func (r *maskingRows) HasNextResultSet() bool { return len(r.sets) > 1 }
func (r *maskingRows) NextResultSet() error {
	if !r.HasNextResultSet() {
		return io.EOF
	}
	r.sets = r.sets[1:]
	r.done = false
	return nil
}

func TestQueryMaskIsSessionAndResultSetScoped(t *testing.T) {
	db := sql.OpenDB(maskingRowsConnector{})
	defer db.Close()
	protected, plain := Handler{db: db, u: &dburl.URL{}}, Handler{db: db, u: &dburl.URL{}}
	p, err := feature.CompilePolicy([]feature.DataMaskingRule{{FieldsPattern: "name", MaskPattern: "MASKED"}})
	if err != nil {
		t.Fatal(err)
	}
	protected.SetMaskPolicy(p)
	for _, tc := range []struct {
		h             *Handler
		q             string
		cross, masked bool
	}{
		{&protected, "multi", false, true}, {&plain, "multi", false, false}, {&protected, "pivot", true, true}, {&plain, "pivot", true, false},
	} {
		opt := metacmd.Option{Params: map[string]string{"format": "aligned", "pager": "off"}}
		if tc.cross {
			opt.Exec = metacmd.ExecCrosstab
		}
		var out bytes.Buffer
		if err := tc.h.doQuery(context.Background(), &out, opt, "SELECT", tc.q, nil); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		if !strings.Contains(got, "next-public") || strings.Contains(got, "next-secret") == tc.masked || strings.Contains(got, "MASKED") != tc.masked || (!tc.cross && (!strings.Contains(got, "first-public") || strings.Contains(got, "first-secret") == tc.masked)) {
			t.Fatalf("query %s masked %v: %q", tc.q, tc.masked, got)
		}
	}
}
