package brcrud_test

import (
	"database/sql"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore/sqlstore"
)

// TestInjectionThroughHTTP sends hostile filter keys, operators and values
// through the full stack into a real SQLite database.
func TestInjectionThroughHTTP(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "crud.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	store, err := sqlstore.New[item](db, sqlstore.SQLite)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTable(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, it := range seedItems {
		if _, err := store.Create(t.Context(), it); err != nil {
			t.Fatal(err)
		}
	}
	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[item](router, "items", store, nil)

	q := func(key, value string) string { return "/items?" + url.Values{key: {value}}.Encode() }
	tests := []struct {
		name       string
		path       string
		wantStatus int
		want       []string
	}{
		{"quote in a value", q("title[eq]", `x' OR '1'='1`), 200, []string{}},
		{"wildcards in contains", q("title[contains]", `%`), 200, []string{}},
		{"underscore in contains", q("title", `_`), 200, []string{}},
		{"SQL in an in-list", q("title[in]", `a') OR (1=1`), 200, []string{}},
		{"comment in a value", q("title", `bolt'; --`), 200, []string{}},
		{"SQL in a field name", q("title; DROP TABLE items", "x"), 400, nil},
		{"SQL in an operator", q("qty[gt) OR (1=1]", "1"), 400, nil},
		{"SQL in a numeric value", q("qty[gt]", "0 OR 1=1"), 400, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := serve(t, router, "GET", tt.path, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if tt.wantStatus == http.StatusOK {
				if got := titles(decode[[]item](t, rec)); !slices.Equal(got, tt.want) {
					t.Errorf("got %v, want %v", got, tt.want)
				}
			}
		})
	}

	all := decode[[]item](t, serve(t, router, "GET", "/items", ""))
	if len(all) != len(seedItems) {
		t.Errorf("table changed: %d rows, want %d", len(all), len(seedItems))
	}
}
