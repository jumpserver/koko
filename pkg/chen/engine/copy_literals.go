package engine

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/dialect"
)

var mysqlCopyTemporal = regexp.MustCompile(`^(?:[0-9]{4}-[0-9]{2}-[0-9]{2}(?: [0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?)?|-?[0-9]{1,3}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?)$`)

// Catalog character lengths can include a BYTE or CHAR unit.
var copyTypeParameters = regexp.MustCompile(`(?i)^(CHAR|VARCHAR2?)\( *[0-9]+ +(?:BYTE|CHAR) *\)$|\([0-9 ,]+\)`)

// This is a closed set of literal codecs, not the parameterized write-back
// formatter. Unknown types fail even for NULL, so their semantics stay explicit.
func copySQLLiteral(d dialect.ID, dataType string, value any) (string, error) {
	t := copyTypeName(dataType)
	if d.UsesMySQLSyntax() {
		t = strings.TrimSuffix(t, " unsigned")
	}
	kind := ""
	if d == dialect.ClickHouse {
		for {
			unwrapped := false
			for _, wrapper := range []string{"nullable(", "lowcardinality("} {
				if strings.HasPrefix(t, wrapper) && strings.HasSuffix(t, ")") {
					t = t[len(wrapper) : len(t)-1]
					unwrapped = true
					break
				}
			}
			if !unwrapped {
				break
			}
		}
	}
	switch d {
	case dialect.DB2:
		switch t {
		case "smallint", "integer", "int", "bigint", "decimal", "numeric", "decfloat", "real", "double", "float":
			kind = "number"
		case "char", "character", "varchar", "character varying", "graphic", "vargraphic", "clob", "dbclob", "long varchar":
			kind = "text"
		case "boolean":
			kind = "bool"
		case "blob", "binary", "varbinary":
			kind = "binary"
		case "date", "time", "timestamp":
			kind = "temporal"
		}
	case dialect.ClickHouse:
		switch t {
		case "int8", "int16", "int32", "int64", "int128", "int256", "uint8", "uint16", "uint32", "uint64", "uint128", "uint256", "float32", "float64", "decimal", "decimal32", "decimal64", "decimal128", "decimal256":
			kind = "number"
		case "string", "fixedstring":
			kind = "text"
		case "bool", "boolean":
			kind = "bool"
		case "uuid":
			kind = "uuid"
		case "date", "date32", "datetime", "datetime64":
			kind = "temporal"
		}
	case dialect.PostgreSQL:
		switch t {
		case "int2", "int4", "int8", "smallint", "integer", "bigint", "numeric", "decimal", "real", "double precision", "float4", "float8":
			kind = "number"
		case "char", "character", "bpchar", "varchar", "character varying", "text", "name":
			kind = "text"
		case "boolean", "bool":
			kind = "bool"
		case "uuid":
			kind = "uuid"
		case "json", "jsonb":
			kind = "json"
		case "bytea":
			kind = "binary"
		case "date", "time", "time without time zone", "timestamp", "timestamp without time zone", "timestamptz", "timestamp with time zone":
			kind = "temporal"
		}
	case dialect.MySQL, dialect.MariaDB, dialect.OceanBaseMySQL:
		switch t {
		case "tinyint", "smallint", "mediumint", "int", "integer", "bigint", "numeric", "decimal", "float", "double", "real":
			kind = "number"
		case "char", "varchar", "tinytext", "text", "mediumtext", "longtext", "enum", "set":
			kind = "text"
		case "boolean", "bool":
			kind = "bool"
		case "json":
			kind = "json"
		case "binary", "varbinary", "tinyblob", "blob", "mediumblob", "longblob":
			kind = "binary"
		case "date", "time", "datetime":
			kind = "temporal"
		case "timestamp":
			return "", errors.New("MySQL TIMESTAMP copying requires a frozen result time_zone, which this result does not retain")
		}
	case dialect.SQLServer:
		switch t {
		case "tinyint", "smallint", "int", "bigint", "numeric", "decimal", "float", "real", "money", "smallmoney":
			kind = "number"
		case "char", "varchar", "text", "nchar", "nvarchar", "ntext":
			kind = "text"
		case "bit":
			kind = "bool"
		case "uniqueidentifier":
			kind = "uuid"
		case "binary", "varbinary", "image":
			kind = "binary"
		case "date", "time", "datetime", "smalldatetime", "datetime2", "datetimeoffset":
			kind = "temporal"
		}
	case dialect.Dameng:
		switch t {
		case "tinyint", "smallint", "int", "integer", "bigint", "dec", "decimal", "numeric", "number", "real", "float", "double", "double precision":
			kind = "number"
		case "char", "varchar", "varchar2":
			kind = "text"
		case "bit":
			kind = "bool"
		case "binary", "varbinary", "raw":
			kind = "binary"
		case "date", "time", "timestamp", "datetime":
			kind = "temporal"
		}
	case dialect.OceanBaseOracle:
		switch t {
		case "number", "float", "binary_float", "binary_double":
			kind = "number"
		case "char", "varchar", "varchar2", "nchar", "nvarchar2":
			kind = "text"
		case "raw":
			kind = "binary"
		case "date", "timestamp":
			kind = "temporal"
		}
	case dialect.Oracle:
		switch t {
		case "number", "float", "binary_float", "binary_double":
			kind = "number"
		case "char", "varchar2", "nchar", "nvarchar2":
			kind = "text"
		case "raw":
			kind = "binary"
		case "date", "timestamp":
			kind = "temporal"
		}
	default:
		return "", errors.New("SQL copying is not supported for this dialect")
	}
	if kind == "" {
		return "", fmt.Errorf("SQL copying does not support type %s for %s", dataType, d)
	}
	if value == nil {
		return "NULL", nil
	}
	text := ""
	switch x := value.(type) {
	case string:
		text = x
	case bool:
		text = strconv.FormatBool(x)
	case float64:
		text = strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return "", errors.New("The cached value has no reliable SQL literal representation")
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", errors.New("Values must be valid text without NUL")
	}
	switch kind {
	case "number":
		if !editNumber.MatchString(text) {
			return "", errors.New("The cached numeric value is not a finite, exact SQL number")
		}
		return text, nil
	case "bool":
		b := text == "true" || text == "1"
		if !b && text != "false" && text != "0" {
			return "", errors.New("The cached boolean value is invalid")
		}
		if d == dialect.PostgreSQL || d == dialect.DB2 {
			if b {
				return "TRUE", nil
			}
			return "FALSE", nil
		}
		if b {
			return "1", nil
		}
		return "0", nil
	case "binary":
		if !strings.HasPrefix(text, "0x") {
			return "", errors.New("Binary copying requires the complete hexadecimal cached value")
		}
		data, err := hex.DecodeString(text[2:])
		if err != nil {
			return "", errors.New("The cached binary value is invalid")
		}
		h := hex.EncodeToString(data)
		switch d {
		case dialect.PostgreSQL:
			return "decode('" + h + "', 'hex')", nil
		case dialect.SQLServer:
			return "0x" + h, nil
		case dialect.DB2:
			return "BLOB(X'" + h + "')", nil
		case dialect.Dameng:
			if len(data) == 0 || len(data) > 32767 {
				return "", errors.New("DM binary SQL copying supports 1 to 32767 bytes")
			}
			return "0x" + h, nil
		case dialect.OceanBaseOracle:
			if len(data) == 0 || len(data) > 2000 {
				return "", errors.New("OB Oracle RAW SQL copying supports 1 to 2000 bytes")
			}
			return "HEXTORAW('" + h + "')", nil
		case dialect.Oracle:
			if len(data) == 0 || len(data) > 2000 {
				return "", errors.New("Oracle RAW SQL copying supports 1 to 2000 bytes")
			}
			return "HEXTORAW('" + h + "')", nil
		default:
			return "X'" + h + "'", nil
		}
	case "temporal":
		if d == dialect.Oracle || d == dialect.OceanBaseOracle {
			return copyOracleTemporalLiteral(t, text)
		}
		return copyTemporalLiteral(d, t, text)
	case "uuid":
		if len(text) != 36 {
			return "", errors.New("The cached UUID is invalid")
		}
		for i, c := range text {
			if i == 8 || i == 13 || i == 18 || i == 23 {
				if c != '-' {
					return "", errors.New("The cached UUID is invalid")
				}
			} else if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return "", errors.New("The cached UUID is invalid")
			}
		}
	case "json":
		if !json.Valid([]byte(text)) {
			return "", errors.New("The cached JSON is invalid")
		}
	}
	literal, err := copyTextLiteral(d, text)
	if err != nil {
		return "", err
	}
	if d == dialect.PostgreSQL && (kind == "uuid" || kind == "json") {
		literal = "CAST(" + literal + " AS " + t + ")"
	}
	return literal, nil
}
func copyTypeName(dataType string) string {
	name := copyTypeParameters.ReplaceAllString(strings.TrimSpace(dataType), "${1}")
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}

