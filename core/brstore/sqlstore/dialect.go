package sqlstore

import (
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/zaid223r/Bridgr/core/brschema"
)

// Dialect is the SQL that differs between databases. Bridgr ships SQLite; the
// methods are exported so other databases can be added without forking.
type Dialect interface {
	// Placeholder returns the n-th (1-based) bind parameter: "?" or "$n".
	Placeholder(n int) string
	// Quote quotes an identifier (table or column name).
	Quote(ident string) string
	// ContainsExpr returns a case-insensitive substring match of column against
	// the bind parameter ph. The value is already wrapped in % and escaped with
	// backslashes.
	ContainsExpr(column, ph string) string
	// ColumnType returns the column definition used by CreateTable.
	ColumnType(f *brschema.Field) string
	// AutoIncrementPK returns the full column definition of an integer primary
	// key that the database generates.
	AutoIncrementPK(column string) string
	// IsUniqueViolation reports whether err is a duplicate key error.
	IsUniqueViolation(err error) bool
}

// SQLite is the dialect for SQLite through modernc.org/sqlite (pure Go, no cgo).
//
// SQLite allows one writer at a time. Use db.SetMaxOpenConns(1), or open the
// database with WAL and a busy timeout, e.g.
// "file:app.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)".
var SQLite Dialect = sqliteDialect{}

type sqliteDialect struct{}

func (sqliteDialect) Placeholder(int) string { return "?" }

func (sqliteDialect) Quote(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// ContainsExpr relies on SQLite's LIKE being case-insensitive for ASCII; letters
// outside ASCII are compared case-sensitively.
func (sqliteDialect) ContainsExpr(column, ph string) string {
	return column + " LIKE " + ph + ` ESCAPE '\'`
}

func (sqliteDialect) ColumnType(f *brschema.Field) string {
	t := f.Type
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	notNull := " NOT NULL"
	if f.Nullable {
		notNull = ""
	}
	// DATETIME and BOOLEAN matter beyond documentation: modernc.org/sqlite uses
	// the declared type to scan values back as time.Time and bool.
	switch {
	case t == reflect.TypeFor[time.Time]():
		return "DATETIME" + notNull
	case t.Kind() == reflect.Bool:
		return "BOOLEAN" + notNull
	case t.Kind() == reflect.String:
		return "TEXT" + notNull
	case t.Kind() == reflect.Slice:
		return "BLOB" // nil []byte is NULL, so byte columns are always nullable
	case t.Kind() == reflect.Float32 || t.Kind() == reflect.Float64:
		return "REAL" + notNull
	default:
		return "INTEGER" + notNull
	}
}

func (d sqliteDialect) AutoIncrementPK(column string) string {
	return d.Quote(column) + " INTEGER PRIMARY KEY AUTOINCREMENT"
}

// sqliteCoder matches modernc.org/sqlite's *sqlite.Error without importing the
// driver, so applications choose the driver version.
type sqliteCoder interface {
	error
	Code() int
}

const (
	sqliteConstraintPrimaryKey = 1555
	sqliteConstraintUnique     = 2067
)

func (sqliteDialect) IsUniqueViolation(err error) bool {
	coder, ok := errors.AsType[sqliteCoder](err)
	if !ok {
		return false
	}
	code := coder.Code()
	return code == sqliteConstraintPrimaryKey || code == sqliteConstraintUnique
}
