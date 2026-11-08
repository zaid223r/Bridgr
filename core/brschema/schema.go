// Package brschema reads a model struct once and caches what the rest of Bridgr
// needs about it: JSON names, database columns, OpenAPI types, the primary key and
// the filterable fields. Request handling uses the cached Model and never
// reflects over struct fields.
package brschema

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

// Field is one JSON-visible field of a model, after embedded structs are flattened.
type Field struct {
	// GoName is the struct field name, e.g. "CreatedAt".
	GoName string
	// JSONName is the key used in request and response bodies and in filters.
	JSONName string
	// Column is the database column: the gorm:"column:..." tag if present,
	// otherwise the snake_case Go name ("CreatedAt" → "created_at", "UserID" → "user_id").
	Column string
	// Type is the Go type, with pointers kept.
	Type reflect.Type
	// Index is the path for reflect.Value.FieldByIndex, through embedded structs.
	Index []int
	// OpenAPIType is "string", "integer", "number", "boolean", "array" or "object".
	OpenAPIType string
	// Format is the OpenAPI format, e.g. "date-time" for time.Time, or "".
	Format string
	// Nullable is true for pointer fields.
	Nullable bool
	// OmitEmpty is true when the json tag has omitempty or omitzero.
	OmitEmpty bool
	// PrimaryKey is true for the model's primary key field.
	PrimaryKey bool

	taggedPK bool
}

// Model is the cached description of a struct type. It is immutable and safe for
// concurrent use.
type Model struct {
	// Type is the struct type (never a pointer).
	Type reflect.Type
	// Name is the struct's type name, e.g. "Todo".
	Name string
	// Fields are the JSON-visible fields in encoding/json order.
	Fields []*Field
	// PrimaryKey is the field tagged gorm:"primaryKey", else the field named ID,
	// else nil.
	PrimaryKey *Field
	// Filterable are the fields list endpoints may filter on, in declaration order.
	Filterable []*Field

	byJSON     map[string]*Field
	filterable map[string]*Field
}

// FieldByJSON returns the field with the given JSON name.
func (m *Model) FieldByJSON(name string) (*Field, bool) {
	f, ok := m.byJSON[name]
	return f, ok
}

// FilterField returns the filterable field with the given JSON name.
func (m *Model) FilterField(name string) (*Field, bool) {
	f, ok := m.filterable[name]
	return f, ok
}

// GenericFilterer is implemented by models that allow filtering on every field.
type GenericFilterer interface {
	GenericFiltering() bool
}

// FieldFilterer is implemented by models that allow filtering on the listed JSON
// field names only.
type FieldFilterer interface {
	FilterableFields() []string
}

type cacheEntry struct {
	model *Model
	err   error
}

// cache is process-wide on purpose, like encoding/json's: a type's description
// never changes, so sharing it across Apps is safe and avoids re-parsing.
var cache sync.Map // reflect.Type → cacheEntry

// Of returns the cached Model for T, parsing it on first use. T must be a struct
// or a pointer to one.
func Of[T any]() (*Model, error) {
	return For(reflect.TypeFor[T]())
}