func copyTextLiteral(d dialect.ID, text string) (string, error) {
	escaped := strings.ReplaceAll(text, "'", "''")
	switch d {
	case dialect.DB2:
		return "'" + escaped + "'", nil
	case dialect.ClickHouse:
		return "unhex('" + hex.EncodeToString([]byte(text)) + "')", nil
	case dialect.MySQL, dialect.MariaDB, dialect.OceanBaseMySQL:
		// Hex text with an explicit charset is independent of NO_BACKSLASH_ESCAPES,
		// ANSI_QUOTES and the session connection charset. It preserves UTF-8 bytes.
		return "CONVERT(X'" + hex.EncodeToString([]byte(text)) + "' USING utf8mb4)", nil
	case dialect.PostgreSQL:
		return "E'" + strings.ReplaceAll(escaped, "\\", "\\\\") + "'", nil
	case dialect.SQLServer:
		return "N'" + escaped + "'", nil
	case dialect.Dameng:
		// DM distinguishes empty strings in its native mode. The HTTP consumer
		// confirms empty-string and UNISTR behavior on the pinned connection.
		if text == "" {
			return "''", nil
		}
		encoded := copyUnicodeText(text)
		if len(encoded) > 32767 {
			return "", errors.New("DM character literal encoding exceeds the 32767-byte copier limit")
		}
		return "UNISTR('" + encoded + "')", nil
	case dialect.OceanBaseOracle:
		// Its own HTTP capability probe confirms whether an empty string
		// survives and whether UNISTR preserves the selected character class.
		if text == "" {
			return "''", nil
		}
		encoded := copyUnicodeText(text)
		if len(encoded) > 4000 {
			return "", errors.New("OB Oracle character literal encoding exceeds the 4000-byte copier limit")
		}
		return "UNISTR('" + encoded + "')", nil
	case dialect.Oracle:
		if text == "" {
			return "", errors.New("Oracle empty character strings become NULL; this value cannot be copied losslessly")
		}
		// UNISTR keeps non-ASCII characters and backslashes independent of
		// the SQL client's database/national character-set conversion.
		encoded := copyUnicodeText(text)
		if len(encoded) > 4000 {
			return "", errors.New("Oracle character literal encoding exceeds the 4000-byte copier limit")
		}
		return "UNISTR('" + encoded + "')", nil
	default:
		return "", errors.New("SQL copying is not supported for this dialect")
	}
}

