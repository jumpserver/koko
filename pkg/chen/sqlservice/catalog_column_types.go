package sqlservice

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// A display of the catalog's declared type, not a reconstructed DDL claim.
// Unknown modifiers stay unknown; byte capacity is never substituted for an
// Oracle character length or a Db2 Unicode declaration's string-unit length.
func catalogColumnType(d Dialect, native string, length, precision, scale, charLength sql.NullInt64, units sql.NullString) string {
	name := strings.TrimSpace(native)
	kind := strings.ToUpper(name)
	if strings.Contains(kind, "(") {
		return name
	}
	if IsOracleDialect(d) {
		switch kind {
		case "NUMBER", "DECIMAL", "NUMERIC":
			if precision.Valid && precision.Int64 > 0 && scale.Valid {
				return fmt.Sprintf("%s(%d,%d)", name, precision.Int64, scale.Int64)
			}
			if kind == "NUMBER" && !precision.Valid && scale.Valid {
				return fmt.Sprintf("%s(*,%d)", name, scale.Int64)
			}
		case "FLOAT":
			if precision.Valid && precision.Int64 > 0 {
				return fmt.Sprintf("%s(%d)", name, precision.Int64)
			}
		case "CHAR", "VARCHAR", "VARCHAR2":
			switch units.String {
			case "C":
				if charLength.Valid && charLength.Int64 > 0 {
					return fmt.Sprintf("%s(%d CHAR)", name, charLength.Int64)
				}
			case "B":
				if length.Valid && length.Int64 > 0 {
					return fmt.Sprintf("%s(%d BYTE)", name, length.Int64)
				}
			}
		case "NCHAR", "NVARCHAR2":
			if charLength.Valid && charLength.Int64 > 0 {
				return fmt.Sprintf("%s(%d)", name, charLength.Int64)
			}
		case "RAW", "BINARY", "VARBINARY":
			if length.Valid && length.Int64 > 0 {
				return fmt.Sprintf("%s(%d)", name, length.Int64)
			}
		case "TIMESTAMP", "TIMESTAMP WITH TIME ZONE", "TIMESTAMP WITH LOCAL TIME ZONE":
			if scale.Valid && scale.Int64 >= 0 && scale.Int64 <= 9 {
				return strings.Replace(name, "TIMESTAMP", fmt.Sprintf("TIMESTAMP(%d)", scale.Int64), 1)
			}
		}
	}
	if d == DB2 {
		switch kind {
		case "DECIMAL", "NUMERIC":
			if length.Valid && length.Int64 > 0 && scale.Valid && scale.Int64 >= 0 {
				return fmt.Sprintf("%s(%d,%d)", name, length.Int64, scale.Int64)
			}
		case "DECFLOAT":
			if length.Valid {
				if length.Int64 == 8 {
					return name + "(16)"
				}
				if length.Int64 == 16 {
					return name + "(34)"
				}
			}
		case "TIMESTAMP":
			if scale.Valid && scale.Int64 >= 0 && scale.Int64 <= 12 {
				return fmt.Sprintf("%s(%d)", name, scale.Int64)
			}
		case "CHARACTER", "CHAR", "VARCHAR", "CHARACTER VARYING", "CLOB", "GRAPHIC", "VARGRAPHIC", "DBCLOB":
			if charLength.Valid && charLength.Int64 > 0 && units.Valid {
				switch units.String {
				case "OCTETS", "CODEUNITS16", "CODEUNITS32":
					return fmt.Sprintf("%s(%d %s)", name, charLength.Int64, units.String)
				}
			}
			// The string-unit columns are NULL for non-Unicode databases. LENGTH is
			// the declared byte limit for character strings; graphic characters use
			// two bytes. Do not take this path for a partially populated unit tuple.
			if !charLength.Valid && !units.Valid && length.Valid && length.Int64 > 0 {
				n := length.Int64
				if kind == "GRAPHIC" || kind == "VARGRAPHIC" || kind == "DBCLOB" {
					if n%2 != 0 {
						return name
					}
					n /= 2
				}
				return fmt.Sprintf("%s(%d)", name, n)
			}
		case "BINARY", "VARBINARY":
			if length.Valid && length.Int64 > 0 {
				return fmt.Sprintf("%s(%d)", name, length.Int64)
			}
		}
	}
	return name
}

// Db2 before the string-unit catalog extension still has LENGTH/SCALE. Retry
// only an undefined-column error, never authentication, timeout or I/O errors.
func catalogMissingStringUnits(err error) bool {
	var state interface{ SQLState() string }
	if errors.As(err, &state) && state.SQLState() == "42703" {
		return true
	}
	text := strings.ToUpper(err.Error())
	return strings.Contains(text, "SQLSTATE=42703") || strings.Contains(text, "SQLSTATE 42703") || strings.Contains(text, "SQL0206N")
}
