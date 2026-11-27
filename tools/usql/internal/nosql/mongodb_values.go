package nosql

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// mongoBSON is opaque to JavaScript. Export preserves the original BSON type
// without exposing Go reflection or converting 64-bit integers to JS numbers.
type mongoBSON struct {
	s     *mongoSession
	value any
	kind  string
}

func (v *mongoBSON) Has(key string) bool       { return v.Get(key) != nil }
func (*mongoBSON) Set(string, goja.Value) bool { return false }
func (*mongoBSON) Delete(string) bool          { return false }
func (*mongoBSON) Keys() []string              { return []string{"_bsontype"} }
func (v *mongoBSON) Get(key string) goja.Value {
	s := v.s
	switch key {
	case "_bsontype":
		return s.vm.ToValue(v.kind)
	case "str":
		if id, ok := v.value.(bson.ObjectID); ok {
			return s.vm.ToValue(id.Hex())
		}
	case "pattern", "options":
		if regex, ok := v.value.(bson.Regex); ok {
			if key == "pattern" {
				return s.vm.ToValue(regex.Pattern)
			}
			return s.vm.ToValue(regex.Options)
		}
	case "toString", "toHexString":
		return s.function(func(goja.FunctionCall) goja.Value { return s.vm.ToValue(v.text()) })
	case "toJSON":
		return s.function(func(goja.FunctionCall) goja.Value {
			raw, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: v.value}}, true, false)
			s.must(err)
			var doc map[string]any
			s.must(json.Unmarshal(raw, &doc))
			return s.vm.ToValue(doc["v"])
		})
	case "valueOf", "toNumber":
		return s.function(func(goja.FunctionCall) goja.Value {
			switch n := v.value.(type) {
			case int64:
				return s.vm.ToValue(float64(n))
			case int32:
				return s.vm.ToValue(n)
			case float64:
				return s.vm.ToValue(n)
			}
			return s.vm.ToValue(v.text())
		})
	case "equals":
		return s.function(func(call goja.FunctionCall) goja.Value {
			return s.vm.ToValue(reflect.DeepEqual(v.value, s.toBSON(call.Argument(0))))
		})
	case "getTimestamp":
		if id, ok := v.value.(bson.ObjectID); ok {
			return s.function(func(goja.FunctionCall) goja.Value { return s.fromBSON(bson.NewDateTimeFromTime(id.Timestamp())) })
		}
	case "base64":
		if binary, ok := v.value.(bson.Binary); ok {
			return s.function(func(goja.FunctionCall) goja.Value {
				return s.vm.ToValue(base64.StdEncoding.EncodeToString(binary.Data))
			})
		}
	}
	return nil
}
func (v *mongoBSON) text() string {
	switch value := v.value.(type) {
	case bson.ObjectID:
		return value.Hex()
	case int64:
		return strconv.FormatInt(value, 10)
	case int32:
		return strconv.FormatInt(int64(value), 10)
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64)
	case bson.Decimal128:
		return value.String()
	case bson.Binary:
		return base64.StdEncoding.EncodeToString(value.Data)
	case bson.Timestamp:
		return fmt.Sprintf("Timestamp({t:%d,i:%d})", value.T, value.I)
	case bson.Regex:
		return "/" + value.Pattern + "/" + value.Options
	}
	return v.kind
}