// UNISTR uses documented four-hex-digit escapes. Each vendor retains its own
// type, length and empty-string policies; DM/OB verify the required conversion.
func copyUnicodeText(text string) string {
	var encoded strings.Builder
	for _, unit := range utf16.Encode([]rune(text)) {
		switch {
		case unit >= 128 || unit == '\\':
			fmt.Fprintf(&encoded, "\\%04X", unit)
		case unit == '\'':
			encoded.WriteString("''")
		default:
			encoded.WriteRune(rune(unit))
		}
	}
	return encoded.String()
}

func copyTemporalLiteral(d dialect.ID, t, text string) (string, error) {
	if d.UsesMySQLSyntax() {
		// MySQL TIME is a duration, including negative values and hours above
		// 23. Preserve valid zero dates and local DATETIME values as returned.
		// Numeric server formats contain no SQL quoting or escape characters.
		if !mysqlCopyTemporal.MatchString(text) {
			return "", errors.New("The cached MySQL temporal value is invalid")
		}
		return "'" + text + "'", nil
	}
	// Only complete ISO values are accepted. BC/infinity, zero dates, named zones,
	// intervals and dialect-specific text require separate codecs.
	if d == dialect.DB2 || d == dialect.ClickHouse {
		if dot := strings.IndexByte(text, '.'); dot >= 0 {
			end := dot + 1
			for end < len(text) && text[end] >= '0' && text[end] <= '9' {
				end++
			}
			if end-dot-1 > 9 {
				return "", errors.New("Temporal precision exceeds the target dialect codec")
			}
		}
	}
	date := t == "date" || t == "date32"
	if d == dialect.Dameng && date && len(text) > len("2006-01-02") {
		// Oracle-compatible DM DATE values can retain a time of day.
		date = false
	}
	clock := t == "time" || t == "time without time zone"
	zone := t == "timestamptz" || t == "timestamp with time zone" || t == "datetimeoffset" || d == dialect.ClickHouse && !date
	layouts := []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999"}
	if date {
		layouts = []string{"2006-01-02"}
	}
	if clock {
		layouts = append([]string{"15:04:05.999999999"}, layouts...)
	}
	var parsed time.Time
	var err error
	for _, layout := range layouts {
		parsed, err = time.Parse(layout, text)
		if err == nil {
			if zone && !strings.Contains(layout, "07:00") && layout != time.RFC3339Nano {
				err = errors.New("timezone missing")
				continue
			}
			break
		}
	}
	if err != nil || !clock && (parsed.Year() < 1 || parsed.Year() > 9999) {
		return "", errors.New("This temporal value is outside the supported complete ISO formats")
	}
	if d == dialect.Dameng && clock && parsed.Nanosecond()%1000 != 0 || d == dialect.SQLServer && parsed.Nanosecond()%100 != 0 || (d.UsesMySQLSyntax()) && parsed.Nanosecond()%1000 != 0 {
		return "", errors.New("Temporal precision exceeds the target dialect codec")
	}
	output := parsed.Format("2006-01-02 15:04:05.999999999")
	if date {
		output = parsed.Format("2006-01-02")
	}
	if clock {
		output = parsed.Format("15:04:05.999999999")
	}
	if zone {
		output = parsed.Format("2006-01-02 15:04:05.999999999Z07:00")
	}
	switch d {
	case dialect.DB2:
		target := "TIMESTAMP"
		if date {
			target = "DATE"
		}
		if clock {
			target = "TIME"
		}
		return target + "('" + output + "')", nil
	case dialect.ClickHouse:
		if t == "date32" {
			return "toDate32('" + output + "')", nil
		}
		if date {
			return "toDate('" + output + "')", nil
		}
		// Explicit offsets retain the cached instant independently of the
		// target session's timezone; local timestamps keep server semantics.
		return "parseDateTime64BestEffort('" + output + "', 9)", nil
	case dialect.PostgreSQL:
		return "CAST('" + output + "' AS " + t + ")", nil
	case dialect.SQLServer:
		if !date && !clock {
			output = strings.Replace(output, " ", "T", 1)
		}
		return "CONVERT(" + t + ", N'" + output + "', 126)", nil
	case dialect.Dameng:
		target := "TIMESTAMP"
		if date {
			target = "DATE"
		}
		if clock {
			target = "TIME"
		}
		return target + " '" + output + "'", nil
	case dialect.Oracle:
		return "TIMESTAMP '" + output + "'", nil
	default:
		target := "DATETIME(6)"
		if date {
			target = "DATE"
		}
		if clock {
			target = "TIME(6)"
		}
		return "CAST('" + output + "' AS " + target + ")", nil
	}
}

