// Package sqlstore is a brstore.Store on database/sql. SQL for each model is
// built once when the store is created; per-request work is binding arguments
// and scanning rows. Pick the database with a Dialect (SQLite today).
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jinzhu/inflection"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brschema"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// Store keeps values of T in one table. It is safe for concurrent use.
type Store[T any] struct {
	db      *sql.DB
	dialect Dialect
	model   *brschema.Model
	table   string
	pk      *brschema.Field
	autoPK  bool
	columns map[string]string // JSON name → quoted column

	selectSQL         string // SELECT cols FROM table
	getSQL            string
	insertSQL         string // every column
	insertGeneratedPK string // every column but the key, RETURNING key
	updateSQL         string
	deleteSQL         string
	insertFields      []*brschema.Field
	insertNoPKFields  []*brschema.Field
	updateFields      []*brschema.Field
}

// Option customizes New.
type Option func(*options)

type options struct{ table string }

// WithTable sets the table name. By default it is the plural snake_case of the
// type name, as GORM names tables: Todo → todos, OrderItem → order_items.
func WithTable(name string) Option {
	return func(o *options) { o.table = name }
}

// New returns a Store for T. T needs a primary key, and every JSON field must be
// a scalar (string, bool, integer, float, time.Time, []byte) or a pointer to one.
func New[T any](db *sql.DB, dialect Dialect, opts ...Option) (*Store[T], error) {
	model, err := brschema.Of[T]()
	if err != nil {
		return nil, err
	}
	if model.PrimaryKey == nil {
		return nil, fmt.Errorf("sqlstore: %s has no primary key", model.Name)
	}
	for _, f := range model.Fields {
		if !storable(f.Type) {
			return nil, fmt.Errorf("sqlstore: %s.%s has type %s, which can't be stored in a column", model.Name, f.GoName, f.Type)
		}
	}
	o := options{table: inflection.Plural(brschema.SnakeCase(model.Name))}
	for _, opt := range opts {
		opt(&o)
	}

	s := &Store[T]{
		db:      db,
		dialect: dialect,
		model:   model,
		table:   o.table,
		pk:      model.PrimaryKey,
		autoPK:  isInteger(model.PrimaryKey.Type),
		columns: map[string]string{},
	}
	s.build()
	return s, nil
}

// build precomputes every statement that doesn't depend on a query.
func (s *Store[T]) build() {
	q := s.dialect.Quote
	table := q(s.table)
	pkCol := q(s.pk.Column)

	var all []string
	for _, f := range s.model.Fields {
		col := q(f.Column)
		s.columns[f.JSONName] = col
		all = append(all, col)
		s.insertFields = append(s.insertFields, f)
		if f != s.pk {
			s.insertNoPKFields = append(s.insertNoPKFields, f)
			s.updateFields = append(s.updateFields, f)
		}
	}

	s.selectSQL = "SELECT " + strings.Join(all, ", ") + " FROM " + table
	s.getSQL = s.selectSQL + " WHERE " + pkCol + " = " + s.dialect.Placeholder(1)
	s.deleteSQL = "DELETE FROM " + table + " WHERE " + pkCol + " = " + s.dialect.Placeholder(1)
	s.insertSQL = s.insertStatement(s.insertFields, "")
	s.insertGeneratedPK = s.insertStatement(s.insertNoPKFields, " RETURNING "+pkCol)

	sets := make([]string, len(s.updateFields))
	for i, f := range s.updateFields {
		sets[i] = q(f.Column) + " = " + s.dialect.Placeholder(i+1)
	}
	s.updateSQL = "UPDATE " + table + " SET " + strings.Join(sets, ", ") +
		" WHERE " + pkCol + " = " + s.dialect.Placeholder(len(sets)+1)
}

func (s *Store[T]) insertStatement(fields []*brschema.Field, suffix string) string {
	cols := make([]string, len(fields))
	phs := make([]string, len(fields))
	for i, f := range fields {
		cols[i] = s.dialect.Quote(f.Column)
		phs[i] = s.dialect.Placeholder(i + 1)
	}
	if len(fields) == 0 {
		return "INSERT INTO " + s.dialect.Quote(s.table) + " DEFAULT VALUES" + suffix
	}
	return "INSERT INTO " + s.dialect.Quote(s.table) + " (" + strings.Join(cols, ", ") +
		") VALUES (" + strings.Join(phs, ", ") + ")" + suffix
}

