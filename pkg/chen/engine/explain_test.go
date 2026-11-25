package engine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
	ob "github.com/oceanbase/go-oceanbase-driver"
)

// Exercise the plan-preparation entry point on a single physical connection.
type explainProbeConn struct {
	driver.Conn
	reported   string
	commentErr error
}

func (c *explainProbeConn) Connect(context.Context) (driver.Conn, error) { return c, nil }
func (*explainProbeConn) Driver() driver.Driver                          { return nil }
func (*explainProbeConn) Close() error                                   { return nil }
func (c *explainProbeConn) QueryContext(_ context.Context, q string, _ []driver.NamedValue) (driver.Rows, error) {
	switch q {
	case "SELECT VERSION()":
		return &explainProbeRow{c.reported}, nil
	case "SELECT @@version_comment":
		if c.commentErr != nil {
			return nil, c.commentErr
		}
		return &explainProbeRow{"Community"}, nil
	default:
		return nil, fmt.Errorf("unexpected query %q", q)
	}
}

type explainProbeRow struct{ value driver.Value }

func (*explainProbeRow) Columns() []string { return []string{"version"} }
func (*explainProbeRow) Close() error      { return nil }
func (r *explainProbeRow) Next(dest []driver.Value) error {
	if r.value == nil {
		return io.EOF
	}
	dest[0], r.value = r.value, nil
	return nil
}

func TestEstimatedPlanSQLVersionProbe(t *testing.T) {
	unsupported := &ob.MySQLError{Number: 1193}
	for _, tc := range []struct {
		name, reported string
		commentErr     error
		wantPlan       bool
	}{
		{"unverified product", "5.7.25", nil, true},
		{"unavailable variable", "5.7.25", unsupported, true},
		{"missing report and unavailable variable", "", unsupported, false},
		{"successful empty report", "", nil, true},
		{"lost connection", "5.7.25", driver.ErrBadConn, false},
		{"interrupted", "5.7.25", context.Canceled, false},
		{"timeout", "5.7.25", context.DeadlineExceeded, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := sql.OpenDB(&explainProbeConn{reported: tc.reported, commentErr: tc.commentErr})
			defer db.Close()
			ctx := context.Background()
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			command, version, err := estimatedPlanSQL(ctx, &console{conn: conn}, dialect.OceanBaseMySQL, "SELECT 1", sqlservice.AnalyzeOptions{})
			if tc.wantPlan {
				if err != nil || command != "EXPLAIN SELECT 1" || version != tc.reported {
					t.Fatalf("plan %q, version %q, error %v", command, version, err)
				}
			} else if !errors.Is(err, tc.commentErr) || command != "" || version != "" {
				t.Fatalf("failed probe returned plan %q, version %q, error %v", command, version, err)
			}
		})
	}
}
