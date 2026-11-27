package handler

import (
	"database/sql"
	"slices"
	"strings"

	"github.com/jumpserver/koko/pkg/dbkit/masking"
)

// WarpRows 是对 sql.Rows 的包装，支持按列索引脱敏
type WarpRows struct {
	rows          *sql.Rows
	maskIndexes   []int
	temp          []interface{}
	dataMaskRules map[int]masking.Transform
	policy        masking.Policy
	planErr       error
}

// NewWarpRows 构造函数
func NewWarpRows(rows *sql.Rows, maskIndexes []int) *WarpRows {
	return &WarpRows{
		rows:        rows,
		maskIndexes: maskIndexes,
	}
}

func rowsWithPolicy(rows *sql.Rows, policy masking.Policy) (*WarpRows, error) {
	w := &WarpRows{rows: rows, policy: policy}
	return w, w.bindColumns()
}

// Each result set has its own column order. Never reuse decisions by ordinal
// across a stored procedure's results, even when their widths happen to match.
func (w *WarpRows) bindColumns() error {
	w.temp = nil
	if !w.policy.Enabled() {
		return nil
	}
	names, err := w.rows.Columns()
	if err != nil {
		return err
	}
	w.dataMaskRules = make(map[int]masking.Transform)
	for i, name := range names {
		// Preserve the terminal matcher's Unicode lowercase and final-LF anchor.
		if m, ok := w.policy.Match(strings.ToLower(strings.TrimSuffix(name, "\n"))); ok {
			w.dataMaskRules[i] = m
		}
	}
	return nil
}

// Next 代理
func (w *WarpRows) Next() bool {
	return w.rows.Next()
}

// Scan 对行进行读取并按规则脱敏
func (w *WarpRows) Scan(dest ...interface{}) error {
	// 初始化 temp 缓存
	if len(w.temp) != len(dest) {
		w.temp = make([]interface{}, len(dest))
		for i := range w.temp {
			w.temp[i] = new(interface{})
		}
	}

	// 先 scan 到 temp
	if err := w.rows.Scan(w.temp...); err != nil {
		return err
	}

	// 遍历每一列，赋值到用户传入的 dest
	for i := range dest {
		src := *w.temp[i].(*interface{})
		if rule, ok := w.dataMaskRules[i]; ok && src != nil {
			switch v := src.(type) {
			case []byte:
				dest[i] = rule.Text(string(v))
			case string:
				dest[i] = rule.Text(v)
			case *string:
				if v != nil {
					dest[i] = rule.Text(*v)
				} else {
					dest[i] = rule.Pattern
				}
			case *sql.NullString:
				if v != nil && v.Valid {
					dest[i] = rule.Text(v.String)
				} else {
					dest[i] = rule.Pattern
				}
			default:
				dest[i] = rule.Pattern
			}
		} else if slices.Contains(w.maskIndexes, i) && !ok {
			// Preserve the legacy NewWarpRows constructor's unassigned cells.
			continue
		} else if src == nil {
			dest[i] = "NULL"
		} else {
			dest[i] = src
		}
	}
	return nil
}

// Columns 代理
func (w *WarpRows) Columns() ([]string, error) {
	return w.rows.Columns()
}

// ColumnTypes 代理
func (w *WarpRows) ColumnTypes() ([]*sql.ColumnType, error) {
	return w.rows.ColumnTypes()
}

// Close 代理
func (w *WarpRows) Close() error {
	return w.rows.Close()
}

// Err 代理
func (w *WarpRows) Err() error {
	if w.planErr != nil {
		return w.planErr
	}
	return w.rows.Err()
}

// NextResultSet 代理
func (w *WarpRows) NextResultSet() bool {
	if w.planErr != nil || !w.rows.NextResultSet() {
		return false
	}
	w.planErr = w.bindColumns()
	return w.planErr == nil
}