// CreateTable creates the table if it doesn't exist. It's meant for tests and
// prototypes; use a migration tool for production schemas.
func (s *Store[T]) CreateTable(ctx context.Context) error {
	defs := make([]string, 0, len(s.model.Fields))
	for _, f := range s.model.Fields {
		switch {
		case f == s.pk && s.autoPK:
			defs = append(defs, s.dialect.AutoIncrementPK(f.Column))
		case f == s.pk:
			defs = append(defs, s.dialect.Quote(f.Column)+" "+s.dialect.ColumnType(f)+" PRIMARY KEY")
		default:
			defs = append(defs, s.dialect.Quote(f.Column)+" "+s.dialect.ColumnType(f))
		}
	}
	ddl := "CREATE TABLE IF NOT EXISTS " + s.dialect.Quote(s.table) + " (" + strings.Join(defs, ", ") + ")"
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("creating table %s: %w", s.table, err)
	}
	return nil
}

// List returns the rows matching q.
func (s *Store[T]) List(ctx context.Context, q brstore.Query) ([]T, error) {
	query, args, err := s.listSQL(q)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", s.table, err)
	}
	defer rows.Close()

	out := []T{}
	for rows.Next() {
		var v T
		if err := rows.Scan(s.scanDest(&v)...); err != nil {
			return nil, fmt.Errorf("scanning %s: %w", s.table, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing %s: %w", s.table, err)
	}
	return out, nil
}

// Get returns the row with primary key id.
func (s *Store[T]) Get(ctx context.Context, id string) (T, error) {
	var v T
	key, err := brstore.ParseID(s.model, id)
	if err != nil {
		return v, err
	}
	err = s.db.QueryRowContext(ctx, s.getSQL, key).Scan(s.scanDest(&v)...)
	if errors.Is(err, sql.ErrNoRows) {
		return v, fmt.Errorf("%s %s: %w", s.model.Name, id, brerrors.ErrNotFound)
	}
	if err != nil {
		return v, fmt.Errorf("getting %s %s: %w", s.model.Name, id, err)
	}
	return v, nil
}

// Create inserts value. A zero integer key is generated by the database.
func (s *Store[T]) Create(ctx context.Context, value T) (T, error) {
	rv := reflect.ValueOf(&value).Elem()
	pk := fieldAt(rv, s.pk.Index)

	var err error
	if s.autoPK && pk.IsZero() {
		err = s.db.QueryRowContext(ctx, s.insertGeneratedPK, s.args(rv, s.insertNoPKFields)...).
			Scan(pk.Addr().Interface())
	} else {
		_, err = s.db.ExecContext(ctx, s.insertSQL, s.args(rv, s.insertFields)...)
	}
	if err != nil {
		if s.dialect.IsUniqueViolation(err) {
			return value, fmt.Errorf("creating %s: %w", s.model.Name, brerrors.ErrConflict)
		}
		return value, fmt.Errorf("creating %s: %w", s.model.Name, err)
	}
	return value, nil
}

// Update replaces every column of the row with primary key id.
func (s *Store[T]) Update(ctx context.Context, id string, value T) (T, error) {
	key, err := brstore.ParseID(s.model, id)
	if err != nil {
		return value, err
	}
	rv := reflect.ValueOf(&value).Elem()
	pk := fieldAt(rv, s.pk.Index)
	pk.Set(reflect.ValueOf(key).Convert(pk.Type()))

	args := append(s.args(rv, s.updateFields), key)
	res, err := s.db.ExecContext(ctx, s.updateSQL, args...)
	if err != nil {
		if s.dialect.IsUniqueViolation(err) {
			return value, fmt.Errorf("updating %s %s: %w", s.model.Name, id, brerrors.ErrConflict)
		}
		return value, fmt.Errorf("updating %s %s: %w", s.model.Name, id, err)
	}
	return value, s.requireRow(res, id)
}

// Delete removes the row with primary key id.
func (s *Store[T]) Delete(ctx context.Context, id string) error {
	key, err := brstore.ParseID(s.model, id)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, s.deleteSQL, key)
	if err != nil {
		return fmt.Errorf("deleting %s %s: %w", s.model.Name, id, err)
	}
	return s.requireRow(res, id)
}

func (s *Store[T]) requireRow(res sql.Result, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s %s: %w", s.model.Name, id, err)
	}
	if n == 0 {
		return fmt.Errorf("%s %s: %w", s.model.Name, id, brerrors.ErrNotFound)
	}
	return nil
}

