package engine

import (
	"errors"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	dm "gitee.com/chunanyong/dm"
	"github.com/jumpserver/koko/pkg/chen/sqlservice"
	"github.com/shopspring/decimal"
	goora "github.com/sijms/go-ora/v2"
)

// Materialize driver-owned locators while rows and their physical connection
// are still open. A value exceeding the result budget is rejected explicitly;
// no partial LOB is presented as a complete cell.
func materializeValue(value any, dbType string, limit int, dialect string) (any, error) {
	if limit <= 0 {
		limit = int(^uint(0) >> 1)
	}
	// Oracle-family DATE may include time of day. Preserve every supplied
	// component in the grid, cached primary keys, SQL copies and full exports.
	if calendar, ok := value.(time.Time); ok && sqlservice.IsOracleDialect(sqlservice.Dialect(dialect)) && strings.EqualFold(dbType, "DATE") {
		if dialect != "dameng" || calendar.Hour() != 0 || calendar.Minute() != 0 || calendar.Second() != 0 || calendar.Nanosecond() != 0 {
			return calendar.Format("2006-01-02 15:04:05.999999999"), nil
		}
	}
	switch x := value.(type) {
	case *dm.DmClob:
		if x == nil {
			return nil, nil
		}
		n, err := x.GetLength()
		if err != nil {
			return nil, err
		}
		if n < 0 || n > int64(limit) {
			return nil, errors.New("large value exceeds result memory limit")
		}
		if n == 0 {
			return "", nil
		}
		text, err := x.ReadString(1, int(n))
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if len(text) > limit {
			return nil, errors.New("large value exceeds result memory limit")
		}
		return text, nil
	case *dm.DmBlob:
		if x == nil {
			return nil, nil
		}
		n, err := x.GetLength()
		if err != nil {
			return nil, err
		}
		if n < 0 || n > int64(limit/2) {
			return nil, errors.New("large value exceeds result memory limit")
		}
		data, err := io.ReadAll(io.LimitReader(x, int64(limit/2+1)))
		if err != nil {
			return nil, err
		}
		if len(data) > limit/2 {
			return nil, errors.New("large value exceeds result memory limit")
		}
		return normalizeValue(data, "BLOB"), nil
	case goora.Clob:
		if !x.Valid {
			return nil, nil
		}
		return x.String, nil
	case goora.NClob:
		if !x.Valid {
			return nil, nil
		}
		return x.String, nil
	case goora.Blob:
		// go-ora deprecated Blob.Valid and constructs fetched values using
		// Data alone. A non-nil empty slice remains a valid empty BLOB.
		if !x.Valid && x.Data == nil {
			return nil, nil
		}
		if x.Data == nil {
			x.Data = []byte{}
		}
		return normalizeValue(x.Data, "BLOB"), nil
	}
	return normalizeValue(value, dbType), nil
}

func normalizeComposite(value any, dbType string) any {
	switch x := value.(type) {
	case decimal.Decimal:
		return x.String()
	case *big.Int:
		if x == nil {
			return nil
		}
		return x.String()
	case big.Int:
		return x.String()
	case *big.Float:
		if x == nil {
			return nil
		}
		return x.Text('g', -1)
	case fmt.Stringer:
		return x.String()
	}
	v := reflect.ValueOf(value)
	if !v.IsValid() {
		return nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return normalizeValue(v.Elem().Interface(), dbType)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		return fmt.Sprint(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return fmt.Sprint(value)
	case reflect.Float32:
		return normalizeValue(float64(v.Float()), dbType)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		result := make([]any, v.Len())
		for i := range result {
			result[i] = normalizeValue(v.Index(i).Interface(), "")
		}
		return result
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		result := map[string]any{}
		for it := v.MapRange(); it.Next(); {
			result[fmt.Sprint(it.Key().Interface())] = normalizeValue(it.Value().Interface(), "")
		}
		return result
	default:
		return value
	}
}

func validTextBytes(data []byte) bool { return utf8.Valid(data) }