// For is Of for a reflect.Type.
func For(t reflect.Type) (*Model, error) {
	if t == nil {
		return nil, fmt.Errorf("brschema: nil type")
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if e, ok := cache.Load(t); ok {
		entry := e.(cacheEntry)
		return entry.model, entry.err
	}
	m, err := parse(t)
	e, _ := cache.LoadOrStore(t, cacheEntry{model: m, err: err})
	entry := e.(cacheEntry)
	return entry.model, entry.err
}

func parse(t reflect.Type) (*Model, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("brschema: %s is not a struct", t)
	}
	m := &Model{
		Type:   t,
		Name:   t.Name(),
		Fields: jsonFields(t),
		byJSON: map[string]*Field{},
	}
	for _, f := range m.Fields {
		m.byJSON[f.JSONName] = f
	}
	m.PrimaryKey = primaryKey(m.Fields)
	if m.PrimaryKey != nil {
		m.PrimaryKey.PrimaryKey = true
	}
	if err := m.resolveFilterable(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Model) resolveFilterable() error {
	m.filterable = map[string]*Field{}
	zero := reflect.Zero(m.Type).Interface()

	if g, ok := zero.(GenericFilterer); ok && g.GenericFiltering() {
		m.Filterable = slices.Clone(m.Fields)
	} else if ff, ok := zero.(FieldFilterer); ok {
		for _, name := range ff.FilterableFields() {
			f, ok := m.byJSON[name]
			if !ok {
				return fmt.Errorf("brschema: %s.FilterableFields: %q is not a JSON field of %s", m.Name, name, m.Name)
			}
			m.Filterable = append(m.Filterable, f)
		}
	}
	for _, f := range m.Filterable {
		m.filterable[f.JSONName] = f
	}
	return nil
}

// candidate is a field found while walking embedded structs, before name
// conflicts between depths are resolved.
type candidate struct {
	field  *Field
	depth  int
	tagged bool
}

// jsonFields lists the fields encoding/json would encode for t, flattening
// embedded structs with the same precedence rules: the shallowest field wins,
// and at equal depth a single json-tagged field wins, otherwise all are dropped.
func jsonFields(t reflect.Type) []*Field {
	var found []candidate
	var walk func(t reflect.Type, index []int, depth int, seen map[reflect.Type]bool)
	walk = func(t reflect.Type, index []int, depth int, seen map[reflect.Type]bool) {
		if seen[t] {
			return
		}
		seen[t] = true
		defer delete(seen, t)

		for i := range t.NumField() {
			sf := t.Field(i)
			tag := sf.Tag.Get("json")
			if tag == "-" {
				continue
			}
			name, opts, _ := strings.Cut(tag, ",")
			idx := append(slices.Clone(index), i)

			ft := sf.Type
			if ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if sf.Anonymous && name == "" && ft.Kind() == reflect.Struct {
				walk(ft, idx, depth+1, seen)
				continue
			}
			if !sf.IsExported() {
				continue
			}
			found = append(found, candidate{
				field:  newField(sf, idx, name, opts),
				depth:  depth,
				tagged: name != "",
			})
		}
	}
	walk(t, nil, 0, map[reflect.Type]bool{})

	byName := map[string][]candidate{}
	var order []string
	for _, c := range found {
		if _, ok := byName[c.field.JSONName]; !ok {
			order = append(order, c.field.JSONName)
		}
		byName[c.field.JSONName] = append(byName[c.field.JSONName], c)
	}

	fields := make([]*Field, 0, len(order))
	for _, name := range order {
		if f := dominant(byName[name]); f != nil {
			fields = append(fields, f)
		}
	}
	slices.SortStableFunc(fields, func(a, b *Field) int { return slices.Compare(a.Index, b.Index) })
	return fields
}

func dominant(cs []candidate) *Field {
	minDepth := slices.MinFunc(cs, func(a, b candidate) int { return a.depth - b.depth }).depth
	var top []candidate
	for _, c := range cs {
		if c.depth == minDepth {
			top = append(top, c)
		}
	}
	if len(top) == 1 {
		return top[0].field
	}
	var tagged []candidate
	for _, c := range top {
		if c.tagged {
			tagged = append(tagged, c)
		}
	}
	if len(tagged) == 1 {
		return tagged[0].field
	}
	return nil
}

func newField(sf reflect.StructField, index []int, jsonName, jsonOpts string) *Field {
	if jsonName == "" {
		jsonName = sf.Name
	}
	f := &Field{
		GoName:    sf.Name,
		JSONName:  jsonName,
		Column:    column(sf),
		Type:      sf.Type,
		Index:     index,
		Nullable:  sf.Type.Kind() == reflect.Pointer,
		OmitEmpty: hasOption(jsonOpts, "omitempty") || hasOption(jsonOpts, "omitzero"),
		taggedPK:  gormPrimaryKey(sf),
	}
	f.OpenAPIType, f.Format = openAPIType(sf.Type)
	return f
}

func hasOption(opts, want string) bool {
	for opt := range strings.SplitSeq(opts, ",") {
		if opt == want {
			return true
		}
	}
	return false
}

func column(sf reflect.StructField) string {
	for part := range strings.SplitSeq(sf.Tag.Get("gorm"), ";") {
		key, val, ok := strings.Cut(strings.TrimSpace(part), ":")
		if ok && strings.EqualFold(key, "column") && val != "" {
			return val
		}
	}
	return SnakeCase(sf.Name)
}

func primaryKey(fields []*Field) *Field {
	var byName *Field
	for _, f := range fields {
		if f.taggedPK {
			return f
		}
		if f.GoName == "ID" && byName == nil {
			byName = f
		}
	}
	return byName
}

func gormPrimaryKey(sf reflect.StructField) bool {
	for part := range strings.SplitSeq(sf.Tag.Get("gorm"), ";") {
		p := strings.ToLower(strings.TrimSpace(part))
		if p == "primarykey" || p == "primary_key" {
			return true
		}
	}
	return false
}

var timeType = reflect.TypeFor[time.Time]()

func openAPIType(t reflect.Type) (typ, format string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType {
		return "string", "date-time"
	}
	switch t.Kind() {
	case reflect.Bool:
		return "boolean", ""
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return "integer", "int32"
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return "integer", "int64"
	case reflect.Float32:
		return "number", "float"
	case reflect.Float64:
		return "number", "double"
	case reflect.String:
		return "string", ""
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "string", "byte"
		}
		return "array", ""
	case reflect.Array:
		return "array", ""
	default:
		return "object", ""
	}
}

// SnakeCase converts a Go identifier to snake_case the way GORM's default naming
// strategy does, keeping initialisms together: "UserID" → "user_id",
// "HTTPServer" → "http_server", "CreatedAt" → "created_at".
func SnakeCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(s) + 4)
	for i, r := range runes {
		upper := r >= 'A' && r <= 'Z'
		if upper && i > 0 {
			prevLower := isLowerOrDigit(runes[i-1])
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'
			if prevLower || (nextLower && runes[i-1] >= 'A' && runes[i-1] <= 'Z') {
				b.WriteByte('_')
			}
		}
		if upper {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isLowerOrDigit(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}
