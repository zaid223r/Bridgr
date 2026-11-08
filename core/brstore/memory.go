package brstore

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brschema"
)

// Memory is an in-process Store, for tests, prototypes and demos. It copies
// values in and out, but slices, maps and pointers inside T are shared with the
// caller. Data is lost when the process exits.
type Memory[T any] struct {
	model *brschema.Model

	mu     sync.RWMutex
	rows   map[string]T
	nextID int64
}

// NewMemory returns an empty Memory store for T. T must have a string or integer
// primary key.
func NewMemory[T any]() (*Memory[T], error) {
	model, err := brschema.Of[T]()
	if err != nil {
		return nil, err
	}
	if model.PrimaryKey == nil {
		return nil, fmt.Errorf("brstore: %s has no primary key", model.Name)
	}
	switch derefType(model.PrimaryKey.Type).Kind() {
	case reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
	default:
		return nil, fmt.Errorf("brstore: %s primary key must be a string or integer", model.Name)
	}
	return &Memory[T]{model: model, rows: map[string]T{}}, nil
}

// List returns the records matching q.
func (m *Memory[T]) List(ctx context.Context, q Query) ([]T, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	filters, err := m.resolveFilters(q.Filters)
	if err != nil {
		return nil, err
	}
	sorts, err := m.resolveSorts(q.Sort)
	if err != nil {
		return nil, err
	}

	out, err := m.matching(filters)
	if err != nil {
		return nil, err
	}

	slices.SortStableFunc(out, func(a, b T) int {
		va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
		for _, s := range sorts {
			c := compareFields(fieldValue(va, s.field), fieldValue(vb, s.field))
			if s.desc {
				c = -c
			}
			if c != 0 {
				return c
			}
		}
		return 0
	})

	start := min(max(q.Offset, 0), len(out))
	out = out[start:]
	if q.Limit > 0 && q.Limit < len(out) {
		out = out[:q.Limit]
	}
	return out, nil
}

// Count returns how many records match q.Filters.
func (m *Memory[T]) Count(ctx context.Context, q Query) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	filters, err := m.resolveFilters(q.Filters)
	if err != nil {
		return 0, err
	}
	out, err := m.matching(filters)
	return int64(len(out)), err
}

func (m *Memory[T]) matching(filters []resolvedFilter) ([]T, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]T, 0, len(m.rows))
	for _, row := range m.rows {
		ok, err := matchesAll(reflect.ValueOf(row), filters)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, row)
		}
	}
	return out, nil
}

// Get returns the record with primary key id.
func (m *Memory[T]) Get(ctx context.Context, id string) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	key, _, err := m.parseKey(id)
	if err != nil {
		return zero, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	row, ok := m.rows[key]
	if !ok {
		return zero, fmt.Errorf("%s %s: %w", m.model.Name, id, brerrors.ErrNotFound)
	}
	return row, nil
}

