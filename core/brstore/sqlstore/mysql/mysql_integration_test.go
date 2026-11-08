//go:build integration

package mysql_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brstore"
	"github.com/zaid223r/Bridgr/core/brstore/sqlstore"
	"github.com/zaid223r/Bridgr/core/brstore/sqlstore/mysql"
	"github.com/zaid223r/Bridgr/core/brstore/storetest"
)

var tableSeq atomic.Int64

func openMySQL(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("BRIDGR_MYSQL_DSN")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("BRIDGR_MYSQL_DSN is not set")
		}
		t.Skip("BRIDGR_MYSQL_DSN not set; run `make integration`")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("connecting to MySQL: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newStore[T any](t *testing.T, db *sql.DB) *sqlstore.Store[T] {
	t.Helper()
	table := fmt.Sprintf("bridgr_test_%d_%d", os.Getpid(), tableSeq.Add(1))
	s, err := sqlstore.New[T](db, mysql.Dialect, sqlstore.WithTable(table))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTable(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), "DROP TABLE "+mysql.Dialect.Quote(table))
	})
	return s
}

func TestMySQLContract(t *testing.T) {
	db := openMySQL(t)
	storetest.Run(t, func(t *testing.T) brstore.Store[storetest.Widget] {
		return newStore[storetest.Widget](t, db)
	})
}

func TestMySQLContainsAndCase(t *testing.T) {
	db := openMySQL(t)
	s := newStore[storetest.Widget](t, db)
	for _, name := range []string{"100% Cotton", "under_score", "underXscore", `back\slash`, "Crème"} {
		if _, err := s.Create(t.Context(), storetest.Widget{Name: name, Released: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		op    brstore.Op
		value string
		want  string
	}{
		{brstore.Contains, "%", "[100% Cotton]"},
		{brstore.Contains, "_", "[under_score]"},
		{brstore.Contains, `\`, `[back\slash]`},
		{brstore.Contains, "cotton", "[100% Cotton]"},
		{brstore.Contains, "creme", "[Crème]"}, // accent-insensitive, as documented
		{brstore.Eq, "100% cotton", "[]"},      // eq is case-sensitive
		{brstore.Eq, "100% Cotton", "[100% Cotton]"},
	}
	for _, tt := range tests {
		got, err := s.List(t.Context(), brstore.Query{Filters: []brstore.Filter{{Field: "name", Op: tt.op, Value: tt.value}}})
		if err != nil {
			t.Fatal(err)
		}
		names := []string{}
		for _, w := range got {
			names = append(names, w.Name)
		}
		if fmt.Sprint(names) != tt.want {
			t.Errorf("%s %q = %v, want %s", tt.op, tt.value, names, tt.want)
		}
	}
}

type sku struct {
	Code  string `json:"code" db:",pk"`
	Label string `json:"label"`
	Qty   uint16 `json:"qty"`
}

func TestMySQLStringKey(t *testing.T) {
	db := openMySQL(t)
	s := newStore[sku](t, db)
	if _, err := s.Create(t.Context(), sku{Code: "A-1", Label: "Box", Qty: 65535}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(t.Context(), sku{Code: "A-1"}); !errors.Is(err, brerrors.ErrConflict) {
		t.Errorf("duplicate key: err = %v, want ErrConflict", err)
	}
	if _, err := s.Create(t.Context(), sku{Code: "a-1"}); err != nil {
		t.Errorf("keys should be case-sensitive (utf8mb4_bin): %v", err)
	}
	got, err := s.Get(t.Context(), "A-1")
	if err != nil || got.Qty != 65535 || got.Label != "Box" {
		t.Errorf("round trip = %+v, %v", got, err)
	}
}