func (s *mongoSession) wrapBSON(value any, kind string) goja.Value {
	return s.vm.NewDynamicObject(&mongoBSON{s: s, value: value, kind: kind})
}
func (s *mongoSession) must(err error) {
	if err != nil {
		object := s.vm.NewGoError(err)
		var labeler interface{ HasErrorLabel(string) bool }
		if errors.As(err, &labeler) {
			_ = object.Set("hasErrorLabel", func(call goja.FunctionCall) goja.Value {
				return s.vm.ToValue(labeler.HasErrorLabel(call.Argument(0).String()))
			})
		}
		var codes interface{ ErrorCodes() []int }
		if errors.As(err, &codes) {
			if values := codes.ErrorCodes(); len(values) > 0 {
				_ = object.Set("code", values[0])
			}
		}
		var commandError mongo.CommandError
		if errors.As(err, &commandError) {
			_ = object.Set("codeName", commandError.Name)
		}
		var writeError mongo.WriteException
		if errors.As(err, &writeError) && len(writeError.Raw) > 0 {
			var result bson.D
			if bson.Unmarshal(writeError.Raw, &result) == nil {
				_ = object.Set("result", s.fromBSON(result))
			}
		}
		panic(object)
	}
}
func (s *mongoSession) function(fn func(goja.FunctionCall) goja.Value) goja.Value {
	return s.vm.ToValue(fn)
}
func (s *mongoSession) toBSON(value goja.Value) any {
	return s.convertBSON(value, make(map[*goja.Object]bool), 0)
}
func (s *mongoSession) convertBSON(value goja.Value, seen map[*goja.Object]bool, depth int) any {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil
	}
	if depth > 100 {
		panic(s.vm.NewTypeError("BSON nesting exceeds 100 levels"))
	}
	if object, ok := value.(*goja.Object); ok {
		if object.ExportType() == reflect.TypeOf((*mongoBSON)(nil)) {
			return object.Export().(*mongoBSON).value
		}
		if seen[object] {
			panic(s.vm.NewTypeError("cyclic objects cannot be encoded as BSON"))
		}
		seen[object] = true
		defer delete(seen, object)
		switch object.ClassName() {
		case "Date":
			fn, _ := goja.AssertFunction(object.Get("getTime"))
			ms, err := fn(object)
			s.must(err)
			if math.IsNaN(ms.ToFloat()) {
				panic(s.vm.NewTypeError("invalid Date"))
			}
			return bson.DateTime(ms.ToInteger())
		case "RegExp":
			flags := ""
			for _, flag := range object.Get("flags").String() {
				if strings.ContainsRune("imsu", flag) {
					flags += string(flag)
				}
			}
			return bson.Regex{Pattern: object.Get("source").String(), Options: flags}
		case "Array":
			length := object.Get("length").ToInteger()
			if length > 1_000_000 {
				panic(s.vm.NewTypeError("BSON array exceeds one million elements"))
			}
			items := make(bson.A, int(length))
			for i := range items {
				items[i] = s.convertBSON(object.Get(strconv.Itoa(i)), seen, depth+1)
			}
			return items
		case "Function":
			return bson.JavaScript(object.String())
		}
		if object.ExportType() == reflect.TypeOf((*mongoCursor)(nil)) {
			panic(s.vm.NewTypeError("call toArray() before encoding a cursor"))
		}
		if object.ExportType() == reflect.TypeOf((*mongoHandle)(nil)) {
			panic(s.vm.NewTypeError("MongoDB handles cannot be encoded as documents"))
		}
		document := bson.D{}
		for _, key := range object.Keys() {
			document = append(document, bson.E{Key: key, Value: s.convertBSON(object.Get(key), seen, depth+1)})
		}
		return document
	}
	switch value := value.Export().(type) {
	case int64:
		if value >= math.MinInt32 && value <= math.MaxInt32 {
			return int32(value)
		}
		return float64(value)
	case *big.Int:
		if !value.IsInt64() {
			panic(s.vm.NewTypeError("BigInt is outside BSON int64 range"))
		}
		return value.Int64()
	default:
		return value
	}
}
func (s *mongoSession) document(value goja.Value) bson.D {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return bson.D{}
	}
	doc, ok := s.toBSON(value).(bson.D)
	if !ok {
		panic(s.vm.NewTypeError("expected a document"))
	}
	return doc
}
func (s *mongoSession) fromBSON(value any) goja.Value {
	switch value := value.(type) {
	case nil:
		return goja.Null()
	case bson.D:
		object := s.vm.NewObject()
		for _, element := range value {
			s.must(object.DefineDataProperty(element.Key, s.fromBSON(element.Value), goja.FLAG_TRUE, goja.FLAG_TRUE, goja.FLAG_TRUE))
		}
		return object
	case bson.M:
		doc := bson.D{}
		for k, v := range value {
			doc = append(doc, bson.E{Key: k, Value: v})
		}
		return s.fromBSON(doc)
	case bson.A:
		items := make([]any, len(value))
		for i, v := range value {
			items[i] = s.fromBSON(v)
		}
		return s.vm.NewArray(items...)
	case []any:
		return s.fromBSON(bson.A(value))
	case []string:
		items := make(bson.A, len(value))
		for i, v := range value {
			items[i] = v
		}
		return s.fromBSON(items)
	case int64:
		return s.wrapBSON(value, "Long")
	case bson.ObjectID:
		return s.wrapBSON(value, "ObjectId")
	case bson.Decimal128:
		return s.wrapBSON(value, "Decimal128")
	case bson.Binary:
		return s.wrapBSON(value, "Binary")
	case bson.Timestamp:
		return s.wrapBSON(value, "Timestamp")
	case bson.MinKey:
		return s.wrapBSON(value, "MinKey")
	case bson.MaxKey:
		return s.wrapBSON(value, "MaxKey")
	case bson.DateTime:
		object, err := s.vm.New(s.vm.Get("Date"), s.vm.ToValue(int64(value)))
		s.must(err)
		return object
	case time.Time:
		return s.fromBSON(bson.NewDateTimeFromTime(value))
	case bson.Regex:
		return s.wrapBSON(value, "BSONRegExp")
	default:
		return s.vm.ToValue(value)
	}
}

