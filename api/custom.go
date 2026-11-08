package api

import (
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/zaid223r/Bridgr/core/brschema"
)

// Operation documents one custom endpoint, such as a typed handler. Types are
// turned into JSON Schema the same way CRUD models are.
type Operation struct {
	// Method is the HTTP method, e.g. "POST".
	Method string
	// Path uses router syntax, e.g. "/todos/{id}/archive".
	Path        string
	OperationID string
	Summary     string
	Tags        []string
	Params      []Param
	// Body is the request body type, or nil for none. Fields whose Go names
	// are in BodyExclude (path, query and header fields) are left out.
	Body        reflect.Type
	BodyExclude []string
	// Response is the success body type, or nil for an empty response.
	Response reflect.Type
	// Status is the success status code.
	Status int
	// Errors are the problem responses to document, e.g. 400, 404, 422, 500.
	Errors []int
}

// Param documents a path, query or header parameter.
type Param struct {
	Name string
	// In is "path", "query" or "header".
	In       string
	Required bool
	Type     reflect.Type
}

// AddOperation documents a custom endpoint.
func (r *Registry) AddOperation(op Operation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.operations = append(r.operations, op)
}

// customOperation renders op as an OpenAPI operation object.
func (g *schemaGen) customOperation(op Operation) map[string]any {
	o := map[string]any{"operationId": op.OperationID}
	if op.Summary != "" {
		o["summary"] = op.Summary
	}
	if len(op.Tags) > 0 {
		o["tags"] = op.Tags
	}
	if len(op.Params) > 0 {
		params := make([]any, len(op.Params))
		for i, p := range op.Params {
			schema := g.schemaFor(p.Type)
			if t, ok := schema["type"].([]string); ok { // optional, not nullable
				schema = clone(schema)
				schema["type"] = t[0]
			}
			params[i] = map[string]any{"name": p.Name, "in": p.In, "required": p.Required || p.In == "path", "schema": schema}
		}
		o["parameters"] = params
	}
	if op.Body != nil {
		o["requestBody"] = map[string]any{
			"required": true,
			"content":  map[string]any{"application/json": map[string]any{"schema": g.bodySchema(op.Body, op.BodyExclude)}},
		}
	}

	responses := map[string]any{}
	status := strconv.Itoa(op.Status)
	if op.Response == nil {
		responses[status] = map[string]any{"description": http.StatusText(op.Status)}
	} else {
		responses[status] = map[string]any{
			"description": http.StatusText(op.Status),
			"content":     map[string]any{"application/json": map[string]any{"schema": g.schemaFor(op.Response)}},
		}
	}
	for _, code := range op.Errors {
		if name, ok := problemResponseNames[strconv.Itoa(code)]; ok {
			responses[strconv.Itoa(code)] = map[string]any{"$ref": "#/components/responses/" + name}
		}
	}
	o["responses"] = responses
	return o
}

// bodySchema is an inline object schema for the JSON fields of t, minus the
// excluded Go fields. Nothing is required: missing fields decode as zero.
func (g *schemaGen) bodySchema(t reflect.Type, exclude []string) map[string]any {
	model, err := brschema.For(t)
	if err != nil {
		return map[string]any{"type": "object"}
	}
	props := map[string]any{}
	for _, f := range model.Fields {
		if slices.Contains(exclude, f.GoName) {
			continue
		}
		props[f.JSONName] = g.schemaFor(f.Type)
	}
	return map[string]any{"type": "object", "properties": props}
}

// addCustom merges custom operations into paths, next to CRUD path items.
func (g *schemaGen) addCustom(paths map[string]any, ops []Operation) {
	for _, op := range ops {
		item, ok := paths[op.Path].(map[string]any)
		if !ok {
			item = map[string]any{}
			paths[op.Path] = item
		}
		item[strings.ToLower(op.Method)] = g.customOperation(op)
	}
}
