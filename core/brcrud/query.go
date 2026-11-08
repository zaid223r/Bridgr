package brcrud

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
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
//
// The reserved parameters limit, offset and sort page and order the results; see
// parsePage and parseSort.
func parseQuery(model *brschema.Model, params url.Values, limits pageLimits) (brstore.Query, error) {
	var q brstore.Query
	var err error
	if q.Limit, q.Offset, err = parsePage(params, limits); err != nil {
		return q, err
	}
	if q.Sort, err = parseSort(model, params.Get("sort")); err != nil {
		return q, err
	}
	var invalid []string
	for _, key := range slices.Sorted(maps.Keys(params)) {
		if reservedParams[key] {
			continue
		}
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

// reservedParams are query parameters that page and sort instead of filtering.
var reservedParams = map[string]bool{"limit": true, "offset": true, "sort": true}

type pageLimits struct {
	def, max int
}

// parsePage reads limit (1..max, default def) and offset (≥ 0, default 0).
func parsePage(params url.Values, limits pageLimits) (limit, offset int, err error) {
	limit = limits.def
	if raw, ok := single(params, "limit"); ok {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 1 || n > limits.max {
			return 0, 0, &brerrors.InvalidParamError{Param: "limit", Value: raw, Reason: fmt.Sprintf("want an integer from 1 to %d", limits.max)}
		}
		limit = n
	}
	if raw, ok := single(params, "offset"); ok {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n < 0 {
			return 0, 0, &brerrors.InvalidParamError{Param: "offset", Value: raw, Reason: "want an integer of 0 or more"}
		}
		offset = n
	}
	for _, p := range []string{"limit", "offset", "sort"} {
		if len(params[p]) > 1 {
			return 0, 0, &brerrors.InvalidParamError{Param: p, Value: strings.Join(params[p], ","), Reason: "give it once"}
		}
	}
	return limit, offset, nil
}

func single(params url.Values, key string) (string, bool) {
	v, ok := params[key]
	if !ok || len(v) == 0 {
		return "", false
	}
	return v[0], true
}

// parseSort reads sort=-qty,title: comma-separated field names, "-" for
// descending. Only filterable fields and the primary key can be sorted on.
func parseSort(model *brschema.Model, raw string) ([]brstore.Sort, error) {
	if raw == "" {
		return nil, nil
	}
	var out []brstore.Sort
	for part := range strings.SplitSeq(raw, ",") {
		name, desc := strings.CutPrefix(part, "-")
		_, filterable := model.FilterField(name)
		isPK := model.PrimaryKey != nil && model.PrimaryKey.JSONName == name
		if !filterable && !isPK {
			return nil, &brerrors.InvalidParamError{Param: "sort", Value: raw, Reason: fmt.Sprintf("can't sort by %q; use %s", name, strings.Join(sortable(model), ", "))}
		}
		out = append(out, brstore.Sort{Field: name, Desc: desc})
	}
	return out, nil
}

func sortable(model *brschema.Model) []string {
	var names []string
	if model.PrimaryKey != nil {
		names = append(names, model.PrimaryKey.JSONName)
	}
	for _, f := range model.Filterable {
		if !slices.Contains(names, f.JSONName) {
			names = append(names, f.JSONName)
		}
	}
	return names
}

// pageLinks builds an RFC 8288 Link header with first, prev, next and last pages.
// Other query parameters (filters, sort) are kept.
func pageLinks(u *url.URL, limit, offset int, total int64) string {
	link := func(off int, rel string) string {
		q := u.Query()
		q.Set("limit", strconv.Itoa(limit))
		q.Set("offset", strconv.Itoa(off))
		return fmt.Sprintf(`<%s?%s>; rel="%s"`, u.Path, q.Encode(), rel)
	}
	last := 0
	if total > 0 {
		last = int((total - 1) / int64(limit) * int64(limit))
	}
	links := []string{link(0, "first")}
	if offset > 0 {
		links = append(links, link(max(offset-limit, 0), "prev"))
	}
	if int64(offset+limit) < total {
		links = append(links, link(offset+limit, "next"))
	}
	links = append(links, link(last, "last"))
	return strings.Join(links, ", ")
}
