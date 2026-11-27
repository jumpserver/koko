// Package dbdiagnostic formats database errors for sessions with data masking enabled.
package dbdiagnostic

import (
	"regexp"
	"strings"
)

const Hidden = "Database diagnostic details are hidden by the data masking policy"

var (
	pgInvalidInput = regexp.MustCompile(`^invalid input syntax for type (smallint|integer|bigint|numeric|real|double precision|boolean|uuid|json|date|timestamp with(?:out)? time zone): "`)
	// ClickHouse quotes the input and escapes embedded quotes and backslashes.
	chInvalidInput = regexp.MustCompile(`^Cannot parse string '(?:[^'\\]|\\(?s:.))*' as (U?Int(?:8|16|32|64|128|256)|Float(?:32|64)): syntax error\b`)
	chVersion      = regexp.MustCompile(` \(NOT_IMPLEMENTED\)(?: \(version [0-9]{1,2}\.[0-9]{1,2}\.[0-9]{1,3}\.[0-9]{1,5}(?: \(official build\))?\))?$`)
)

// Mask reconstructs recognized diagnostics from fixed text. Driver messages
// have no source column, and their values can contain quotes and line breaks.
func Mask(driver, code, message string) string {
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
			body := strings.TrimSuffix(chVersion.ReplaceAllString(message, ""), ".")
			for _, column := range []string{"block_number", "block_offset"} {
				setting := "Lightweight updates are not supported. Lightweight updates are supported only for tables with materialized _" +
					column + " column. Run 'MODIFY SETTING enable_" + column + "_column = 1' command to enable it"
				if body == setting {
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
	return Hidden
}

// Message accepts the complete diagnostic produced by a usql driver. Only
// recognized driver names and error codes may be returned to the terminal.
func Message(text string) string {
	driver, rest, _ := strings.Cut(text, ": ")
	code, message, _ := strings.Cut(rest, ": ")
	if masked := Mask(driver, code, message); masked != Hidden {
		return driver + ": " + code + ": " + masked
	}
	return Hidden
}
