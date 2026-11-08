package brstore

import (
	"reflect"
	"slices"

	"github.com/zaid223r/Bridgr/core/brschema"
)

var (
	stringOps  = []Op{Eq, Ne, Contains, In}
	boolOps    = []Op{Eq, Ne}
	orderedOps = []Op{Eq, Ne, Gt, Gte, Lt, Lte, In}
	allOps     = []Op{Eq, Ne, Gt, Gte, Lt, Lte, Contains, In}
)

// OpsFor returns the operators clients may use on field. Strings get no range
// operators because databases order text by different collations; booleans only
// compare for (in)equality. The result must not be modified.
func OpsFor(field *brschema.Field) []Op {
	t := field.Type
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType {
		return orderedOps
	}
	switch t.Kind() {
	case reflect.String:
		return stringOps
	case reflect.Bool:
		return boolOps
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return orderedOps
	default:
		return nil
	}
}

// ParseOp returns the operator named s ("eq", "gte", ...).
func ParseOp(s string) (Op, bool) {
	op := Op(s)
	return op, slices.Contains(allOps, op)
}
