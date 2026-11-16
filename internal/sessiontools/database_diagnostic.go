package sessiontools

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	ob "github.com/oceanbase/go-oceanbase-driver"
	"go.mongodb.org/mongo-driver/mongo"
)

func (s *MySQLSanitizer) diagnostic(err error) error {
	if err == nil || s == nil || len(s.rules) == 0 {
		return err
	}
	var pg *pgconn.PgError
	var ch *clickhouse.Exception
	var my *mysql.MySQLError
	var ocean *ob.MySQLError
	var driver, code, message string
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return context.DeadlineExceeded
	case errors.As(err, &pg):
		driver, code, message = "pq", pg.Code, pg.Message
	case errors.As(err, &ch):
		driver, code, message = "clickhouse", strconv.Itoa(int(ch.Code)), ch.Message
	case errors.As(err, &my):
		driver, code, message = "mysql", strconv.Itoa(int(my.Number)), my.Message
	case errors.As(err, &ocean):
		driver, code, message = "oceanbase", strconv.Itoa(int(ocean.Number)), ocean.Message
	case mongo.IsDuplicateKeyError(err):
		return errors.New("MongoDB duplicate key: ####")
	}
	masked := maskedDiagnostic(driver, code, message)
	if masked == hiddenDiagnostic {
		return errors.New(masked)
	}
	return errors.New(driver + ": " + code + ": " + masked)
}

const hiddenDiagnostic = "Database diagnostic details are hidden by the data masking policy"

var (
	pgInvalidInput = regexp.MustCompile(`^invalid input syntax for type (smallint|integer|bigint|numeric|real|double precision|boolean|uuid|json|date|timestamp with(?:out)? time zone): "`)
	// ClickHouse quotes the input and escapes embedded quotes and backslashes.
	chInvalidInput = regexp.MustCompile(`^Cannot parse string '(?:[^'\\]|\\(?s:.))*' as (U?Int(?:8|16|32|64|128|256)|Float(?:32|64)): syntax error\b`)
	chVersion      = regexp.MustCompile(`^ \(NOT_IMPLEMENTED\)(?: \(version [0-9]{1,2}\.[0-9]{1,2}\.[0-9]{1,3}\.[0-9]{1,5}(?: \(official build\))?\))?$`)
)

// maskedDiagnostic reconstructs recognized diagnostics from fixed text. Driver messages
// have no source column, and their values can contain quotes and line breaks.
func maskedDiagnostic(driver, code, message string) string {
	switch driver {
	case "pq":
		switch code {
		case "22P02", "22007":
			if m := pgInvalidInput.FindStringSubmatch(message); m != nil {
				return `invalid input syntax for type ` + m[1] + `: "####"`
			}
		case "22012":
			if message == "division by zero" {
				return message
			}
		}
	case "clickhouse":
		switch code {
		case "6":
			if m := chInvalidInput.FindStringSubmatch(message); m != nil {
				// The parsed prefix and the trailing expression can repeat the input.
				return "Cannot parse string '####' as " + m[1] + ": syntax error"
			}
		case "48":
			for _, column := range []string{"block_number", "block_offset"} {
				setting := "Lightweight updates are not supported. Lightweight updates are supported only for tables with materialized _" +
					column + " column. Run 'MODIFY SETTING enable_" + column + "_column = 1' command to enable it"
				suffix, ok := strings.CutPrefix(message, setting)
				suffix = strings.TrimPrefix(suffix, ".")
				if ok && (suffix == "" || chVersion.MatchString(suffix)) {
					return setting
				}
			}
		}
	case "mysql", "oceanbase", "oboracle":
		switch code {
		case "1062":
			if strings.HasPrefix(message, "Duplicate entry '") && strings.Contains(message, "' for key '") && strings.HasSuffix(message, "'") {
				return "Duplicate entry '####' for key '####'"
			}
		case "1046":
			if message == "No database selected" {
				return message
			}
		}
	}
	return hiddenDiagnostic
}
