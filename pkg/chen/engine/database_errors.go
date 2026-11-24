package engine

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	dm "gitee.com/chunanyong/dm"
	"github.com/ClickHouse/clickhouse-go/v2"
	mssql "github.com/microsoft/go-mssqldb"
	ob "github.com/oceanbase/go-oceanbase-driver"
	mysqlcatalog "github.com/pingcap/tidb/pkg/parser/mysql"
	oranetwork "github.com/sijms/go-ora/v2/network"
)

const (
	hiddenDatabaseDiagnostic = "Database diagnostic details are hidden by the data masking policy"
	clickHouseQuotedValue    = `'(?:[^'\\]|\\(?s:.))*'`
)

var (
	clickHouseErrorTrailer = regexp.MustCompile(` \(NOT_IMPLEMENTED\)(?: \(version [0-9]{1,2}\.[0-9]{1,2}\.[0-9]{1,3}\.[0-9]{1,5}(?: \(official build\))?\))?$`)
	clickHouseInvalidInput = regexp.MustCompile(`^Cannot parse string ` + clickHouseQuotedValue +
		` as (U?Int(?:8|16|32|64|128|256)|Float(?:32|64)): syntax error (?:at begin of string\b|at position [0-9]+ \(parsed just ` + clickHouseQuotedValue + `\))`)
	postgresInvalidInput = regexp.MustCompile(`^invalid input syntax for type (?:smallint|integer|bigint|numeric|real|double precision|boolean|uuid|json): `)
	mysqlMessageArgument = regexp.MustCompile(`%[-.0-9]*[sdv]`)
)

func (v *Server) maskDatabaseDiagnostic(s *session, diagnostic *APIError) (*APIError, bool) {
	if _, masked := v.provider.Mask(s.grant, "", "", diagnostic.Message); !masked {
		return diagnostic, false
	}
	return &APIError{diagnostic.Code, maskedDatabaseMessage(diagnostic.Code, diagnostic.Message)}, true
}

// Error messages have no source-column metadata. Only copy complete, known
// value-free messages or rebuild the fixed parts of a diagnostic that quotes data.
func maskedDatabaseMessage(code, message string) string {
	switch code {
	case "CLICKHOUSE_6":
		if m := clickHouseInvalidInput.FindStringSubmatch(message); m != nil {
			// The parsed prefix, position and any appended context also describe the input.
			return "Cannot parse string '####' as " + m[1] + ": syntax error"
		}
	case "CLICKHOUSE_48":
		// The native driver returns the bare message. HTTP can append the
		// symbolic code and server version to the same diagnostic.
		const missingColumn = "Lightweight updates are not supported. " +
			"Lightweight updates are supported only for tables with materialized _%s column. " +
			"Run 'MODIFY SETTING enable_%s_column = 1' command to enable it"
		body := strings.TrimSuffix(clickHouseErrorTrailer.ReplaceAllString(message, ""), ".")
		for _, column := range []string{"block_number", "block_offset"} {
			if body == fmt.Sprintf(missingColumn, column, column) {
				return message
			}
		}
	case "22P02":
		if prefix := postgresInvalidInput.FindString(message); prefix != "" {
			return prefix + `"####"`
		}
	case "22012":
		if message == "division by zero" {
			return message
		}
	default:
		vendor, number, _ := strings.Cut(code, "_")
		if vendor == "MYSQL" || vendor == "OCEANBASE" {
			return maskedMySQLDiagnostic(number, message)
		}
	}
	return hiddenDatabaseDiagnostic
}

// Rebuild the message from the MySQL catalog so interpolated values,
// including values with embedded quotes or newlines, are never copied.
func maskedMySQLDiagnostic(number, message string) string {
	n, err := strconv.ParseUint(number, 10, 16)
	template := mysqlcatalog.MySQLErrName[uint16(n)]
	if err != nil || template == nil {
		return hiddenDatabaseDiagnostic
	}
	if message == template.Raw {
		return message
	}
	parts := mysqlMessageArgument.Split(template.Raw, -1)
	if len(parts) < 2 || parts[0] == "" {
		return hiddenDatabaseDiagnostic
	}
	body, ok := strings.CutPrefix(message, parts[0])
	if !ok {
		return hiddenDatabaseDiagnostic
	}
	body, ok = strings.CutSuffix(body, parts[len(parts)-1])
	if !ok {
		return hiddenDatabaseDiagnostic
	}
	for _, separator := range parts[1 : len(parts)-1] {
		_, body, ok = strings.Cut(body, separator)
		if !ok {
			return hiddenDatabaseDiagnostic
		}
	}
	return strings.Join(parts, "####")
}

func extraDatabaseError(err error) *APIError {
	var ms mssql.Error
	if errors.As(err, &ms) {
		return &APIError{fmt.Sprintf("MSSQL_%d", ms.Number), ms.Message}
	}
	var oracle *oranetwork.OracleError
	if errors.As(err, &oracle) {
		return &APIError{fmt.Sprintf("ORA_%05d", oracle.ErrCode), oracle.Error()}
	}
	var oceanbase *ob.MySQLError
	if errors.As(err, &oceanbase) {
		return &APIError{fmt.Sprintf("OCEANBASE_%d", oceanbase.Number), oceanbase.Message}
	}
	var dameng *dm.DmError
	if errors.As(err, &dameng) {
		if isDamengTransportError(err) {
			return nil
		}
		return &APIError{fmt.Sprintf("DM_%d", dameng.ErrCode), dameng.ErrText}
	}
	var ch *clickhouse.Exception
	if errors.As(err, &ch) {
		return &APIError{fmt.Sprintf("CLICKHOUSE_%d", ch.Code), ch.Message}
	}
	return db2DatabaseError(err)
}

func extraServerCancellation(err error) bool {
	var oracle *oranetwork.OracleError
	if errors.As(err, &oracle) {
		return oracle.ErrCode == 1013
	}
	var oceanbase *ob.MySQLError
	if errors.As(err, &oceanbase) {
		return oceanbase.Number == 1317
	}
	var ch *clickhouse.Exception
	if errors.As(err, &ch) {
		return ch.Code == 394
	}
	return db2ServerCancellation(err)
}
