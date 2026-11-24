package engine

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	db2 "github.com/go-db2/go-db2"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
)

var db2SQLState = regexp.MustCompile(`^[0-9A-Z]{5}$`)
var db2PhysicalColumn = regexp.MustCompile(`^TBSPACEID=([0-9]{1,10}), *TABLEID=([0-9]{1,10}), *COLNO=([0-9]{1,10})$`)

// SQLERRMC and encoding errors can contain actual field values. Expose only
// codes, verified column names and the documented numeric tokens below.
func db2EditDiagnostic(err error, argColumns []string, table *sqlservice.EditTable) *APIError {
	var parameter interface{ ParameterIndex() int }
	if errors.As(err, &parameter) {
		index := parameter.ParameterIndex()
		if index > 0 && index <= len(argColumns) {
			return editError("invalid_changes", fmt.Sprintf("Column %s: A data type or format is invalid. Check the column type and input value.", argColumns[index-1]))
		}
		return nil
	}
	var native *db2.Error
	if !errors.As(err, &native) || db2UncertainNativeError(err) {
		return nil
	}
	code := "DB2_SQL_ERROR"
	message := fmt.Sprintf("Db2 SQLCODE=%d", native.SQLCode)
	if db2SQLState.MatchString(native.SQLState) {
		code = native.SQLState
		message += ", SQLSTATE=" + code
	}
	token := strings.TrimSpace(native.Message)
	switch {
	case native.SQLCode == -301 && code == "07006", native.SQLCode == -311 && code == "22501":
		// Both messages identify a one-based input parameter.
		index, err := strconv.Atoi(token)
		if err == nil && index > 0 && index <= len(argColumns) && token == strconv.Itoa(index) {
			message += fmt.Sprintf(", column %q (parameter %d)", argColumns[index-1], index)
		}
	case native.SQLCode == -407 && code == "23502", native.SQLCode == -408 && code == "42821":
		message += db2EditColumnDetail(token, native.SQLCode == -407, table)
	}
	return editError(code, message)
}

func db2EditColumnDetail(token string, nullAssignment bool, table *sqlservice.EditTable) string {
	if nullAssignment {
		if parts := db2PhysicalColumn.FindStringSubmatch(token); parts != nil {
			// All three captures are decimal integers of at most ten digits.
			var id sqlservice.DB2ColumnID
			id.Tablespace, _ = strconv.ParseInt(parts[1], 10, 64)
			id.Table, _ = strconv.ParseInt(parts[2], 10, 64)
			id.Column, _ = strconv.ParseInt(parts[3], 10, 64)
			if table != nil {
				if name, ok := table.DB2ColumnNames[id]; ok {
					return fmt.Sprintf(", column %q", name)
				}
			}
			// A trigger can fail in another table, even at the same COLNO.
			return fmt.Sprintf(", TBSPACEID=%d, TABLEID=%d, COLNO=%d", id.Tablespace, id.Table, id.Column)
		}
	}
	if table != nil {
		for _, column := range table.Columns {
			name := quoteDatabaseIdentifier(column.Name, "db2")
			if token == column.Name || token == name || token == table.Schema+"."+table.Table+"."+column.Name ||
				token == editQualified(table.Schema, table.Table, "db2")+"."+name {
				return fmt.Sprintf(", column %q", column.Name)
			}
		}
	}
	return ""
}