func (s *mongoSession) initBSON() {
	construct := func(name string, fn func(goja.FunctionCall) goja.Value) {
		s.must(s.vm.Set(name, func(call goja.ConstructorCall) *goja.Object {
			return fn(goja.FunctionCall{Arguments: call.Arguments}).ToObject(s.vm)
		}))
	}
	construct("ObjectId", func(call goja.FunctionCall) goja.Value {
		value := bson.NewObjectID()
		if !goja.IsUndefined(call.Argument(0)) {
			var err error
			value, err = bson.ObjectIDFromHex(call.Argument(0).String())
			s.must(err)
		}
		return s.wrapBSON(value, "ObjectId")
	})
	for _, name := range []string{"NumberLong", "Long"} {
		construct(name, func(call goja.FunctionCall) goja.Value {
			text := "0"
			if !goja.IsUndefined(call.Argument(0)) {
				text = call.Argument(0).String()
			}
			value, err := strconv.ParseInt(text, 10, 64)
			s.must(err)
			return s.wrapBSON(value, "Long")
		})
	}
	for _, name := range []string{"NumberInt", "Int32"} {
		construct(name, func(call goja.FunctionCall) goja.Value {
			text := "0"
			if !goja.IsUndefined(call.Argument(0)) {
				text = call.Argument(0).String()
			}
			value, err := strconv.ParseInt(text, 10, 32)
			s.must(err)
			return s.wrapBSON(int32(value), "Int32")
		})
	}
	for _, name := range []string{"NumberDecimal", "Decimal128"} {
		construct(name, func(call goja.FunctionCall) goja.Value {
			text := "0"
			if !goja.IsUndefined(call.Argument(0)) {
				text = call.Argument(0).String()
			}
			value, err := bson.ParseDecimal128(text)
			s.must(err)
			return s.wrapBSON(value, "Decimal128")
		})
	}
	construct("Double", func(call goja.FunctionCall) goja.Value {
		value := float64(0)
		if !goja.IsUndefined(call.Argument(0)) {
			value = call.Argument(0).ToFloat()
		}
		return s.wrapBSON(value, "Double")
	})
	construct("BSONRegExp", func(call goja.FunctionCall) goja.Value {
		pattern := call.Argument(0).String()
		opts := ""
		if !goja.IsUndefined(call.Argument(1)) {
			opts = call.Argument(1).String()
		}
		for _, flag := range opts {
			if !strings.ContainsRune("imsux", flag) {
				panic(s.vm.NewTypeError("invalid BSON regex option"))
			}
		}
		return s.wrapBSON(bson.Regex{Pattern: pattern, Options: opts}, "BSONRegExp")
	})
	construct("BinData", func(call goja.FunctionCall) goja.Value {
		kind := call.Argument(0).ToInteger()
		if kind < 0 || kind > 255 {
			panic(s.vm.NewTypeError("binary subtype must be 0..255"))
		}
		data, err := base64.StdEncoding.DecodeString(call.Argument(1).String())
		s.must(err)
		return s.wrapBSON(bson.Binary{Subtype: byte(kind), Data: data}, "Binary")
	})
	construct("UUID", func(call goja.FunctionCall) goja.Value {
		id := uuid.New()
		if !goja.IsUndefined(call.Argument(0)) {
			var err error
			id, err = uuid.Parse(call.Argument(0).String())
			s.must(err)
		}
		return s.wrapBSON(bson.Binary{Subtype: 4, Data: id[:]}, "Binary")
	})
	construct("Timestamp", func(call goja.FunctionCall) goja.Value {
		var t, i int64
		if object, ok := call.Argument(0).(*goja.Object); ok {
			if v := object.Get("t"); v != nil {
				t = v.ToInteger()
			}
			if v := object.Get("i"); v != nil {
				i = v.ToInteger()
			}
		} else if !goja.IsUndefined(call.Argument(0)) {
			t = call.Argument(0).ToInteger()
			i = call.Argument(1).ToInteger()
		}
		if t < 0 || t > math.MaxUint32 || i < 0 || i > math.MaxUint32 {
			panic(s.vm.NewTypeError("Timestamp fields must be uint32"))
		}
		return s.wrapBSON(bson.Timestamp{T: uint32(t), I: uint32(i)}, "Timestamp")
	})
	construct("MinKey", func(goja.FunctionCall) goja.Value { return s.wrapBSON(bson.MinKey{}, "MinKey") })
	construct("MaxKey", func(goja.FunctionCall) goja.Value { return s.wrapBSON(bson.MaxKey{}, "MaxKey") })
	s.must(s.vm.Set("ISODate", s.function(func(call goja.FunctionCall) goja.Value {
		args := []goja.Value{}
		if !goja.IsUndefined(call.Argument(0)) {
			args = append(args, call.Argument(0))
		}
		date, err := s.vm.New(s.vm.Get("Date"), args...)
		s.must(err)
		return date
	})))
	ejson := s.vm.NewObject()
	_ = ejson.Set("stringify", func(call goja.FunctionCall) goja.Value {
		value := s.toBSON(call.Argument(0))
		canonical := true
		if o, ok := call.Argument(3).(*goja.Object); ok && o.Get("relaxed") != nil {
			canonical = !o.Get("relaxed").ToBoolean()
		}
		raw, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: value}}, canonical, false)
		s.must(err)
		return s.vm.ToValue(string(raw[len(`{"v":`) : len(raw)-1]))
	})
	_ = ejson.Set("parse", func(call goja.FunctionCall) goja.Value {
		var doc bson.D
		s.must(bson.UnmarshalExtJSON([]byte(`{"v":`+call.Argument(0).String()+`}`), false, &doc))
		return s.fromBSON(doc[0].Value)
	})
	_ = ejson.Set("serialize", func(call goja.FunctionCall) goja.Value {
		raw, err := bson.MarshalExtJSON(bson.D{{Key: "v", Value: s.toBSON(call.Argument(0))}}, true, false)
		s.must(err)
		var doc map[string]any
		s.must(json.Unmarshal(raw, &doc))
		return s.vm.ToValue(doc["v"])
	})
	_ = ejson.Set("deserialize", func(call goja.FunctionCall) goja.Value {
		raw, err := json.Marshal(call.Argument(0).Export())
		s.must(err)
		var doc bson.D
		s.must(bson.UnmarshalExtJSON(append(append([]byte(`{"v":`), raw...), '}'), false, &doc))
		return s.fromBSON(doc[0].Value)
	})
	s.must(s.vm.Set("EJSON", ejson))
}

