package api

import (
	"reflect"

	"github.com/zaid223r/Bridgr/core/brschema"
)

// ReflectModel maps each JSON field of a struct (or pointer to struct) to an
// OpenAPI property keyed by its JSON name. It panics if the model is invalid,
// e.g. FilterableFields names a field that does not exist.
func ReflectModel(model any) map[string]any {
	m := mustSchema(model)
	properties := make(map[string]any, len(m.Fields))
	for _, f := range m.Fields {
		properties[f.JSONName] = property(f)
	}
	return properties
}

func property(f *brschema.Field) map[string]any {
	p := map[string]any{"type": f.OpenAPIType}
	if f.Format != "" {
		p["format"] = f.Format
	}
	if f.Nullable {
		p["nullable"] = true
	}
	return p
}

func mustSchema(model any) *brschema.Model {
	m, err := brschema.For(reflect.TypeOf(model))
	if err != nil {
		panic(err)
	}
	return m
}
