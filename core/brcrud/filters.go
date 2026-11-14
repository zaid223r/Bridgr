package brcrud

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/zaid223r/Bridgr/api"
	"github.com/zaid223r/Bridgr/core/brschema"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// FilterOps is a set of filter operators a list endpoint accepts on one field.
// Combine them with |, e.g. Exact | Contains.
type FilterOps uint16

// Filter operator sets. A plain ?field=value is an exact match when Exact is
// allowed, otherwise a substring match when Contains is; repeating it
// (?qty=1&qty=2) matches any of the values when In is allowed.
const (
	// Exact allows ?field=value and ?field[eq]=value.
	Exact FilterOps = 1 << iota
	// Not allows ?field[ne]=value.
	Not
	// Contains allows ?field[contains]=value, a case-insensitive substring
	// match. Strings only.
	Contains
	// Range allows ?field[gt]=, [gte], [lt] and [lte]. Numbers and times only.
	Range
	// In allows ?field[in]=a,b,c and repeating a plain value.
	In
	// All allows every operator that applies to the field's type.
	All FilterOps = 1<<16 - 1
)

// Filters maps a JSON field name to the operators clients may filter it with.
// Fields that aren't listed can't be filtered: a list request naming them gets
// 400.
type Filters map[string]FilterOps

// filterSpec is one filterable field, resolved at registration.
type filterSpec struct {
	field   *brschema.Field
	allowed []brstore.Op
	// plain is the operator of ?field=value, or "" when it isn't allowed.
	plain brstore.Op
}

// listSpec is everything a list request may use, resolved at registration.
type listSpec struct {
	filters map[string]filterSpec
	// sortable are the JSON names sort accepts: the primary key, then Options.Sort.
	sortable []string
	limits   pageLimits
}

// resolveList checks Filters and Sort against the model and precomputes what
// each field accepts. Mistakes panic, so they surface at startup.
func resolveList(where string, model *brschema.Model, filters Filters, sort []string, limits pageLimits) listSpec {
	spec := listSpec{filters: map[string]filterSpec{}, limits: limits}
	for _, name := range slices.Sorted(maps.Keys(filters)) {
		field := listField(where, model, "Filters", name)
		spec.filters[name] = resolveOps(where, field, filters[name])
	}
	spec.sortable = []string{model.PrimaryKey.JSONName}
	for _, name := range sort {
		field := listField(where, model, "Sort", name)
		if slices.Contains(spec.sortable, field.JSONName) {
			continue
		}
		if brstore.OpsFor(field) == nil {
			panic(fmt.Sprintf("%s: Sort: %s has type %s, which can't be sorted", where, name, field.Type))
		}
		spec.sortable = append(spec.sortable, name)
	}
	return spec
}

// listField looks up a field named in Filters or Sort.
func listField(where string, model *brschema.Model, option, name string) *brschema.Field {
	field, ok := model.FieldByJSON(name)
	switch {
	case reservedParams[name]:
		panic(fmt.Sprintf("%s: %s: %q is reserved for paging and sorting", where, option, name))
	case !ok:
		panic(fmt.Sprintf("%s: %s: %q is not a JSON field of %s", where, option, name, model.Name))
	case field.WriteOnly:
		panic(fmt.Sprintf("%s: %s: %q is writeonly; filtering or sorting on it would reveal it", where, option, name))
	}
	return field
}

// resolveOps turns an operator set into the operators valid for field.
func resolveOps(where string, field *brschema.Field, ops FilterOps) filterSpec {
	valid := brstore.OpsFor(field)
	if valid == nil {
		panic(fmt.Sprintf("%s: Filters: %s has type %s, which can't be filtered", where, field.JSONName, field.Type))
	}
	if ops == 0 {
		panic(fmt.Sprintf("%s: Filters: %s allows no operators", where, field.JSONName))
	}
	var want []brstore.Op
	if ops == All {
		want = valid
	} else {
		for _, set := range []struct {
			flag FilterOps
			name string
			ops  []brstore.Op
		}{
			{Exact, "Exact", []brstore.Op{brstore.Eq}},
			{Not, "Not", []brstore.Op{brstore.Ne}},
			{Contains, "Contains", []brstore.Op{brstore.Contains}},
			{Range, "Range", []brstore.Op{brstore.Gt, brstore.Gte, brstore.Lt, brstore.Lte}},
			{In, "In", []brstore.Op{brstore.In}},
		} {
			if ops&set.flag == 0 {
				continue
			}
			for _, op := range set.ops {
				if !slices.Contains(valid, op) {
					panic(fmt.Sprintf("%s: Filters: %s doesn't apply to %s (%s); it allows %s",
						where, set.name, field.JSONName, field.Type, opNames(valid)))
				}
			}
			want = append(want, set.ops...)
		}
	}
	spec := filterSpec{field: field}
	for _, op := range valid { // keep the canonical order for docs and errors
		if slices.Contains(want, op) {
			spec.allowed = append(spec.allowed, op)
		}
	}
	switch {
	case slices.Contains(spec.allowed, brstore.Eq):
		spec.plain = brstore.Eq
	case slices.Contains(spec.allowed, brstore.Contains):
		spec.plain = brstore.Contains
	}
	return spec
}

// docs lists the filters for the OpenAPI document, sorted by name.
func (s listSpec) docs() []api.FilterDoc {
	out := make([]api.FilterDoc, 0, len(s.filters))
	for _, name := range slices.Sorted(maps.Keys(s.filters)) {
		fs := s.filters[name]
		out = append(out, api.FilterDoc{Field: fs.field, Ops: fs.allowed, Plain: fs.plain})
	}
	return out
}

func opNames(ops []brstore.Op) string {
	names := make([]string, len(ops))
	for i, op := range ops {
		names[i] = string(op)
	}
	return strings.Join(names, ", ")
}
