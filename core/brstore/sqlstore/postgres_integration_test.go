//go:build integration

package sqlstore_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brstore"
	"github.com/zaid223r/Bridgr/core/brstore/sqlstore"
	"github.com/zaid223r/Bridgr/core/brstore/storetest"
)

var tableSeq atomic.Int64

// openDSN opens the database named by envVar. It skips the test when the
// variable is unset locally and fails in CI, where it must be set.
func openDSN(t *testing.T, driver, envVar string) *sql.DB {
	t.Helper()
	dsn := os.Getenv(envVar)
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("%s is not set", envVar)
		}
		t.Skipf("%s not set; run `make integration`", envVar)
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatalf("connecting with %s: %v", envVar, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// newDBStore creates a store on its own table and drops the table afterwards.
func newDBStore[T any](t *testing.T, db *sql.DB, d sqlstore.Dialect) *sqlstore.Store[T] {
	t.Helper()
	table := fmt.Sprintf("bridgr_test_%d_%d", os.Getpid(), tableSeq.Add(1))
	s, err := sqlstore.New[T](db, d, sqlstore.WithTable(table))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTable(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// t.Context is cancelled during cleanup, so drop with a fresh context.
		_, _ = db.ExecContext(context.Background(), "DROP TABLE "+d.Quote(table))
	})
	return s
}

func TestPostgresContract(t *testing.T) {
	db := openDSN(t, "pgx", "BRIDGR_POSTGRES_DSN")
	storetest.Run(t, func(t *testing.T) brstore.Store[storetest.Widget] {
		return newDBStore[storetest.Widget](t, db, sqlstore.Postgres)
	})
}

func TestPostgresContainsEscapes(t *testing.T) {
	db := openDSN(t, "pgx", "BRIDGR_POSTGRES_DSN")
	checkContainsEscapes(t, newDBStore[storetest.Widget](t, db, sqlstore.Postgres))
}

func TestPostgresStringKeyAndTypes(t *testing.T) {
	db := openDSN(t, "pgx", "BRIDGR_POSTGRES_DSN")
	s := newDBStore[OrderItem](t, db, sqlstore.Postgres)

	in := OrderItem{Audit: &Audit{CreatedBy: "zaid"}, SKU: "A-1", Qty: 65535, Label: "Box", Payload: []byte{0, 255}}
	if _, err := s.Create(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(t.Context(), in); !errors.Is(err, brerrors.ErrConflict) {
		t.Errorf("duplicate key: err = %v, want ErrConflict", err)
	}
	got, err := s.Get(t.Context(), "A-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Qty != 65535 || got.CreatedBy != "zaid" || got.Shipped != nil || string(got.Payload) != "\x00\xff" {
		t.Errorf("round trip = %+v", got)
	}
}
