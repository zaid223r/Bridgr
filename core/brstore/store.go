// Package brstore defines the storage contract every Bridgr backend implements
// (SQL, MongoDB, in-memory) and the typed Query they receive.
//
// Field names in queries are JSON names from the model's brschema.Model. The HTTP
// layer decides which fields clients may filter on and parses values into Go
// types (see ParseValue); a store only has to apply the query.
package brstore

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brschema"
)

// Store persists values of a model type T. Every implementation must pass the
// contract suite in package storetest. In short:
//
//   - Get, Update and Delete return an error wrapping brerrors.ErrNotFound for an
//     unknown id, including an id that doesn't parse as the primary key type.
//   - Create assigns the next integer primary key when it is zero, and returns an
//     error wrapping brerrors.ErrConflict when the key already exists.
//   - Update replaces every field except the primary key, which comes from id.
//   - List without Sort orders by primary key ascending; the primary key also
//     breaks ties after the requested Sort. Where nil pointer fields sort is
//     unspecified.
//   - A query naming an unknown field returns an error wrapping ErrInvalidQuery.
//   - A nil pointer field matches only Ne; every other operator excludes it.
//   - A cancelled ctx returns an error wrapping ctx.Err().
type Store[T any] interface {
	List(ctx context.Context, q Query) ([]T, error)
	Get(ctx context.Context, id string) (T, error)
	Create(ctx context.Context, value T) (T, error)
	Update(ctx context.Context, id string, value T) (T, error)
	Delete(ctx context.Context, id string) error
}

// ErrInvalidQuery means a Query referenced an unknown field or used an operator
// that doesn't apply to the field's type. The HTTP layer validates queries before
// they reach a store, so this indicates a programming error (500).
var ErrInvalidQuery = errors.New("brstore: invalid query")

// Op is a filter comparison.
type Op string

// Filter operators.
const (
	Eq       Op = "eq"
	Ne       Op = "ne"
	Gt       Op = "gt"
	Gte      Op = "gte"
	Lt       Op = "lt"
	Lte      Op = "lte"
	Contains Op = "contains" // case-insensitive substring match, strings only
	In       Op = "in"       // Value is a slice; matches any element
)

// Filter restricts results to records whose Field compares to Value with Op.
// Value has the field's Go type as produced by ParseValue (int64, uint64,
// float64, string, bool or time.Time), or a []any of those for In.
type Filter struct {
	Field string
	Op    Op
	Value any
}

// Sort orders results by Field, descending when Desc is set.
type Sort struct {
	Field string
	Desc  bool
}

// Query selects records for List. Filters are combined with AND. Limit 0 means
// no limit.
type Query struct {
	Filters []Filter
	Sort    []Sort
	Limit   int
	Offset  int
}

var timeType = reflect.TypeFor[time.Time]()

// ParseValue converts a query-string value to field's Go type: int64, uint64,
// float64, string, bool, or time.Time (RFC 3339). A value that doesn't parse
// returns *brerrors.InvalidFilterValueError.
func ParseValue(field *brschema.Field, s string) (any, error) {
	t := field.Type
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	bad := &brerrors.InvalidFilterValueError{Field: field.JSONName, Value: s, Type: typeName(field)}

	if t == timeType {
		v, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return nil, bad
		}
		return v, nil
	}
	switch t.Kind() {
	case reflect.String:
		return s, nil
	case reflect.Bool:
		v, err := strconv.ParseBool(s)
		if err != nil {
			return nil, bad
		}
		return v, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v, err := strconv.ParseInt(s, 10, t.Bits())
		if err != nil {
			return nil, bad
		}
		return v, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v, err := strconv.ParseUint(s, 10, t.Bits())
		if err != nil {
			return nil, bad
		}
		return v, nil
	case reflect.Float32, reflect.Float64:
		v, err := strconv.ParseFloat(s, t.Bits())
		if err != nil {
			return nil, bad
		}
		return v, nil
	default:
		return nil, fmt.Errorf("%w: field %s of type %s can't be filtered", ErrInvalidQuery, field.JSONName, field.Type)
	}
}

func typeName(f *brschema.Field) string {
	if f.Format != "" {
		return f.OpenAPIType + " (" + f.Format + ")"
	}
	return f.OpenAPIType
}

// ParseID converts a path id to the model's primary key type. An id that doesn't
// parse can't match any record, so the error wraps brerrors.ErrNotFound.
func ParseID(model *brschema.Model, id string) (any, error) {
	if model.PrimaryKey == nil {
		return nil, fmt.Errorf("brstore: %s has no primary key", model.Name)
	}
	v, err := ParseValue(model.PrimaryKey, id)
	if err != nil {
		return nil, fmt.Errorf("id %q: %w", id, brerrors.ErrNotFound)
	}
	return v, nil
}
