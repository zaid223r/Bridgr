package brcrud

import (
	"context"
	"maps"
	"slices"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brschema"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// RegisterStore mounts CRUD routes for T backed by any brstore.Store, such as
// brstore.NewMemory. See RegisterCRUDRoutes for the routes and FromStore for how
// list filters are interpreted.
func RegisterStore[T any](router *brhttp.Router, path string, store brstore.Store[T], opts *BridgrOptions[T]) {
	RegisterCRUDRoutes(router, path, FromStore(store), opts)
}

// FromStore adapts a brstore.Store to BridgrModel. List query parameters must
// name filterable fields (see brschema) and are parsed to the field's type:
//
//   - ?name=bolt on a string field matches case-insensitively as a substring
//   - ?qty=3 on any other field matches exactly
//   - repeating a parameter (?qty=1&qty=2) matches any of the values
//
// Unknown fields and unparsable values are rejected with 400 errors.
func FromStore[T any](store brstore.Store[T]) BridgrModel[T] {
	return storeModel[T]{store: store}
}

type storeModel[T any] struct {
	store brstore.Store[T]
}

func (s storeModel[T]) List(ctx context.Context, filters map[string][]string) ([]T, error) {
	model, err := brschema.Of[T]()
	if err != nil {
		return nil, err
	}
	var q brstore.Query
	var invalid []string
	for _, key := range slices.Sorted(maps.Keys(filters)) {
		field, ok := model.FilterField(key)
		if !ok {
			invalid = append(invalid, key)
			continue
		}
		f, err := toFilter(field, filters[key])
		if err != nil {
			return nil, err
		}
		q.Filters = append(q.Filters, f)
	}
	if len(invalid) > 0 {
		return nil, &brerrors.InvalidFilterFieldError{Fields: invalid}
	}
	return s.store.List(ctx, q)
}

func toFilter(field *brschema.Field, raw []string) (brstore.Filter, error) {
	values := make([]any, 0, len(raw))
	for _, r := range raw {
		v, err := brstore.ParseValue(field, r)
		if err != nil {
			return brstore.Filter{}, err
		}
		values = append(values, v)
	}
	switch {
	case len(values) > 1:
		return brstore.Filter{Field: field.JSONName, Op: brstore.In, Value: values}, nil
	case field.OpenAPIType == "string" && field.Format == "":
		return brstore.Filter{Field: field.JSONName, Op: brstore.Contains, Value: values[0]}, nil
	default:
		return brstore.Filter{Field: field.JSONName, Op: brstore.Eq, Value: values[0]}, nil
	}
}

func (s storeModel[T]) Get(ctx context.Context, id string) (T, error) {
	return s.store.Get(ctx, id)
}

func (s storeModel[T]) Create(ctx context.Context, input T) (T, error) {
	return s.store.Create(ctx, input)
}

func (s storeModel[T]) Update(ctx context.Context, id string, input T) (T, error) {
	return s.store.Update(ctx, id, input)
}

func (s storeModel[T]) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}
