package brcrud

import (
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brschema"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// parseQuery turns list query parameters into a brstore.Query. Parameters must
// name filterable fields (see brschema); values are parsed to the field's type.
//
//	?title=bolt          plain string value: case-insensitive substring match
//	?qty=3               plain value of any other type: exact match
//	?qty=1&qty=2         repeated plain value: any of the values
//	?qty[gte]=3          operator: eq, ne, gt, gte, lt, lte, contains, in
//	?qty[in]=1,2,3       in takes a comma-separated list (and may repeat)
//	?qty[gte]=1&qty[lt]=9  several operators on one field are combined with AND
//
// The operators allowed per type come from brstore.OpsFor. Unknown fields,
// unknown or unsuitable operators and unparsable values return 400 errors.
func parseQuery(model *brschema.Model, params url.Values) (brstore.Query, error) {
	var q brstore.Query
	var invalid []string
	for _, key := range slices.Sorted(maps.Keys(params)) {
		name, opName, hasOp := splitKey(key)
		field, ok := model.FilterField(name)
		if !ok {
			if !slices.Contains(invalid, name) {
				invalid = append(invalid, name)
			}
			continue
		}
		var f brstore.Filter
		var err error
		if hasOp {
			f, err = operatorFilter(field, opName, params[key])
		} else {
			f, err = plainFilter(field, params[key])
		}
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

// splitKey splits "qty[gte]" into "qty", "gte". Keys without a well-formed
// bracket suffix are plain field names.
func splitKey(key string) (name, op string, hasOp bool) {
	i := strings.IndexByte(key, '[')
	if i <= 0 || !strings.HasSuffix(key, "]") {
		return key, "", false
	}
	return key[:i], key[i+1 : len(key)-1], true
}

func plainFilter(field *brschema.Field, raw []string) (brstore.Filter, error) {
	values, err := parseValues(field, raw)
	if err != nil {
		return brstore.Filter{}, err
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

func operatorFilter(field *brschema.Field, opName string, raw []string) (brstore.Filter, error) {
	allowed := brstore.OpsFor(field)
	op, ok := brstore.ParseOp(opName)
	if !ok || !slices.Contains(allowed, op) {
		names := make([]string, len(allowed))
		for i, a := range allowed {
			names[i] = string(a)
		}
		return brstore.Filter{}, &brerrors.InvalidFilterOperatorError{Field: field.JSONName, Op: opName, Allowed: names}
	}

	if op == brstore.In {
		var parts []string
		for _, r := range raw {
			parts = append(parts, strings.Split(r, ",")...)
		}
		values, err := parseValues(field, parts)
		if err != nil {
			return brstore.Filter{}, err
		}
		return brstore.Filter{Field: field.JSONName, Op: op, Value: values}, nil
	}

	if len(raw) > 1 {
		return brstore.Filter{}, &brerrors.InvalidFilterValueError{
			Field: field.JSONName, Value: strings.Join(raw, ","), Type: "a single value (use [in] for several)",
		}
	}
	values, err := parseValues(field, raw)
	if err != nil {
		return brstore.Filter{}, err
	}
	return brstore.Filter{Field: field.JSONName, Op: op, Value: values[0]}, nil
}

func parseValues(field *brschema.Field, raw []string) ([]any, error) {
	values := make([]any, 0, len(raw))
	for _, r := range raw {
		v, err := brstore.ParseValue(field, r)
		if err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, nil
}
