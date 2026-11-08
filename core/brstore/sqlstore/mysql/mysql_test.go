package mysql

import (
	"errors"
	"fmt"
	"testing"
	"time"

	driver "github.com/go-sql-driver/mysql"

	"github.com/zaid223r/Bridgr/core/brschema"
)

type columns struct {
	Code  string     `json:"code" db:",pk"`
	Name  string     `json:"name"`
	Note  *string    `json:"note"`
	I8    int8       `json:"i8"`
	U8    uint8      `json:"u8"`
	I32   int32      `json:"i32"`
	U64   uint64     `json:"u64"`
	F64   float64    `json:"f64"`
	B     bool       `json:"b"`
	T     time.Time  `json:"t"`
	PT    *time.Time `json:"pt"`
	Bytes []byte     `json:"bytes"`
}

func TestColumnTypes(t *testing.T) {
	t.Parallel()
	m, err := brschema.Of[columns]()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"code":  "VARCHAR(255) CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL",
		"name":  "TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin NOT NULL",
		"note":  "TEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_bin",
		"i8":    "TINYINT NOT NULL",
		"u8":    "TINYINT UNSIGNED NOT NULL",
		"i32":   "INT NOT NULL",
		"u64":   "BIGINT UNSIGNED NOT NULL",
		"f64":   "DOUBLE NOT NULL",
		"b":     "BOOLEAN NOT NULL",
		"t":     "DATETIME(6) NOT NULL",
		"pt":    "DATETIME(6)",
		"bytes": "LONGBLOB",
	}
	for name, w := range want {
		f, _ := m.FieldByJSON(name)
		if got := Dialect.ColumnType(f); got != w {
			t.Errorf("%s = %q, want %q", name, got, w)
		}
	}
}

func TestSQL(t *testing.T) {
	t.Parallel()
	d := Dialect
	if d.Placeholder(4) != "?" || d.Quote("we`ird") != "`we``ird`" || d.Returning() {
		t.Error("placeholder, quoting or Returning wrong")
	}
	if got := d.ContainsExpr("`name`", "?"); got != "`name` LIKE ? COLLATE utf8mb4_0900_ai_ci" {
		t.Errorf("ContainsExpr = %s", got)
	}
	if got := d.AutoIncrementPK("id"); got != "`id` BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY" {
		t.Errorf("AutoIncrementPK = %s", got)
	}
	if q, _ := d.SyncSequence("t", "id"); q != "" || d.NoLimit() != "LIMIT 18446744073709551615" {
		t.Error("SyncSequence or NoLimit wrong")
	}
}

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err  error
		want bool
	}{
		{fmt.Errorf("insert: %w", &driver.MySQLError{Number: 1062}), true},
		{&driver.MySQLError{Number: 1048}, false}, // column cannot be null
		{errors.New("Duplicate entry"), false},
	}
	for _, tt := range tests {
		if got := Dialect.IsUniqueViolation(tt.err); got != tt.want {
			t.Errorf("IsUniqueViolation(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}
