package sqlstore

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zaid223r/Bridgr/core/brschema"
)

type columnTypes struct {
	I8    int8       `json:"i8"`
	I32   int32      `json:"i32"`
	I64   int64      `json:"i64"`
	U64   uint64     `json:"u64"`
	F32   float32    `json:"f32"`
	F64   float64    `json:"f64"`
	B     bool       `json:"b"`
	S     string     `json:"s"`
	T     time.Time  `json:"t"`
	PT    *time.Time `json:"pt"`
	Bytes []byte     `json:"bytes"`
}

func TestColumnTypes(t *testing.T) {
	t.Parallel()
	m, err := brschema.Of[columnTypes]()
	if err != nil {
		t.Fatal(err)
	}
	want := map[Dialect]map[string]string{
		SQLite: {
			"i8": "INTEGER NOT NULL", "u64": "INTEGER NOT NULL", "f64": "REAL NOT NULL",
			"b": "BOOLEAN NOT NULL", "s": "TEXT NOT NULL", "t": "DATETIME NOT NULL",
			"pt": "DATETIME", "bytes": "BLOB",
		},
		Postgres: {
			"i8": "SMALLINT NOT NULL", "i32": "INTEGER NOT NULL", "i64": "BIGINT NOT NULL",
			"u64": "NUMERIC(20, 0) NOT NULL", "f32": "REAL NOT NULL", "f64": "DOUBLE PRECISION NOT NULL",
			"b": "BOOLEAN NOT NULL", "s": "TEXT NOT NULL", "t": "TIMESTAMPTZ NOT NULL",
			"pt": "TIMESTAMPTZ", "bytes": "BYTEA",
		},
	}
	for d, cols := range want {
		for name, wantType := range cols {
			f, _ := m.FieldByJSON(name)
			if got := d.ColumnType(f); got != wantType {
				t.Errorf("%T %s = %q, want %q", d, name, got, wantType)
			}
		}
	}
}

func TestPostgresSQL(t *testing.T) {
	t.Parallel()
	d := Postgres
	if d.Placeholder(3) != "$3" || d.Quote(`we"ird`) != `"we""ird"` || d.NoLimit() != "" {
		t.Error("placeholder, quoting or NoLimit wrong")
	}
	if got := d.ContainsExpr(`"name"`, "$1"); got != `"name" ILIKE $1 ESCAPE '\'` {
		t.Errorf("ContainsExpr = %s", got)
	}
	query, args := d.SyncSequence("widgets", "id")
	want := `SELECT setval(pg_get_serial_sequence($1, $2), GREATEST((SELECT MAX("id") FROM "widgets"), 1))`
	if query != want || fmt.Sprint(args) != `["widgets" id]` {
		t.Errorf("SyncSequence = %s %v", query, args)
	}
	if q, _ := SQLite.SyncSequence("widgets", "id"); q != "" || SQLite.NoLimit() != "LIMIT -1" {
		t.Error("SQLite should not sync sequences and needs LIMIT -1")
	}
}

type fakePgError struct{ code string }

func (e fakePgError) Error() string    { return "pg: " + e.code }
func (e fakePgError) SQLState() string { return e.code }

type fakeSQLiteError struct{ code int }

func (e fakeSQLiteError) Error() string { return fmt.Sprint("sqlite: ", e.code) }
func (e fakeSQLiteError) Code() int     { return e.code }

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		d    Dialect
		err  error
		want bool
	}{
		{Postgres, fmt.Errorf("insert: %w", fakePgError{"23505"}), true},
		{Postgres, fakePgError{"23503"}, false}, // foreign key, not unique
		{Postgres, errors.New("duplicate key"), false},
		{SQLite, fmt.Errorf("insert: %w", fakeSQLiteError{2067}), true},
		{SQLite, fakeSQLiteError{1555}, true},
		{SQLite, fakeSQLiteError{1299}, false}, // NOT NULL
	}
	for _, tt := range tests {
		if got := tt.d.IsUniqueViolation(tt.err); got != tt.want {
			t.Errorf("%T.IsUniqueViolation(%v) = %v, want %v", tt.d, tt.err, got, tt.want)
		}
	}
}
