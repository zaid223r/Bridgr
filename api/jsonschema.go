package api

import (
	"encoding"
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/zaid223r/Bridgr/core/brschema"
)

// schemaGen turns Go types into JSON Schema (2020-12, as used by OpenAPI 3.1).
// Named struct types become components and are referenced with $ref.
type schemaGen struct {
	components map[string]any
	names      map[reflect.Type]string
	taken      map[string]reflect.Type
}

func newSchemaGen() *schemaGen {
	return &schemaGen{
		components: map[string]any{"Problem": problemSchema()},
		names:      map[reflect.Type]string{},
		taken:      map[string]reflect.Type{"Problem": nil},
	}
}

var (
	timeType          = reflect.TypeFor[time.Time]()
	rawMessageType    = reflect.TypeFor[json.RawMessage]()
	jsonMarshalerType = reflect.TypeFor[json.Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
)

// schemaFor returns the schema of values of type t as encoding/json writes them.
func (g *schemaGen) schemaFor(t reflect.Type) map[string]any {
	if t.Kind() == reflect.Pointer {
		return nullable(g.schemaFor(t.Elem()))
	}
	switch {
	case t == timeType:
		return map[string]any{"type": "string", "format": "date-time"}
	case t == rawMessageType:
		return map[string]any{}
	case t.Implements(jsonMarshalerType) || reflect.PointerTo(t).Implements(jsonMarshalerType):
		// Custom JSON: we can't know its shape.
		return map[string]any{}
	case t.Implements(textMarshalerType) || reflect.PointerTo(t).Implements(textMarshalerType):
		return map[string]any{"type": "string"}
	}

	switch t.Kind() {
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return map[string]any{"type": "integer", "format": "int32"}
	case reflect.Int, reflect.Int64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return map[string]any{"type": "integer", "format": "int32", "minimum": 0}
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return map[string]any{"type": "integer", "format": "int64", "minimum": 0}
	case reflect.Float32:
		return map[string]any{"type": "number", "format": "float"}
	case reflect.Float64:
		return map[string]any{"type": "number", "format": "double"}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return nullable(map[string]any{"type": "string", "contentEncoding": "base64"})
		}
		// A nil slice encodes as null.
		return nullable(map[string]any{"type": "array", "items": g.schemaFor(t.Elem())})
	case reflect.Array:
		return map[string]any{"type": "array", "items": g.schemaFor(t.Elem()), "minItems": t.Len(), "maxItems": t.Len()}
	case reflect.Map:
		// A nil map encodes as null.
		return nullable(map[string]any{"type": "object", "additionalProperties": g.schemaFor(t.Elem())})
	case reflect.Struct:
		if t.Name() == "" {
			return g.objectSchema(t, false)
		}
		return g.ref(t)
	default: // interface, func, chan
		return map[string]any{}
	}
}

// ref registers t as a component, if it isn't already, and returns a $ref to it.
func (g *schemaGen) ref(t reflect.Type) map[string]any {
	name, ok := g.names[t]
	if !ok {
		name = g.uniqueName(t)
		g.names[t] = name
		g.components[name] = map[string]any{} // placeholder breaks recursion
		g.components[name] = g.objectSchema(t, false)
	}
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

// inputRef registers "<Name>Input", the request body of create, replace and
// update: no property is required (missing fields are zero on create and replace,
// unchanged on update), and a generated integer key is left out.
func (g *schemaGen) inputRef(t reflect.Type) map[string]any {
	base := g.names[t]
	if base == "" {
		g.ref(t)
		base = g.names[t]
	}
	name := base + "Input"
	if _, ok := g.components[name]; !ok {
		g.components[name] = g.objectSchema(t, true)
	}
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

// objectSchema describes a struct using brschema's field list, so the schema
// matches what encoding/json writes (embedded structs flattened, json:"-"
// skipped). In responses, fields without omitempty are always present, hence
// required, writeonly fields are left out and the key and readonly fields are
// marked readOnly. Input schemas require nothing, drop readonly fields and
// generated integer keys, and mark writeonly fields writeOnly.
func (g *schemaGen) objectSchema(t reflect.Type, input bool) map[string]any {
	model, err := brschema.For(t)
	if err != nil {
		return map[string]any{"type": "object"}
	}
	props := map[string]any{}
	required := []string{}
	for _, f := range model.Fields {
		switch {
		case input && f.ReadOnly,
			input && f.PrimaryKey && f.OpenAPIType == "integer",
			!input && f.WriteOnly:
			continue
		}
		s := g.schemaFor(f.Type)
		switch {
		case !input && (f.PrimaryKey || f.ReadOnly):
			s = withFlag(s, "readOnly")
		case input && f.WriteOnly:
			s = withFlag(s, "writeOnly")
		}
		props[f.JSONName] = s
		if !f.OmitEmpty && !input {
			required = append(required, f.JSONName)
		}
	}
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// withFlag returns s with a boolean keyword such as readOnly set.
func withFlag(s map[string]any, flag string) map[string]any {
	out := map[string]any{flag: true}
	if _, isRef := s["$ref"]; isRef {
		out["allOf"] = []any{s}
		return out
	}
	for k, v := range s {
		out[k] = v
	}
	return out
}

// nullable allows null in addition to s.
func nullable(s map[string]any) map[string]any {
	if typ, ok := s["type"].(string); ok {
		out := map[string]any{}
		for k, v := range s {
			out[k] = v
		}
		out["type"] = []string{typ, "null"}
		return out
	}
	if len(s) == 0 {
		return s // already accepts anything
	}
	return map[string]any{"anyOf": []any{s, map[string]any{"type": "null"}}}
}

// uniqueName picks a component name for t: its Go name, prefixed with the
// package name if another type already uses it.
func (g *schemaGen) uniqueName(t reflect.Type) string {
	base := typeName(t.Name())
	if other, taken := g.taken[base]; !taken || other == t {
		g.taken[base] = t
		return base
	}
	pkg := t.PkgPath()
	pkg = pkg[strings.LastIndex(pkg, "/")+1:]
	name := pascal(pkg) + base
	for i := 2; ; i++ {
		if _, taken := g.taken[name]; !taken {
			g.taken[name] = t
			return name
		}
		name = pascal(pkg) + base + strconv.Itoa(i)
	}
}

// typeName turns a Go type name into a component name. Generic instantiations
// like "Page[example.com/app.Todo]" become "PageTodo".
func typeName(name string) string {
	var b strings.Builder
	for part := range strings.FieldsFuncSeq(name, func(r rune) bool { return r == '[' || r == ']' || r == ',' }) {
		part = strings.TrimSpace(part)
		if i := strings.LastIndexAny(part, "./"); i >= 0 {
			part = part[i+1:]
		}
		b.WriteString(pascal(part))
	}
	return b.String()
}

// pascal turns "order-items" or "order_items" into "OrderItems".
func pascal(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upper = true
			continue
		}
		if upper {
			r = unicode.ToUpper(r)
			upper = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
