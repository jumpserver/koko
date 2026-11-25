package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jumpserver/koko/pkg/chen/dialect"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

var editNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
var editIntegerParts = regexp.MustCompile(`^([+-]?)([0-9]*)(?:\.([0-9]*))?(?:[eE]([+-]?[0-9]+))?$`)

// Only values are parameters. All identifiers and primary key predicates originate
// from authenticated result provenance plus the current server catalog.
func buildEditPlan(d dialect.ID, cid string, r *Result, x *editResolved, req editPreviewRequest) (*editPlan, error) {
	if len(req.Changes) == 0 || len(req.Changes) > 500 {
		return nil, errors.New("A save must contain between 1 and 500 row changes")
	}
	if req.Source == "csv" && x.view.Mode != "table" {
		return nil, errors.New("CSV import requires a table browse result")
	}
	p := &editPlan{id: newID("edit_"), consoleID: cid, resultID: r.ID, source: req.Source, count: len(req.Changes), expires: time.Now().Add(15 * time.Minute)}
	p.schemaFingerprint = editSchemaFingerprint(x)
	refs := map[string]int{}
	for i, ref := range r.rowRefs {
		refs[ref] = i
	}
	seenRows := map[string]bool{}
	target := editQualifiedTable(x.table, d)
	// Preserve Java's operation order. Deletions can free a primary/unique
	// key that an insertion in the same save reuses.
	changes := slices.Clone(req.Changes)
	rank := map[string]int{"delete": 0, "update": 1, "insert": 2}
	slices.SortStableFunc(changes, func(a, b editChange) int { return rank[a.Op] - rank[b.Op] })
	for _, change := range changes {
		if change.Op != "insert" && change.Op != "update" && change.Op != "delete" {
			return nil, errors.New("Unknown row operation")
		}
		if change.Op != "update" && x.view.Mode != "table" {
			return nil, errors.New("Only table browse results support adding or deleting rows")
		}
		if req.Source == "csv" && change.Op != "insert" {
			return nil, errors.New("CSV import only appends new rows")
		}
		row := -1
		if change.Op != "insert" {
			var ok bool
			row, ok = refs[change.RowRef]
			if !ok || seenRows[change.RowRef] {
				return nil, errors.New("A row reference is invalid or appears more than once")
			}
			seenRows[change.RowRef] = true
			if r.original[row][x.pk] == nil {
				return nil, errors.New("A primary key value is unavailable")
			}
		} else if change.RowRef != "" {
			return nil, errors.New("New rows must not have an existing row reference")
		}
		if change.Op == "delete" && len(change.Values) > 0 {
			return nil, errors.New("Deleting a row does not accept new column values")
		}
		if change.Op == "update" && len(change.Values) == 0 {
			return nil, errors.New("An update must change at least one column")
		}
		indexes := []int{}
		for key := range change.Values {
			i, err := strconv.Atoi(key)
			if err != nil || strconv.Itoa(i) != key || i < 0 || i >= len(x.columns) {
				return nil, errors.New("A column index is invalid")
			}
			indexes = append(indexes, i)
		}
		sort.Ints(indexes)
		cmd := editCommand{op: change.Op}
		assign, policyAssign, previewAssign := []string{}, []string{}, []string{}
		colNames, placeholders, policyValues, previewValues := []string{}, []string{}, []string{}, []string{}
		for _, index := range indexes {
			field, cv := x.columns[index], x.view.Columns[index]
			if change.Op == "update" && !cv.Editable || change.Op == "insert" && !cv.Insertable {
				return nil, fmt.Errorf("Column %s is read-only for this operation", cv.Name)
			}
			value, err := convertEditValue(field, change.Values[strconv.Itoa(index)])
			if err != nil {
				return nil, fmt.Errorf("Column %s: %s", cv.Name, err)
			}
			value, err = normalizeTypedEditValue(d, field, value)
			if err != nil {
				return nil, fmt.Errorf("Column %s: %s", cv.Name, err)
			}
			name := quoteDatabaseIdentifier(field.Name, d)
			cmd.args = append(cmd.args, editDriverBindValue(d, field, value))
			cmd.argColumns = append(cmd.argColumns, field.Name)
			placeholder := editTypedExpression(d, field, editPlaceholder(d, len(cmd.args)))
			literal := editTypedLiteral(d, field, value, false)
			assign = append(assign, name+" = "+placeholder)
			policyAssign = append(policyAssign, name+" = "+literal)
			previewAssign = append(previewAssign, name+" = "+literal)
			colNames = append(colNames, name)
			placeholders = append(placeholders, placeholder)
			policyValues = append(policyValues, literal)
			previewValues = append(previewValues, literal)
		}
		if change.Op == "insert" {
			for i, field := range x.columns {
				if _, present := change.Values[strconv.Itoa(i)]; !present && !field.Nullable && !field.Generated && !field.Identity && !field.HasDefault {
					return nil, fmt.Errorf("Column %s requires a value", x.view.Columns[i].Name)
				}
			}
			if len(indexes) == 0 {
				switch d {
				case dialect.DB2:
					// Db2 requires DEFAULT inside VALUES. The required-column
					// check above proves the primary key can be generated or defaulted.
					key := quoteDatabaseIdentifier(x.columns[x.pk].Name, d)
					cmd.sql = "INSERT INTO " + target + " (" + key + ") VALUES (DEFAULT)"
				case dialect.MySQL, dialect.MariaDB, dialect.OceanBaseMySQL:
					cmd.sql = "INSERT INTO " + target + " () VALUES ()"
				case dialect.Oracle, dialect.Dameng, dialect.OceanBaseOracle:
					return nil, errors.New("Provide at least one writable column for this database")
				default:
					cmd.sql = "INSERT INTO " + target + " DEFAULT VALUES"
				}
				cmd.policySQL, cmd.previewSQL = cmd.sql, cmd.sql
			} else {
				prefix := "INSERT INTO " + target + " (" + strings.Join(colNames, ", ") + ") VALUES ("
				cmd.sql = prefix + strings.Join(placeholders, ", ") + ")"
				cmd.policySQL = prefix + strings.Join(policyValues, ", ") + ")"
				cmd.previewSQL = prefix + strings.Join(previewValues, ", ") + ")"
			}
			p.inserts++
		} else {
			field := x.columns[x.pk]
			name := quoteDatabaseIdentifier(field.Name, d)
			value, err := normalizeTypedEditValue(d, field, r.original[row][x.pk])
			if err != nil {
				return nil, fmt.Errorf("Column %s primary key value: %s", field.Name, err)
			}
			cmd.args = append(cmd.args, editDriverBindValue(d, field, value))
			cmd.argColumns = append(cmd.argColumns, field.Name)
			where := editTypedEquality(d, field, name, editPlaceholder(d, len(cmd.args)))
			policyValue := value
			if d == dialect.Oracle && editOracleJSON(d, field) {
				policyValue = editASCIIJSON(value.(string))
			}
			policyWhere := editTypedEquality(d, field, name, editMaskSQL(d, policyValue, false))
			masked := row < len(r.masked) && x.pk < len(r.masked[row]) && r.masked[row][x.pk]
			previewWhere := editTypedEquality(d, field, name, editMaskSQL(d, policyValue, masked))
			if change.Op == "update" {
				cmd.sql = "UPDATE " + target + " SET " + strings.Join(assign, ", ")
				cmd.policySQL = "UPDATE " + target + " SET " + strings.Join(policyAssign, ", ")
				cmd.previewSQL = "UPDATE " + target + " SET " + strings.Join(previewAssign, ", ")
				p.updates++
			} else {
				cmd.sql = "DELETE FROM " + target
				cmd.policySQL = cmd.sql
				cmd.previewSQL = cmd.sql
				p.deletes++
			}
			cmd.sql += " WHERE " + where
			cmd.policySQL += " WHERE " + policyWhere
			cmd.previewSQL += " WHERE " + previewWhere
		}
		p.commands = append(p.commands, cmd)
	}
	lines := []string{}
	for _, cmd := range p.commands {
		lines = append(lines, cmd.previewSQL+";")
	}
	p.previewSQL = strings.Join(lines, "\n")
	return p, nil
}
func convertEditValue(field sqlservice.EditColumn, value *string) (any, error) {
	if value == nil {
		if !field.Nullable {
			return nil, errors.New("NULL is not allowed")
		}
		return nil, nil
	}
	if !utf8.ValidString(*value) || strings.ContainsRune(*value, 0) {
		return nil, errors.New("Value must be valid text without NUL")
	}
	// Classify the type, not ENUM/SET member text such as 'INT' or 'JSON'.
	t := editBaseType(field.DataType)
	v := *value
	if strings.Contains(t, "BOOL") || t == "BIT" {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "t", "1", "yes", "y":
			return true, nil
		case "false", "f", "0", "no", "n":
			return false, nil
		default:
			return nil, errors.New("Enter true, false, 1 or 0")
		}
	}
	if strings.Contains(t, "INT") && !strings.Contains(t, "INTERVAL") {
		integer, err := exactEditInteger(v)
		if err != nil {
			return nil, errors.New("Enter a whole number without separators")
		}
		v = integer
	}
	if strings.Contains(t, "DECIMAL") || strings.Contains(t, "NUMERIC") || strings.Contains(t, "NUMBER") || strings.Contains(t, "FLOAT") || strings.Contains(t, "DOUBLE") || t == "REAL" || t == "MONEY" || t == "SMALLMONEY" {
		if !editNumber.MatchString(v) {
			return nil, errors.New("Enter a valid number without separators")
		}
	}
	if strings.Contains(t, "JSON") && !json.Valid([]byte(v)) {
		return nil, errors.New("Enter valid JSON")
	}
	if strings.Contains(t, "FLOAT") || strings.Contains(t, "DOUBLE") || t == "REAL" {
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
			return nil, errors.New("Enter a finite number")
		}
	}
	return v, nil
}

