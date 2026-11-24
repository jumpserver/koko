package engine

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/sqlservice"
	goora "github.com/sijms/go-ora/v2"
)

var editTimestampTZExcessPrecision = regexp.MustCompile(`[.,][0-9]{10,}`)

const editTimestampTZFormat = `'YYYY-MM-DD"T"HH24:MI:SS.FF9TZH:TZM'`

func editBaseType(t string) string {
	return strings.ToUpper(sqlservice.EditTypeName(t))
}

func editOracleTimezone(dialect string, field sqlservice.EditColumn) bool {
	return (dialect == "oracle" || dialect == "oceanbase-oracle") && editBaseType(field.DataType) == "TIMESTAMP WITH TIME ZONE"
}

func editOracleJSON(dialect string, field sqlservice.EditColumn) bool {
	return (dialect == "oracle" || dialect == "oceanbase-oracle") && editBaseType(field.DataType) == "JSON"
}

func editDriverBindValue(dialect string, field sqlservice.EditColumn, value any) any {
	if dialect == "oracle" && editOracleJSON(dialect, field) && value != nil {
		// JSON accepts a UTF-8 BLOB independently of the database/client
		// VARCHAR2 character set. A Go string bind uses that character set.
		return goora.Blob{Data: []byte(value.(string)), Valid: true}
	}
	return value
}

func editTypedLiteral(dialect string, field sqlservice.EditColumn, value any, masked bool) string {
	if dialect == "oracle" && editOracleJSON(dialect, field) && value != nil && !masked {
		value = editASCIIJSON(value.(string))
	}
	return editTypedExpression(dialect, field, editMaskSQL(dialect, value, masked))
}

// Preserve JSON spelling/number precision while making SQL preview literals
// independent of a database character set that cannot represent the document.
func editASCIIJSON(text string) string {
	const digits = "0123456789abcdef"
	var out strings.Builder
	writeUnit := func(unit rune) {
		out.WriteString(`\u`)
		for shift := 12; shift >= 0; shift -= 4 {
			out.WriteByte(digits[(unit>>shift)&15])
		}
	}
	for _, value := range text {
		if value <= 127 {
			out.WriteByte(byte(value))
		} else if value <= 0xffff {
			writeUnit(value)
		} else {
			high, low := utf16.EncodeRune(value)
			writeUnit(high)
			writeUnit(low)
		}
	}
	return out.String()
}

// Bind timezone values with an explicit, numeric-offset format. In particular,
// never reinterpret a cached value using the console's current NLS settings.
func normalizeTypedEditValue(dialect string, field sqlservice.EditColumn, value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	if text, ok := value.(string); ok {
		typeName := strings.TrimSuffix(editBaseType(field.DataType), " UNSIGNED")
		bits := 0
		switch typeName {
		case "TINYINT":
			bits = 8
		case "SMALLINT", "INT2", "SMALLSERIAL":
			bits = 16
		case "MEDIUMINT":
			bits = 24
		case "INT", "INTEGER", "INT4", "SERIAL":
			bits = 32
		case "BIGINT", "INT8", "BIGSERIAL":
			bits = 64
		}
		if bits > 0 {
			integer, err := exactEditInteger(text)
			var signed int64
			if err == nil {
				if strings.HasSuffix(editBaseType(field.DataType), " UNSIGNED") || dialect == "sqlserver" && typeName == "TINYINT" {
					_, err = strconv.ParseUint(integer, 10, bits)
				} else {
					signed, err = strconv.ParseInt(integer, 10, bits)
				}
			}
			if err != nil {
				return nil, errors.New("Enter a whole number within the column's range")
			}
			if dialect == "db2" {
				return signed, nil
			}
			return integer, nil
		}
		if dialect == "db2" {
			return db2EditTextValue(field.DataType, typeName, text)
		}
	}
	if editOracleTimezone(dialect, field) {
		return editTimestampTZValue(value)
	}
	if sqlservice.IsOracleDialect(sqlservice.Dialect(dialect)) && (editBaseType(field.DataType) == "DATE" || editBaseType(field.DataType) == "TIMESTAMP") {
		var calendar time.Time
		valid := false
		switch v := value.(type) {
		case time.Time:
			calendar = v
			valid = true
		case string:
			if editTimestampTZExcessPrecision.MatchString(v) {
				return nil, errors.New("Timestamp supports at most 9 fractional-second digits")
			}
			for _, layout := range []string{"2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999", "2006-01-02"} {
				parsed, err := time.Parse(layout, strings.TrimSpace(v))
				if err == nil {
					calendar = parsed
					valid = true
					break
				}
			}
			if !valid && editBaseType(field.DataType) == "TIMESTAMP" {
				if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(v)); err == nil {
					// Java's Timestamp.from preserves the instant, then JDBC uses
					// the process local zone for an unzoned timestamp bind.
					calendar, valid = parsed.In(time.Local), true
				}
			}
		}
		if !valid || calendar.Year() < 1 || calendar.Year() > 9999 {
			return nil, errors.New("Enter an ISO date or date and time")
		}
		if editBaseType(field.DataType) == "DATE" {
			return calendar.Format("2006-01-02 15:04:05"), nil
		}
		return calendar.Format("2006-01-02 15:04:05.000000000"), nil
	}
	if editOracleJSON(dialect, field) {
		text, ok := value.(string)
		if !ok || !utf8.ValidString(text) || !json.Valid([]byte(text)) {
			return nil, errors.New("JSON must be complete UTF-8 text")
		}
		return text, nil
	}
	return value, nil
}

