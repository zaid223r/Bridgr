package api

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/jinzhu/inflection"

	"github.com/zaid223r/Bridgr/core/brstore"
)

// pathItems returns the two path items of a CRUD resource: /path and /path/{id}.
func (g *schemaGen) pathItems(res resource) map[string]any {
	model := res.model
	// Custom types replace the model's schemas, which are then left out of
	// the document altogether, so it never shows fields clients can't see.
	custom := func(t reflect.Type, fallback func(reflect.Type) map[string]any) map[string]any {
		if t != nil {
			return g.schemaFor(t)
		}
		return fallback(model.Type)
	}
	ref := custom(res.docs.Response, g.ref)
	createBody := custom(res.docs.CreateBody, g.inputRef)
	updateBody := custom(res.docs.UpdateBody, g.inputRef)
	plural := pascal(res.path)
	singular := inflection.Singular(plural)
	tags := []string{res.path}

	op := func(id, summary string, responses map[string]any, extra map[string]any) map[string]any {
		o := map[string]any{"operationId": id, "summary": summary, "tags": tags, "responses": responses}
		for k, v := range extra {
			o[k] = v
		}
		return o
	}
	errs := func(codes ...string) map[string]any {
		if res.docs.Auth {
			codes = append(codes, "401", "403")
		}
		out := map[string]any{}
		for _, c := range codes {
			if c == "422" && !res.docs.Validate {
				continue
			}
			out[c] = map[string]any{"$ref": "#/components/responses/" + problemResponseNames[c]}
		}
		return out
	}
	with := func(base map[string]any, code string, resp map[string]any) map[string]any {
		base[code] = resp
		return base
	}
	jsonBody := func(schema map[string]any) map[string]any {
		return map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
	}
	jsonResponse := func(desc string, schema map[string]any) map[string]any {
		return map[string]any{"description": desc, "content": map[string]any{"application/json": map[string]any{"schema": schema}}}
	}

	listResp := jsonResponse("A page of "+res.path+".", map[string]any{"type": "array", "items": ref})
	listResp["headers"] = map[string]any{
		"X-Total-Count": map[string]any{"description": "Number of records matching the filters, across all pages.", "schema": map[string]any{"type": "integer", "minimum": 0}},
		"Link":          map[string]any{"description": "RFC 8288 links to the first, prev, next and last pages.", "schema": map[string]any{"type": "string"}},
	}

	collection := map[string]any{
		"get": op("list"+plural, "List "+res.path, with(errs("400", "500"), "200", listResp),
			map[string]any{"parameters": listParameters(g, res)}),
		"post": op("create"+singular, "Create a "+singular, with(errs("400", "409", "413", "422", "500"), "201", jsonResponse("The created record.", ref)),
			map[string]any{"requestBody": jsonBody(createBody)}),
	}

	idParam := map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}}
	if pk := model.PrimaryKey; pk != nil {
		idParam["schema"] = g.schemaFor(pk.Type)
	}
	item := map[string]any{
		"parameters": []any{idParam},
		"get":        op("get"+singular, "Get a "+singular, with(errs("404", "500"), "200", jsonResponse("The record.", ref)), nil),
		"put": op("replace"+singular, "Replace a "+singular+"; fields missing from the body are reset",
			with(errs("400", "404", "409", "413", "422", "500"), "200", jsonResponse("The stored record.", ref)),
			map[string]any{"requestBody": jsonBody(updateBody)}),
		"patch": op("update"+singular, "Update the fields of a "+singular+" present in the body",
			with(errs("400", "404", "409", "413", "422", "500"), "200", jsonResponse("The stored record.", ref)),
			map[string]any{"requestBody": jsonBody(updateBody)}),
		"delete": op("delete"+singular, "Delete a "+singular,
			with(errs("404", "500"), "204", map[string]any{"description": "Deleted."}), nil),
	}

	base := "/" + res.path
	return map[string]any{base: collection, base + "/{id}": item}
}