// Java accepts exact integral decimal/exponent input and rejects overflow.
// Normalize without floating-point rounding or allocating an exponent-sized
// integer: no supported SQL integer has more than 20 decimal digits.
func exactEditInteger(value string) (string, error) {
	parts := editIntegerParts.FindStringSubmatch(value)
	if parts == nil || parts[2] == "" && parts[3] == "" {
		return "", errors.New("invalid integer")
	}
	digits := strings.TrimLeft(parts[2]+parts[3], "0")
	if digits == "" {
		return "0", nil
	}
	exponent := 0
	if parts[4] != "" {
		var err error
		exponent, err = strconv.Atoi(parts[4])
		if err != nil || exponent > len(value)+20 || exponent < -len(value)-20 {
			return "", errors.New("integer out of range")
		}
	}
	scale := exponent - len(parts[3])
	if scale < 0 {
		if -scale >= len(digits) || strings.Trim(digits[len(digits)+scale:], "0") != "" {
			return "", errors.New("fractional integer")
		}
		digits = digits[:len(digits)+scale]
	} else {
		if len(digits)+scale > 20 {
			return "", errors.New("integer out of range")
		}
		digits += strings.Repeat("0", scale)
	}
	if len(digits) > 20 {
		return "", errors.New("integer out of range")
	}
	if parts[1] == "-" {
		digits = "-" + digits
	}
	return digits, nil
}
