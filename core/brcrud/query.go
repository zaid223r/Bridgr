package brcrud

import (
	"maps"
	"net/url"
	"slices"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brschema"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// parseQuery turns list query parameters into a brstore.Query. Parameters must
// name filterable fields (see brschema) and are parsed to the field's type:
//
//   - ?name=bolt on a string field matches case-insensitively as a substring
//   - ?qty=3 on any other field matches exactly
//   - repeating a parameter (?qty=1&qty=2) matches any of the values
//
// Unknown fields and unparsable values return 400 errors.
func parseQuery(model *brschema.Model, params url.Values) (brstore.Query, error) {
	var q brstore.Query
	var invalid []string
	for _, key := range slices.Sorted(maps.Keys(params)) {
		field, ok := model.FilterField(key)
		if !ok {
			invalid = append(invalid, key)
			continue
		}
		f, err := toFilter(field, params[key])
		if err != nil {
			return q, err
		}
		q.Filters = append(q.Filters, f)
	}
	if len(invalid) > 0 {
		return q, &brerrors.InvalidFilterFieldError{Fields: invalid}
	}
	return q, nil
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