// listSQL builds the SELECT for q. Field names are checked against the model,
// so only known, quoted column names reach the SQL text; values are always
// bound as parameters.
func (s *Store[T]) listSQL(q brstore.Query) (string, []any, error) {
	var b strings.Builder
	b.WriteString(s.selectSQL)
	var args []any
	ph := func(v any) string {
		args = append(args, bindValue(v))
		return s.dialect.Placeholder(len(args))
	}

	for i, f := range q.Filters {
		col, ok := s.columns[f.Field]
		if !ok {
			return "", nil, fmt.Errorf("%w: unknown filter field %q", brstore.ErrInvalidQuery, f.Field)
		}
		field, _ := s.model.FieldByJSON(f.Field)
		cond, err := s.condition(col, field, f, ph)
		if err != nil {
			return "", nil, err
		}
		if i == 0 {
			b.WriteString(" WHERE ")
		} else {
			b.WriteString(" AND ")
		}
		b.WriteString(cond)
	}

	b.WriteString(" ORDER BY ")
	for _, srt := range q.Sort {
		col, ok := s.columns[srt.Field]
		if !ok {
			return "", nil, fmt.Errorf("%w: unknown sort field %q", brstore.ErrInvalidQuery, srt.Field)
		}
		b.WriteString(col)
		if srt.Desc {
			b.WriteString(" DESC")
		}
		b.WriteString(", ")
	}
	b.WriteString(s.dialect.Quote(s.pk.Column))

	if q.Limit > 0 {
		b.WriteString(" LIMIT " + ph(int64(q.Limit)))
	} else if q.Offset > 0 {
		// SQL needs a LIMIT before OFFSET; -1 means "no limit" in SQLite.
		b.WriteString(" LIMIT -1")
	}
	if q.Offset > 0 {
		b.WriteString(" OFFSET " + ph(int64(q.Offset)))
	}
	return b.String(), args, nil
}

func (s *Store[T]) condition(col string, field *brschema.Field, f brstore.Filter, ph func(any) string) (string, error) {
	switch f.Op {
	case brstore.Eq:
		return col + " = " + ph(f.Value), nil
	case brstore.Ne:
		// NULL never compares unequal in SQL; the Store contract says a missing
		// value is "not equal", so nullable columns need the explicit IS NULL.
		if field.Nullable {
			return "(" + col + " <> " + ph(f.Value) + " OR " + col + " IS NULL)", nil
		}
		return col + " <> " + ph(f.Value), nil
	case brstore.Gt:
		return col + " > " + ph(f.Value), nil
	case brstore.Gte:
		return col + " >= " + ph(f.Value), nil
	case brstore.Lt:
		return col + " < " + ph(f.Value), nil
	case brstore.Lte:
		return col + " <= " + ph(f.Value), nil
	case brstore.Contains:
		str, ok := f.Value.(string)
		if !ok || field.OpenAPIType != "string" || field.Format != "" {
			return "", fmt.Errorf("%w: contains needs a string field and value (%s)", brstore.ErrInvalidQuery, f.Field)
		}
		return s.dialect.ContainsExpr(col, ph("%"+escapeLike(str)+"%")), nil
	case brstore.In:
		values := reflect.ValueOf(f.Value)
		if values.Kind() != reflect.Slice {
			return "", fmt.Errorf("%w: %s in needs a slice, got %T", brstore.ErrInvalidQuery, f.Field, f.Value)
		}
		if values.Len() == 0 {
			return "1 = 0", nil
		}
		phs := make([]string, values.Len())
		for i := range phs {
			phs[i] = ph(values.Index(i).Interface())
		}
		return col + " IN (" + strings.Join(phs, ", ") + ")", nil
	default:
		return "", fmt.Errorf("%w: unknown operator %q", brstore.ErrInvalidQuery, f.Op)
	}
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// args returns the bind values of fields, in order.
func (s *Store[T]) args(rv reflect.Value, fields []*brschema.Field) []any {
	out := make([]any, len(fields))
	for i, f := range fields {
		v, err := rv.FieldByIndexErr(f.Index)
		if err != nil {
			// The field sits behind a nil embedded pointer: store its zero value.
			// Reading the row back allocates the embedded struct.
			v = reflect.Zero(f.Type)
		}
		out[i] = bindValue(v.Interface())
	}
	return out
}

// bindValue normalizes values before they reach the driver. Times are stored in
// UTC so text-based storage (SQLite) compares and sorts them correctly.
func bindValue(v any) any {
	switch t := v.(type) {
	case time.Time:
		return t.UTC()
	case *time.Time:
		if t == nil {
			return nil
		}
		return t.UTC()
	}
	return v
}

// scanDest returns pointers to v's fields in column order, allocating nil
// embedded struct pointers on the way.
func (s *Store[T]) scanDest(v *T) []any {
	rv := reflect.ValueOf(v).Elem()
	dest := make([]any, len(s.model.Fields))
	for i, f := range s.model.Fields {
		dest[i] = fieldAt(rv, f.Index).Addr().Interface()
	}
	return dest
}

// fieldAt is FieldByIndex that allocates nil embedded pointers instead of
// panicking.
func fieldAt(v reflect.Value, index []int) reflect.Value {
	for i, x := range index {
		if i > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(x)
	}
	return v
}

func storable(t reflect.Type) bool {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeFor[time.Time]() {
		return true
	}
	switch t.Kind() {
	case reflect.Bool, reflect.String, reflect.Float32, reflect.Float64,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	case reflect.Slice:
		return t.Elem().Kind() == reflect.Uint8
	default:
		return false
	}
}

func isInteger(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}
