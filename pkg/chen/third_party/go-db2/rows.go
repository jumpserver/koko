package db2

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"io"
	"reflect"
	"time"

	"github.com/go-db2/go-db2/internal/codec"
	"github.com/go-db2/go-db2/internal/drda"
)

// resultSet represents a single tabular result set of columns and rows.
type resultSet struct {
	columns []drda.ColumnDescription
	data    [][]driver.Value
	stream  *drda.QueryStream
}

// Rows implements database/sql/driver.Rows, driver.RowsNextResultSet, and column type introspection interfaces.
type Rows struct {
	resultSets   []resultSet
	currSetIndex int
	cursor       int
	closed       bool
	onClose      func() error
}

// newRows instantiates a new driver.Rows container for a single result set.
func newRows(columns []drda.ColumnDescription, data [][]driver.Value) *Rows {
	return &Rows{
		resultSets: []resultSet{
			{columns: columns, data: data},
		},
		currSetIndex: 0,
		cursor:       0,
	}
}

// newMultiRows instantiates a new driver.Rows container for multiple result sets.
func newMultiRows(sets []resultSet) *Rows {
	if len(sets) == 0 {
		sets = []resultSet{{}}
	}
	return &Rows{
		resultSets:   sets,
		currSetIndex: 0,
		cursor:       0,
	}
}

// newStreamingRows creates a driver.Rows backed by a QueryStream and optional cleanup callback.
func newStreamingRows(columns []drda.ColumnDescription, initialData [][]driver.Value, stream *drda.QueryStream, onClose func() error) *Rows {
	return &Rows{
		resultSets: []resultSet{
			{columns: columns, data: initialData, stream: stream},
		},
		currSetIndex: 0,
		cursor:       0,
		onClose:      onClose,
	}
}

// currentSet returns a pointer to the active result set.
func (r *Rows) currentSet() *resultSet {
	if r.currSetIndex >= 0 && r.currSetIndex < len(r.resultSets) {
		return &r.resultSets[r.currSetIndex]
	}
	return &resultSet{}
}

// Columns returns the names of the columns in the current result set.
func (r *Rows) Columns() []string {
	cur := r.currentSet()
	names := make([]string, len(cur.columns))
	for i, c := range cur.columns {
		names[i] = c.Name
	}
	return names
}

// Close closes the Rows iterator and releases underlying resources.
func (r *Rows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true

	var firstErr error
	for i := range r.resultSets {
		if r.resultSets[i].stream != nil {
			if err := r.resultSets[i].stream.Close(context.Background()); err != nil && firstErr == nil {
				firstErr = err
			}
			r.resultSets[i].stream = nil
		}
	}

	if r.onClose != nil {
		if err := r.onClose(); err != nil && firstErr == nil {
			firstErr = err
		}
		r.onClose = nil
	}

	return firstErr
}

// Next satisfies the driver.Rows interface.
func (r *Rows) Next(dest []driver.Value) error {
	if r.closed {
		return io.EOF
	}

	cur := r.currentSet()

	if r.cursor >= len(cur.data) {
		if cur.stream == nil {
			return io.EOF
		}

		batch, err := cur.stream.FetchNextBatch(context.Background())
		if err != nil {
			return err
		}

		cur.data = batch
		r.cursor = 0
		if len(cur.data) == 0 {
			return io.EOF
		}
	}

	row := cur.data[r.cursor]
	r.cursor++
	for i := range dest {
		if i < len(row) {
			dest[i] = row[i]
		} else {
			dest[i] = nil
		}
	}
	return nil
}

// HasNextResultSet returns whether there are more result sets available.
func (r *Rows) HasNextResultSet() bool {
	if r.closed {
		return false
	}
	return r.currSetIndex+1 < len(r.resultSets)
}

// NextResultSet advances the iterator to the next result set.
func (r *Rows) NextResultSet() error {
	if r.closed {
		return io.EOF
	}
	if !r.HasNextResultSet() {
		return io.EOF
	}
	r.currSetIndex++
	r.cursor = 0
	return nil
}

// Pre-allocated static reflect.Type instances to eliminate reflect.TypeOf and interface boxing on every call.
var (
	scanTypeAny         = reflect.TypeOf(new(any)).Elem()
	scanTypeInt16       = reflect.TypeOf(int16(0))
	scanTypeInt32       = reflect.TypeOf(int32(0))
	scanTypeInt64       = reflect.TypeOf(int64(0))
	scanTypeFloat64     = reflect.TypeOf(float64(0))
	scanTypeTime        = reflect.TypeOf(time.Time{})
	scanTypeByteSlice   = reflect.TypeOf([]byte{})
	scanTypeBool        = reflect.TypeOf(true)
	scanTypeString      = reflect.TypeOf("")
	scanTypeNullInt64   = reflect.TypeOf((*sql.NullInt64)(nil)).Elem()
	scanTypeNullFloat64 = reflect.TypeOf((*sql.NullFloat64)(nil)).Elem()
	scanTypeNullString  = reflect.TypeOf((*sql.NullString)(nil)).Elem()
	scanTypeNullTime    = reflect.TypeOf((*sql.NullTime)(nil)).Elem()
	scanTypeNullBool    = reflect.TypeOf((*sql.NullBool)(nil)).Elem()
)

