package sqlservice

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

type MySQLVersionSource uint8

const (
	MySQLVersionUnreported MySQLVersionSource = iota
	MySQLVersionFunction
	MySQLVersionComment
)

// Query supplies the native SQL recorded in catalog provenance and cursor fingerprints.
func (s MySQLVersionSource) Query() string {
	switch s {
	case MySQLVersionFunction:
		return "SELECT VERSION()"
	case MySQLVersionComment:
		return "SELECT @@version_comment"
	default:
		return ""
	}
}

// MySQLServerVersion describes one observation on the caller's physical connection.
// Reported is the original VERSION() text, including its SQL compatibility version.
// Product is the banner used by the feature. An unbranded banner identifies no vendor.
type MySQLServerVersion struct {
	Reported string
	Product  string
	Source   MySQLVersionSource
}

func (v MySQLServerVersion) FromVersionComment() bool {
	return v.Source == MySQLVersionComment
}

func (v MySQLServerVersion) ReportsMariaDB() bool {
	return dialect.HasMariaDBBrand(v.Product)
}

// ReportsOceanBase checks product identity. Operations that depend on tenant mode
// additionally verify @@ob_compatibility_mode on their own connection.
func (v MySQLServerVersion) ReportsOceanBase() bool {
	return dialect.HasOceanBaseBrand(v.Product)
}

// MySQLVersionProbeError identifies the failed query. Unwrap preserves driver
// diagnostics such as an unsupported variable, a lost connection or a deadline.
type MySQLVersionProbeError struct {
	Source MySQLVersionSource
	cause  error
}

func (e *MySQLVersionProbeError) Error() string { return e.cause.Error() }
func (e *MySQLVersionProbeError) Unwrap() error { return e.cause }

// RequireReportedVersion applies the browse and diagram policy. Both require a
// successful VERSION() and propagate cancellation. A failed optional comment
// leaves the product unverified. Overview requires both probes to succeed.
func RequireReportedVersion(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if stopped := ctx.Err(); stopped != nil {
		return stopped
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var probe *MySQLVersionProbeError
	if errors.As(err, &probe) && probe.Source == MySQLVersionComment {
		return nil
	}
	return err
}

// ReadMySQLServerVersion preserves the original report if the optional lookup
// fails. Product identity can also be unknown after two successful queries.
func ReadMySQLServerVersion(ctx context.Context, conn *sql.Conn, d dialect.ID) (MySQLServerVersion, error) {
	var reported string
	if err := conn.QueryRowContext(ctx, MySQLVersionFunction.Query()).Scan(&reported); err != nil {
		return MySQLServerVersion{}, &MySQLVersionProbeError{Source: MySQLVersionFunction, cause: err}
	}
	return resolveMySQLServerVersion(ctx, conn, d, reported)
}

// The diagram reads VERSION() alongside other session settings in one query.
// Resolve its product using the same connection without querying VERSION() twice.
func resolveMySQLServerVersion(ctx context.Context, conn *sql.Conn, d dialect.ID, reported string) (MySQLServerVersion, error) {
	version := MySQLServerVersion{Reported: reported, Product: reported, Source: MySQLVersionFunction}
	if d != dialect.OceanBaseMySQL || version.ReportsOceanBase() {
		return version, nil
	}
	var comment string
	if err := conn.QueryRowContext(ctx, MySQLVersionComment.Query()).Scan(&comment); err != nil {
		// The primary probe succeeded. Callers that accept an unverified
		// product can still use the original banner.
		return version, &MySQLVersionProbeError{Source: MySQLVersionComment, cause: err}
	}
	if dialect.HasOceanBaseBrand(comment) {
		version.Product, version.Source = comment, MySQLVersionComment
	}
	return version, nil
}

// OceanBaseMySQLVersion returns the product version and the query that supplied
// it. Some OceanBase deployments expose only the MySQL compatibility version
// through VERSION().
//
// Deprecated: use ReadMySQLServerVersion for typed query provenance.
func OceanBaseMySQLVersion(ctx context.Context, conn *sql.Conn) (string, string, error) {
	version, err := ReadMySQLServerVersion(ctx, conn, dialect.OceanBaseMySQL)
	if failure, ok := err.(*MySQLVersionProbeError); ok {
		err = failure.Unwrap()
	}
	if version.Source == MySQLVersionUnreported {
		return "", MySQLVersionFunction.Query(), err
	}
	return version.Product, version.Source.Query(), err
}