// Create stores value, assigning the next integer primary key when it is zero.
func (m *Memory[T]) Create(ctx context.Context, value T) (T, error) {
	if err := ctx.Err(); err != nil {
		return value, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	pk := reflect.ValueOf(&value).Elem().FieldByIndex(m.model.PrimaryKey.Index)
	if pk.IsZero() {
		if pk.Kind() == reflect.String {
			return value, fmt.Errorf("brstore: %s primary key %s is empty", m.model.Name, m.model.PrimaryKey.JSONName)
		}
		m.nextID++
		setInt(pk, m.nextID)
	}
	key := keyOf(pk)
	if _, exists := m.rows[key]; exists {
		return value, fmt.Errorf("%s %s already exists: %w", m.model.Name, key, brerrors.ErrConflict)
	}
	if n, err := strconv.ParseInt(key, 10, 64); err == nil && n > m.nextID {
		m.nextID = n
	}
	m.rows[key] = value
	return value, nil
}

// Update replaces the record with primary key id; the key itself comes from id.
func (m *Memory[T]) Update(ctx context.Context, id string, value T) (T, error) {
	if err := ctx.Err(); err != nil {
		return value, err
	}
	key, parsed, err := m.parseKey(id)
	if err != nil {
		return value, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[key]; !ok {
		return value, fmt.Errorf("%s %s: %w", m.model.Name, id, brerrors.ErrNotFound)
	}
	pk := reflect.ValueOf(&value).Elem().FieldByIndex(m.model.PrimaryKey.Index)
	pk.Set(reflect.ValueOf(parsed).Convert(pk.Type()))
	m.rows[key] = value
	return value, nil
}

// Delete removes the record with primary key id.
func (m *Memory[T]) Delete(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key, _, err := m.parseKey(id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[key]; !ok {
		return fmt.Errorf("%s %s: %w", m.model.Name, id, brerrors.ErrNotFound)
	}
	delete(m.rows, key)
	return nil
}

// parseKey returns the map key and typed value for a path id.
func (m *Memory[T]) parseKey(id string) (string, any, error) {
	v, err := ParseID(m.model, id)
	if err != nil {
		return "", nil, err
	}
	return keyOf(reflect.ValueOf(v)), v, nil
}

func keyOf(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	default:
		return strconv.FormatUint(v.Uint(), 10)
	}
}

func setInt(v reflect.Value, n int64) {
	if v.CanInt() {
		v.SetInt(n)
	} else {
		v.SetUint(uint64(n))
	}
}

type resolvedFilter struct {
	field *brschema.Field
	op    Op
	value any
}

type resolvedSort struct {
	field *brschema.Field
	desc  bool
}

func (m *Memory[T]) resolveFilters(fs []Filter) ([]resolvedFilter, error) {
	out := make([]resolvedFilter, 0, len(fs))
	for _, f := range fs {
		field, ok := m.model.FieldByJSON(f.Field)
		if !ok {
			return nil, fmt.Errorf("%w: unknown filter field %q", ErrInvalidQuery, f.Field)
		}
		out = append(out, resolvedFilter{field: field, op: f.Op, value: f.Value})
	}
	return out, nil
}

func (m *Memory[T]) resolveSorts(ss []Sort) ([]resolvedSort, error) {
	out := make([]resolvedSort, 0, len(ss)+1)
	for _, s := range ss {
		field, ok := m.model.FieldByJSON(s.Field)
		if !ok {
			return nil, fmt.Errorf("%w: unknown sort field %q", ErrInvalidQuery, s.Field)
		}
		out = append(out, resolvedSort{field: field, desc: s.Desc})
	}
	// The primary key breaks ties so results are deterministic, as with SQL's
	// ORDER BY ..., pk.
	return append(out, resolvedSort{field: m.model.PrimaryKey}), nil
}

// fieldValue returns the field's value, dereferenced; invalid for a nil pointer
// on the path.
func fieldValue(row reflect.Value, f *brschema.Field) reflect.Value {
	v, err := row.FieldByIndexErr(f.Index)
	if err != nil {
		return reflect.Value{}
	}
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}

func matchesAll(row reflect.Value, filters []resolvedFilter) (bool, error) {
	for _, f := range filters {
		ok, err := matches(fieldValue(row, f.field), f)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func matches(v reflect.Value, f resolvedFilter) (bool, error) {
	if !v.IsValid() {
		// NULL semantics: a nil pointer only satisfies "not equal".
		return f.op == Ne, nil
	}
	switch f.op {
	case In:
		values := reflect.ValueOf(f.value)
		if values.Kind() != reflect.Slice {
			return false, fmt.Errorf("%w: %s in needs a slice, got %T", ErrInvalidQuery, f.field.JSONName, f.value)
		}
		for i := range values.Len() {
			c, err := compareTo(v, values.Index(i).Interface(), f.field)
			if err != nil {
				return false, err
			}
			if c == 0 {
				return true, nil
			}
		}
		return false, nil
	case Contains:
		s, ok := f.value.(string)
		if !ok || v.Kind() != reflect.String {
			return false, fmt.Errorf("%w: contains needs a string field and value (%s)", ErrInvalidQuery, f.field.JSONName)
		}
		return strings.Contains(strings.ToLower(v.String()), strings.ToLower(s)), nil
	}

	c, err := compareTo(v, f.value, f.field)
	if err != nil {
		return false, err
	}
	switch f.op {
	case Eq:
		return c == 0, nil
	case Ne:
		return c != 0, nil
	case Gt:
		return c > 0, nil
	case Gte:
		return c >= 0, nil
	case Lt:
		return c < 0, nil
	case Lte:
		return c <= 0, nil
	default:
		return false, fmt.Errorf("%w: unknown operator %q", ErrInvalidQuery, f.op)
	}
}

// compareTo compares a field value with a filter value of a compatible Go type.
// field is only used in the error message and may be nil.
func compareTo(v reflect.Value, want any, field *brschema.Field) (int, error) {
	w := reflect.ValueOf(want)
	name := v.Type().String()
	if field != nil {
		name = field.JSONName
	}
	mismatch := fmt.Errorf("%w: value %v (%T) doesn't match field %s", ErrInvalidQuery, want, want, name)

	if v.Type() == timeType {
		t, ok := want.(time.Time)
		if !ok {
			return 0, mismatch
		}
		return v.Interface().(time.Time).Compare(t), nil
	}
	switch {
	case v.CanInt() && w.CanInt():
		return cmp.Compare(v.Int(), w.Int()), nil
	case v.CanInt() && w.CanUint():
		if v.Int() < 0 {
			return -1, nil
		}
		return cmp.Compare(uint64(v.Int()), w.Uint()), nil
	case v.CanUint() && w.CanUint():
		return cmp.Compare(v.Uint(), w.Uint()), nil
	case v.CanUint() && w.CanInt():
		if w.Int() < 0 {
			return 1, nil
		}
		return cmp.Compare(v.Uint(), uint64(w.Int())), nil
	case v.CanFloat() && (w.CanFloat() || w.CanInt()):
		return cmp.Compare(v.Float(), toFloat(w)), nil
	case v.Kind() == reflect.String && w.Kind() == reflect.String:
		return cmp.Compare(v.String(), w.String()), nil
	case v.Kind() == reflect.Bool && w.Kind() == reflect.Bool:
		return cmp.Compare(boolInt(v.Bool()), boolInt(w.Bool())), nil
	default:
		return 0, mismatch
	}
}

func toFloat(v reflect.Value) float64 {
	if v.CanInt() {
		return float64(v.Int())
	}
	return v.Float()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// compareFields orders two field values for sorting. Nil sorts first here; the
// Store contract leaves NULL ordering unspecified because SQL databases differ.
func compareFields(a, b reflect.Value) int {
	switch {
	case !a.IsValid() && !b.IsValid():
		return 0
	case !a.IsValid():
		return -1
	case !b.IsValid():
		return 1
	}
	c, err := compareTo(a, b.Interface(), nil)
	if err != nil {
		return 0
	}
	return c
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