// listParameters documents limit, offset, sort and one parameter per filterable
// field.
func listParameters(g *schemaGen, res resource) []any {
	limit := map[string]any{"type": "integer", "minimum": 1}
	if res.docs.MaxLimit > 0 {
		limit["maximum"] = res.docs.MaxLimit
	}
	if res.docs.DefaultLimit > 0 {
		limit["default"] = res.docs.DefaultLimit
	}
	params := []any{
		map[string]any{"name": "limit", "in": "query", "schema": limit, "description": "Page size."},
		map[string]any{"name": "offset", "in": "query", "schema": map[string]any{"type": "integer", "minimum": 0, "default": 0}, "description": "Number of records to skip."},
	}
	if sortable := res.docs.Sort; len(sortable) > 0 {
		params = append(params, map[string]any{
			"name": "sort", "in": "query", "schema": map[string]any{"type": "string"},
			"description": "Comma-separated fields, '-' for descending, e.g. -" + sortable[len(sortable)-1] + ". Sortable: " + strings.Join(sortable, ", ") + ".",
		})
	}
	for _, fd := range res.docs.Filters {
		schema := g.schemaFor(fd.Field.Type)
		if t, ok := schema["type"].([]string); ok { // a filter value is never null
			schema = clone(schema)
			schema["type"] = t[0]
		}
		params = append(params, map[string]any{
			"name": fd.Field.JSONName, "in": "query", "schema": schema, "description": filterDescription(fd),
		})
	}
	return params
}

func clone(s map[string]any) map[string]any {
	out := make(map[string]any, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// filterDescription explains a filter parameter and the operators it accepts, in
// the name[op]=value form that OpenAPI can't express as separate parameters.
func filterDescription(fd FilterDoc) string {
	name := fd.Field.JSONName
	var parts []string
	switch fd.Plain {
	case brstore.Eq:
		parts = append(parts, "Exact match.")
	case brstore.Contains:
		parts = append(parts, "Case-insensitive substring match.")
	default:
		parts = append(parts, fmt.Sprintf("Use an operator, e.g. %s[%s].", name, fd.Ops[0]))
	}
	if slices.Contains(fd.Ops, brstore.In) {
		parts = append(parts, "Repeat to match any of several values.")
	}
	forms := make([]string, len(fd.Ops))
	for i, op := range fd.Ops {
		forms[i] = fmt.Sprintf("%s[%s]", name, op)
	}
	parts = append(parts, fmt.Sprintf("Operators: %s.", strings.Join(forms, ", ")))
	if slices.Contains(fd.Ops, brstore.In) {
		parts = append(parts, fmt.Sprintf("%s[in] takes a comma-separated list.", name))
	}
	return strings.Join(parts, " ")
}

var problemResponseNames = map[string]string{
	"400": "BadRequest",
	"401": "Unauthorized",
	"403": "Forbidden",
	"404": "NotFound",
	"409": "Conflict",
	"413": "PayloadTooLarge",
	"422": "UnprocessableEntity",
	"500": "InternalServerError",
}

func problemResponses() map[string]any {
	descriptions := map[string]string{
		"BadRequest":          "Invalid filter, paging parameter or JSON body. invalid_fields names the offending parameters.",
		"Unauthorized":        "Missing or invalid credentials.",
		"Forbidden":           "Not allowed to perform this operation.",
		"NotFound":            "No record has this id.",
		"Conflict":            "A record with this key already exists.",
		"PayloadTooLarge":     "The request body is larger than the server allows.",
		"UnprocessableEntity": "The body failed validation; detail explains why.",
		"InternalServerError": "Something failed on the server. Details are logged, not returned.",
	}
	out := map[string]any{}
	for name, desc := range descriptions {
		out[name] = map[string]any{
			"description": desc,
			"content": map[string]any{
				"application/problem+json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Problem"}},
			},
		}
	}
	return out
}

// problemSchema is RFC 9457 problem details plus Bridgr's invalid_fields.
func problemSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "RFC 9457 problem details.",
		"required":    []string{"title", "status"},
		"properties": map[string]any{
			"type":           map[string]any{"type": "string", "format": "uri-reference", "description": "Omitted means about:blank."},
			"title":          map[string]any{"type": "string"},
			"status":         map[string]any{"type": "integer"},
			"detail":         map[string]any{"type": "string"},
			"instance":       map[string]any{"type": "string"},
			"request_id":     map[string]any{"type": "string", "description": "The X-Request-ID of the request, for support."},
			"invalid_fields": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"errors": map[string]any{
				"type":        "array",
				"description": "Field-level validation problems (422).",
				"items": map[string]any{
					"type":     "object",
					"required": []string{"field", "message"},
					"properties": map[string]any{
						"field":   map[string]any{"type": "string"},
						"message": map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}
