package db2

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// Result implements the database/sql/driver.Result interface.
type Result struct {
	mu           sync.Mutex
	affectedRows int64
	lastInsertId int64
}

// newResult creates a new driver.Result with affected rows count and preset lastInsertId.
func newResult(affectedRows, lastInsertId int64) *Result {
	return &Result{
		affectedRows: affectedRows,
		lastInsertId: lastInsertId,
	}
}

// newResultWithConn creates a new driver.Result without retaining connection pointer.
func newResultWithConn(conn *Conn, affectedRows int64, isInsert bool) *Result {
	return &Result{
		affectedRows: affectedRows,
	}
}

// LastInsertId returns the database's auto-generated ID or an explicit error directing to DB2 idiomatic syntax.
func (r *Result) LastInsertId() (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.lastInsertId != 0 {
		return r.lastInsertId, nil
	}
	return 0, errors.New("db2: LastInsertId is not supported by Db2 DRDA protocol; use 'SELECT ... FROM FINAL TABLE (INSERT ...)' to retrieve generated identity keys")
}

// RowsAffected returns the number of rows affected by the query.
func (r *Result) RowsAffected() (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.affectedRows, nil
}

func isInsertQuery(sql string) bool {
	s := stripLeadingCommentsAndSpaces(sql)
	return hasPrefixFold(s, "INSERT")
}

func parseIdentityValue(val any) (int64, error) {
	switch v := val.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case uint64:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint:
		return int64(v), nil
	case float64:
		return int64(v), nil
	case float32:
		return int64(v), nil
	case string:
		str := strings.TrimSpace(v)
		if idx := strings.Index(str, "."); idx != -1 {
			str = str[:idx]
		}
		if str == "" {
			return 0, nil
		}
		return strconv.ParseInt(str, 10, 64)
	case []byte:
		str := strings.TrimSpace(string(v))
		if idx := strings.Index(str, "."); idx != -1 {
			str = str[:idx]
		}
		if str == "" {
			return 0, nil
		}
		return strconv.ParseInt(str, 10, 64)
	default:
		str := fmt.Sprint(val)
		if idx := strings.Index(str, "."); idx != -1 {
			str = str[:idx]
		}
		return strconv.ParseInt(str, 10, 64)
	}
}

var _ driver.Result = (*Result)(nil)