// Require all date/time components; DATE output formatted by NLS may already
// have lost the time of day. Explicit numeric format models avoid NLS defaults.
func copyOracleTemporalLiteral(t, text string) (string, error) {
	if dot := strings.IndexByte(text, '.'); dot >= 0 && len(text)-dot-1 > 9 {
		return "", errors.New("Oracle temporal precision exceeds nine fractional digits")
	}
	var parsed time.Time
	var err error
	for _, layout := range []string{"2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999"} {
		parsed, err = time.Parse(layout, text)
		if err == nil {
			break
		}
	}
	if err != nil || parsed.Year() < 1 || parsed.Year() > 9999 {
		return "", errors.New("Oracle temporal copying requires a complete ISO date and time; NLS text or date-only values cannot prove the original time of day")
	}
	if t == "date" {
		if parsed.Nanosecond() != 0 {
			return "", errors.New("Oracle DATE cannot represent fractional seconds")
		}
		return "TO_DATE('" + parsed.Format("2006-01-02 15:04:05") + "', 'YYYY-MM-DD HH24:MI:SS')", nil
	}
	return "TO_TIMESTAMP('" + parsed.Format("2006-01-02 15:04:05.000000000") + "', 'YYYY-MM-DD HH24:MI:SS.FF9')", nil
}