// ColumnTypeScanType returns the Go type that can be used to scan types into.
// Optimization: Returns pre-allocated package-level reflect.Type variables (0 allocs/op).
func (r *Rows) ColumnTypeScanType(index int) reflect.Type {
	cur := r.currentSet()
	if index < 0 || index >= len(cur.columns) {
		return scanTypeAny
	}

	col := cur.columns[index]
	sqlType := codec.SQLType(col.SQLType).BaseType()

	if col.Nullable {
		switch sqlType {
		case codec.SQLTypeSmall, codec.SQLTypeInteger, codec.SQLTypeBigInt:
			return scanTypeNullInt64
		case codec.SQLTypeFloat:
			return scanTypeNullFloat64
		case codec.SQLTypeDate, codec.SQLTypeTimestamp:
			return scanTypeNullTime
		case codec.SQLTypeBlob, codec.SQLTypeBinary, codec.SQLTypeVarBinary:
			return scanTypeByteSlice
		case codec.SQLTypeBoolean:
			return scanTypeNullBool
		case codec.SQLTypeChar, codec.SQLTypeVarChar, codec.SQLTypeLong, codec.SQLTypeClob,
			codec.SQLTypeGraphic, codec.SQLTypeVarGraph, codec.SQLTypeLonGraph, codec.SQLTypeDbClob,
			codec.SQLTypeXML, codec.SQLTypeDecimal, codec.SQLTypeNumeric, codec.SQLTypeDecFloat,
			codec.SQLTypeTime:
			return scanTypeNullString
		default:
			return scanTypeNullString
		}
	}

	switch sqlType {
	case codec.SQLTypeSmall:
		return scanTypeInt16
	case codec.SQLTypeInteger:
		return scanTypeInt32
	case codec.SQLTypeBigInt:
		return scanTypeInt64
	case codec.SQLTypeFloat:
		return scanTypeFloat64
	case codec.SQLTypeDate, codec.SQLTypeTimestamp:
		return scanTypeTime
	case codec.SQLTypeBlob, codec.SQLTypeBinary, codec.SQLTypeVarBinary:
		return scanTypeByteSlice
	case codec.SQLTypeBoolean:
		return scanTypeBool
	default:
		return scanTypeString
	}
}

// ColumnTypeDatabaseTypeName returns the database system type name.
func (r *Rows) ColumnTypeDatabaseTypeName(index int) string {
	cur := r.currentSet()
	if index < 0 || index >= len(cur.columns) {
		return ""
	}
	return codec.SQLType(cur.columns[index].SQLType).BaseType().String()
}

// ColumnTypeNullable returns whether the column is nullable.
func (r *Rows) ColumnTypeNullable(index int) (nullable, ok bool) {
	cur := r.currentSet()
	if index < 0 || index >= len(cur.columns) {
		return false, false
	}
	return cur.columns[index].Nullable, true
}

// ColumnTypeLength returns the column length if variable or fixed length.
func (r *Rows) ColumnTypeLength(index int) (length int64, ok bool) {
	cur := r.currentSet()
	if index < 0 || index >= len(cur.columns) {
		return 0, false
	}
	return cur.columns[index].Length, true
}

// ColumnTypePrecisionScale returns precision and scale for decimal/numeric types.
func (r *Rows) ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool) {
	cur := r.currentSet()
	if index < 0 || index >= len(cur.columns) {
		return 0, 0, false
	}
	col := cur.columns[index]
	if col.Precision == 0 && col.Scale == 0 {
		return 0, 0, false
	}
	return int64(col.Precision), int64(col.Scale), true
}

var (
	_ driver.Rows                           = (*Rows)(nil)
	_ driver.RowsNextResultSet              = (*Rows)(nil)
	_ driver.RowsColumnTypeScanType         = (*Rows)(nil)
	_ driver.RowsColumnTypeDatabaseTypeName = (*Rows)(nil)
	_ driver.RowsColumnTypeNullable         = (*Rows)(nil)
	_ driver.RowsColumnTypeLength           = (*Rows)(nil)
	_ driver.RowsColumnTypePrecisionScale   = (*Rows)(nil)
)
