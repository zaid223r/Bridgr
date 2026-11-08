package sqlstore_test

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brstore"
	"github.com/zaid223r/Bridgr/core/brstore/sqlstore"
	"github.com/zaid223r/Bridgr/core/brstore/storetest"
)

// openSQLite returns a fresh file-backed database, closed when the test ends.
func openSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newStore[T any](t *testing.T, opts ...sqlstore.Option) (*sqlstore.Store[T], *sql.DB) {
	t.Helper()
	db := openSQLite(t)
	s, err := sqlstore.New[T](db, sqlstore.SQLite, opts...)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTable(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s, db
}

func TestSQLiteContract(t *testing.T) {
	t.Parallel()
	storetest.Run(t, func(t *testing.T) brstore.Store[storetest.Widget] {
		s, _ := newStore[storetest.Widget](t)
		return s
	})
}

type Audit struct {
	CreatedBy string `json:"created_by"`
}

type OrderItem struct {
	*Audit
	SKU      string     `json:"sku" gorm:"primaryKey"`
	Qty      uint16     `json:"qty"`
	Label    string     `json:"label" gorm:"column:display_label"`
	Shipped  *time.Time `json:"shipped"`
	Payload  []byte     `json:"payload"`
	internal int        //nolint:unused // proves unexported fields are not columns
}

func TestNamingAndStringKeys(t *testing.T) {
	t.Parallel()
	s, db := newStore[OrderItem](t)

	var table string
	if err := db.QueryRowContext(t.Context(), `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table != "order_items" {
		t.Errorf("table = %q, want order_items", table)
	}

	shipped := time.Date(2026, 5, 1, 12, 0, 0, 0, time.FixedZone("UTC+3", 3*3600))
	in := OrderItem{Audit: &Audit{CreatedBy: "zaid"}, SKU: "A-1", Qty: 3, Label: "Box", Shipped: &shipped, Payload: []byte{1, 2}}
	if _, err := s.Create(t.Context(), in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(t.Context(), in); !errors.Is(err, brerrors.ErrConflict) {
		t.Errorf("duplicate string key: err = %v, want ErrConflict", err)
	}

	got, err := s.Get(t.Context(), "A-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Audit == nil || got.CreatedBy != "zaid" || got.Label != "Box" || got.Qty != 3 ||
		!got.Shipped.Equal(shipped) || string(got.Payload) != "\x01\x02" {
		t.Errorf("round trip = %+v (audit %+v)", got, got.Audit)
	}

	var label string
	if err := db.QueryRowContext(t.Context(), `SELECT display_label FROM order_items`).Scan(&label); err != nil || label != "Box" {
		t.Errorf("gorm column tag not used: %q, %v", label, err)
	}

	// A nil embedded pointer is stored as zero values instead of panicking.
	if _, err := s.Create(t.Context(), OrderItem{SKU: "B-2"}); err != nil {
		t.Errorf("create with nil embedded pointer: %v", err)
	}
}

func TestWithTable(t *testing.T) {
	t.Parallel()
	s, db := newStore[storetest.Widget](t, sqlstore.WithTable("gadgets"))
	if _, err := s.Create(t.Context(), storetest.Widget{Name: "x", Released: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM gadgets`).Scan(&n); err != nil || n != 1 {
		t.Errorf("rows in gadgets = %d, %v", n, err)
	}
}

func TestContainsEscapesWildcards(t *testing.T) {
	t.Parallel()
	s, _ := newStore[storetest.Widget](t)
	for _, name := range []string{"100% cotton", "100 percent", "under_score", "underXscore", `back\slash`} {
		if _, err := s.Create(t.Context(), storetest.Widget{Name: name, Released: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	tests := map[string][]string{
		"%":  {"100% cotton"},
		"_":  {"under_score"},
		`\`:  {`back\slash`},
		"0%": {"100% cotton"},
	}
	for needle, want := range tests {
		got, err := s.List(t.Context(), brstore.Query{Filters: []brstore.Filter{{Field: "name", Op: brstore.Contains, Value: needle}}})
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, w := range got {
			names = append(names, w.Name)
		}
		if fmt.Sprint(names) != fmt.Sprint(want) {
			t.Errorf("contains %q = %v, want %v", needle, names, want)
		}
	}
}

func TestInjectionStaysData(t *testing.T) {
	t.Parallel()
	s, db := newStore[storetest.Widget](t)
	if _, err := s.Create(t.Context(), storetest.Widget{Name: "safe", Released: time.Now()}); err != nil {
		t.Fatal(err)
	}
	evil := `x'; DROP TABLE widgets; --`
	if _, err := s.List(t.Context(), brstore.Query{Filters: []brstore.Filter{{Field: "name", Op: brstore.Eq, Value: evil}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(t.Context(), brstore.Query{Filters: []brstore.Filter{{Field: "name; DROP TABLE widgets", Op: brstore.Eq, Value: "x"}}}); !errors.Is(err, brstore.ErrInvalidQuery) {
		t.Errorf("unknown field: err = %v, want ErrInvalidQuery", err)
	}
	var n int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM widgets`).Scan(&n); err != nil || n != 1 {
		t.Errorf("table damaged: count = %d, err = %v", n, err)
	}
}

func TestNewRejectsUnsupportedModels(t *testing.T) {
	t.Parallel()
	db := openSQLite(t)
	type noPK struct {
		Name string `json:"name"`
	}
	type withSlice struct {
		ID   int      `json:"id"`
		Tags []string `json:"tags"`
	}
	if _, err := sqlstore.New[noPK](db, sqlstore.SQLite); err == nil || !strings.Contains(err.Error(), "no primary key") {
		t.Errorf("no primary key: %v", err)
	}
	if _, err := sqlstore.New[withSlice](db, sqlstore.SQLite); err == nil || !strings.Contains(err.Error(), "Tags") {
		t.Errorf("slice field: %v", err)
	}
}

func BenchmarkGet(b *testing.B) {
	db, err := sql.Open("sqlite", filepath.Join(b.TempDir(), "bench.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	s, err := sqlstore.New[storetest.Widget](db, sqlstore.SQLite)
	if err != nil {
		b.Fatal(err)
	}
	if err := s.CreateTable(b.Context()); err != nil {
		b.Fatal(err)
	}
	w, err := s.Create(b.Context(), storetest.Widget{Name: "bench", Price: 1, Released: time.Now()})
	if err != nil {
		b.Fatal(err)
	}
	id := fmt.Sprint(w.ID)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := s.Get(b.Context(), id); err != nil {
			b.Fatal(err)
		}
	}
}
