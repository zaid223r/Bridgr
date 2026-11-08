// Package mysql is the sqlstore dialect for MySQL 8 through
// github.com/go-sql-driver/mysql:
//
//	db, err := sql.Open("mysql", "user:pass@tcp(host:3306)/db?parseTime=true")
//	todos, err := sqlstore.New[Todo](db, mysql.Dialect)
//
// The DSN must set parseTime=true so DATETIME columns scan into time.Time; times
// are stored in UTC. Text columns created by CreateTable use the utf8mb4_bin
// collation so eq is case-sensitive like on SQLite and Postgres; contains
// compares with utf8mb4_0900_ai_ci, which ignores case and accents.
//
// It lives in its own package because recognising duplicate keys needs the
// driver's error type.
package mysql

import (
	"errors"
	"reflect"
	"strings"
	"time"

	driver "github.com/go-sql-driver/mysql"

	"github.com/zaid223r/Bridgr/core/brschema"
	"github.com/zaid223r/Bridgr/core/brstore/sqlstore"
)

// Dialect is the MySQL dialect.
var Dialect sqlstore.Dialect = dialect{}

type dialect struct{}

const erDupEntry = 1062

func (dialect) Placeholder(int) string { return "?" }

func (dialect) Quote(ident string) string {
	return "`" + strings.ReplaceAll(ident, "`", "``") + "`"
}

// ContainsExpr overrides the column's binary collation for this comparison only.
// LIKE's default escape character in MySQL is the backslash.
func (dialect) ContainsExpr(column, ph string) string {
	return column + " LIKE " + ph + " COLLATE utf8mb4_0900_ai_ci"
}

func (dialect) ColumnType(f *brschema.Field) string {
	t := f.Type
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	notNull := " NOT NULL"
	if f.Nullable {
		notNull = ""
	}
	const text = " CHARACTER SET utf8mb4 COLLATE utf8mb4_bin"
	switch {
	case t == reflect.TypeFor[time.Time]():
		return "DATETIME(6)" + notNull
	case t.Kind() == reflect.Slice:
		return "LONGBLOB" // nil []byte is NULL, so byte columns are always nullable
	case t.Kind() == reflect.String && f.PrimaryKey:
		return "VARCHAR(255)" + text + notNull // TEXT can't be a primary key
	}
	switch t.Kind() {
	case reflect.Bool:
		return "BOOLEAN" + notNull
	case reflect.String:
		return "TEXT" + text + notNull
	case reflect.Float32:
		return "FLOAT" + notNull
	case reflect.Float64:
		return "DOUBLE" + notNull
	case reflect.Int8:
		return "TINYINT" + notNull
	case reflect.Uint8:
		return "TINYINT UNSIGNED" + notNull
	case reflect.Int16:
		return "SMALLINT" + notNull
	case reflect.Uint16:
		return "SMALLINT UNSIGNED" + notNull
	case reflect.Int32:
		return "INT" + notNull
	case reflect.Uint32:
		return "INT UNSIGNED" + notNull
	case reflect.Uint, reflect.Uint64:
		return "BIGINT UNSIGNED" + notNull
	default:
		return "BIGINT" + notNull
	}
}

func (d dialect) AutoIncrementPK(column string) string {
	return d.Quote(column) + " BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY"
}

func (dialect) IsUniqueViolation(err error) bool {
	e, ok := errors.AsType[*driver.MySQLError](err)
	return ok && e.Number == erDupEntry
}

// NoLimit is MySQL's documented way to say "all rows" before an OFFSET.
func (dialect) NoLimit() string { return "LIMIT 18446744073709551615" }

func (dialect) SyncSequence(string, string) (string, []any) { return "", nil }

func (dialect) Returning() bool { return false }