// shellLiteral bridges ordinary helpers to gomongo while preserving BSON.
func shellLiteral(value any) (string, error) {
	quote := func(text string) string { raw, _ := json.Marshal(text); return string(raw) }
	switch value := value.(type) {
	case nil:
		return "null", nil
	case bson.D:
		parts := make([]string, 0, len(value))
		for _, e := range value {
			literal, err := shellLiteral(e.Value)
			if err != nil {
				return "", err
			}
			parts = append(parts, quote(e.Key)+":"+literal)
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	case bson.A:
		parts := make([]string, len(value))
		for i, item := range value {
			literal, err := shellLiteral(item)
			if err != nil {
				return "", err
			}
			parts[i] = literal
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case string:
		return quote(value), nil
	case int64:
		return "NumberLong(" + quote(strconv.FormatInt(value, 10)) + ")", nil
	case int32:
		return "NumberInt(" + strconv.FormatInt(int64(value), 10) + ")", nil
	case bson.ObjectID:
		return "ObjectId(" + quote(value.Hex()) + ")", nil
	case bson.Decimal128:
		return "NumberDecimal(" + quote(value.String()) + ")", nil
	case bson.DateTime:
		return "ISODate(" + quote(value.Time().UTC().Format(time.RFC3339Nano)) + ")", nil
	case bson.Regex:
		return "RegExp(" + quote(value.Pattern) + "," + quote(value.Options) + ")", nil
	case bson.Timestamp:
		return fmt.Sprintf("Timestamp(%d,%d)", value.T, value.I), nil
	case bson.Binary:
		if value.Subtype == 4 && len(value.Data) == 16 {
			id, err := uuid.FromBytes(value.Data)
			if err != nil {
				return "", err
			}
			return "UUID(" + quote(id.String()) + ")", nil
		}
		return "", fmt.Errorf("helper cannot encode binary subtype %d; use a database command", value.Subtype)
	default:
		raw, err := json.Marshal(value)
		return string(raw), err
	}
}
