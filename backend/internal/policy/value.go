package policy

import (
	"encoding/json"
	"math"

	"github.com/google/uuid"
)

type valueKind uint8

const (
	kindNull valueKind = iota
	kindBool
	kindInt
	kindFloat
	kindString
	kindList
)

func (k valueKind) String() string {
	switch k {
	case kindNull:
		return "null"
	case kindBool:
		return "bool"
	case kindInt:
		return "int"
	case kindFloat:
		return "float"
	case kindString:
		return "string"
	case kindList:
		return "list"
	default:
		return "unknown"
	}
}

// value adalah nilai hasil evaluasi. Nilai nol = null.
type value struct {
	kind valueKind
	b    bool
	i    int64
	f    float64
	s    string
	list []value
}

func boolValue(b bool) value {
	return value{kind: kindBool, b: b}
}

func intValue(i int64) value {
	return value{kind: kindInt, i: i}
}

func floatValue(f float64) value {
	return value{kind: kindFloat, f: f}
}

func stringValue(s string) value {
	return value{kind: kindString, s: s}
}

func listValue(items []value) value {
	return value{kind: kindList, list: items}
}

func (v value) isNumber() bool {
	return v.kind == kindInt || v.kind == kindFloat
}

func (v value) asFloat() float64 {
	if v.kind == kindInt {
		return float64(v.i)
	}
	return v.f
}

// normalize mengubah nilai mentah dari Env menjadi value. Tipe yang tidak dikenal adalah galat
// (gagal-tertutup), bukan diubah diam-diam menjadi string.
func normalize(raw any) (value, error) {
	switch x := raw.(type) {
	case []any:
		return normalizeList(x)
	case []string:
		items := make([]any, len(x))
		for i, s := range x {
			items[i] = s
		}
		return normalizeList(items)
	default:
		return normalizeScalar(raw)
	}
}

func normalizeList(items []any) (value, error) {
	if len(items) > MaxListElements {
		return value{}, evalErr("atribut daftar lebih dari %d elemen", MaxListElements)
	}
	out := make([]value, 0, len(items))
	for _, it := range items {
		v, err := normalizeScalar(it)
		if err != nil {
			return value{}, err
		}
		out = append(out, v)
	}
	return listValue(out), nil
}

func normalizeScalar(raw any) (value, error) {
	switch x := raw.(type) {
	case nil:
		return value{}, nil
	case bool:
		return boolValue(x), nil
	case string:
		if len(x) > MaxAttrStringBytes {
			return value{}, evalErr("atribut string lebih dari %d byte", MaxAttrStringBytes)
		}
		return stringValue(x), nil
	case int:
		return intValue(int64(x)), nil
	case int8:
		return intValue(int64(x)), nil
	case int16:
		return intValue(int64(x)), nil
	case int32:
		return intValue(int64(x)), nil
	case int64:
		return intValue(x), nil
	case uint8:
		return intValue(int64(x)), nil
	case uint16:
		return intValue(int64(x)), nil
	case uint32:
		return intValue(int64(x)), nil
	case uint:
		return unsignedValue(uint64(x))
	case uint64:
		return unsignedValue(x)
	case float32:
		return finiteFloat(float64(x))
	case float64:
		return finiteFloat(x)
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return intValue(i), nil
		}
		f, err := x.Float64()
		if err != nil {
			return value{}, evalErr("json.Number tidak sah")
		}
		return finiteFloat(f)
	case uuid.UUID:
		return stringValue(x.String()), nil
	case *uuid.UUID:
		if x == nil {
			return value{}, nil
		}
		return stringValue(x.String()), nil
	}
	return value{}, evalErr("tipe atribut %T tidak didukung", raw)
}

func unsignedValue(u uint64) (value, error) {
	if u > math.MaxInt64 {
		return value{}, evalErr("bilangan bulat tak bertanda di luar jangkauan")
	}
	return intValue(int64(u)), nil
}

func finiteFloat(f float64) (value, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return value{}, evalErr("bilangan desimal bukan bilangan terhingga")
	}
	return floatValue(f), nil
}
