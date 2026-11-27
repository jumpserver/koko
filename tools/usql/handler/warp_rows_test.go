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

	"github.com/jumpserver-dev/usql/feature"
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
			masked := &WarpRows{
				rows: rows, maskIndexes: []int{0},
				dataMaskRules: map[int]feature.DataMaskingRule{
					0: {MaskingMethod: tt.method, MaskPattern: tt.pattern},
				},
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
func (maskingRowsConn) QueryContext(_ context.Context, _ string, args []driver.NamedValue) (driver.Rows, error) {
	return &maskingRows{value: args[0].Value.(string)}, nil
}

type maskingRows struct {
	value string
	done  bool
}

func (*maskingRows) Columns() []string { return []string{"name", "plain"} }
func (*maskingRows) Close() error      { return nil }
func (r *maskingRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	values[0], values[1] = []byte(r.value), []byte("unchanged")
	r.done = true
	return nil
}