func editTimestampTZValue(value any) (string, error) {
	var instant time.Time
	valid := false
	switch v := value.(type) {
	case time.Time:
		instant = v
		valid = true
	case string:
		if editTimestampTZExcessPrecision.MatchString(v) {
			return "", errors.New("Timestamp with time zone supports at most 9 fractional-second digits")
		}
		// Oracle's native driver materializes this type as RFC3339Nano. The
		// OceanBase text protocol also permits strings: accept only forms that
		// carry an unambiguous full year and numeric offset, including its ISO
		// session format. Two-digit years or region-only NLS output fail closed.
		for _, layout := range []string{
			time.RFC3339Nano,
			"2006-01-02 15:04:05.999999999 -07:00",
			"2006-01-02 15:04:05.999999999-07:00",
			"2006-01-02 15:04:05.999999999 -0700",
			"02-Jan-2006 03.04.05.999999999 PM -07:00",
			"02-Jan-2006 15:04:05.999999999 -07:00",
		} {
			var err error
			instant, err = time.Parse(layout, strings.TrimSpace(v))
			if err == nil {
				valid = true
				break
			}
		}
	}
	_, offset := instant.Zone()
	if !valid || instant.Year() < 1 || instant.Year() > 9999 || offset%60 != 0 || offset < -12*3600 || offset > 14*3600 {
		return "", errors.New("Timestamp with time zone requires a full year and numeric offset, for example 2026-11-02T13:14:15.123456789+08:00; ambiguous cached NLS values require an ISO session format and a refresh")
	}
	return instant.Format("2006-01-02T15:04:05.000000000-07:00"), nil
}

func editTypedExpression(dialect string, field sqlservice.EditColumn, expression string) string {
	if dialect == "db2" && expression == "?" && db2EditTemporalType(editBaseType(field.DataType)) {
		// Db2 uses the target column's temporal precision for a string in an
		// assignment or comparison. Its native Go timestamp bind only sends microseconds.
		return "CAST(? AS VARCHAR(" + strconv.Itoa(db2TemporalBindSize) + "))"
	}
	if dialect == "oracle" && editOracleJSON(dialect, field) {
		return "JSON(" + expression + ")"
	}
	if editOracleTimezone(dialect, field) {
		return "TO_TIMESTAMP_TZ(" + expression + ", " + editTimestampTZFormat + ")"
	}
	if sqlservice.IsOracleDialect(sqlservice.Dialect(dialect)) {
		switch editBaseType(field.DataType) {
		case "DATE":
			return "TO_DATE(" + expression + ", 'YYYY-MM-DD HH24:MI:SS')"
		case "TIMESTAMP":
			return "TO_TIMESTAMP(" + expression + ", 'YYYY-MM-DD HH24:MI:SS.FF9')"
		}
	}
	return expression
}

func editTypedEquality(dialect string, field sqlservice.EditColumn, column, expression string) string {
	if editOracleJSON(dialect, field) {
		return "JSON_EQUAL(" + column + ", " + editTypedExpression(dialect, field, expression) + " ERROR ON ERROR)"
	}
	return column + " = " + editTypedExpression(dialect, field, expression)
}
