package bridgr_test

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	bridgr "github.com/zaid223r/Bridgr"
	"github.com/zaid223r/Bridgr/core/brstore"
	"github.com/zaid223r/Bridgr/core/brstore/sqlstore"
)

type Note struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestDatabaseStoresEveryResource(t *testing.T) {
	t.Parallel()
	for name, db := range map[string]func(t *testing.T) *bridgr.Database{
		"memory": func(*testing.T) *bridgr.Database { return bridgr.Memory() },
		"sqlite": func(t *testing.T) *bridgr.Database { return bridgr.SQL(openSQLite(t), sqlstore.SQLite) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			app := bridgr.New(bridgr.Config{Logger: quiet(), DB: db(t)})
			bridgr.CRUD[Todo](app, "todos", nil)
			bridgr.CRUD[Note](app, "notes", nil)
			if err := app.CreateTables(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := app.CreateTables(t.Context()); err != nil {
				t.Fatalf("second CreateTables = %v, want existing tables skipped", err)
			}
			h := app.Handler()

			if rec := do(t, h, "POST", "/todos", `{"title":"ship"}`); rec.Code != http.StatusCreated {
				t.Fatalf("create todo: %d %s", rec.Code, rec.Body)
			}
			if rec := do(t, h, "POST", "/notes", `{"body":"hi"}`); rec.Code != http.StatusCreated {
				t.Fatalf("create note: %d %s", rec.Code, rec.Body)
			}
			if rec := do(t, h, "GET", "/todos/1", ""); !strings.Contains(rec.Body.String(), `"title":"ship"`) {
				t.Errorf("GET /todos/1 = %d %s", rec.Code, rec.Body)
			}
			if rec := do(t, h, "GET", "/notes", ""); rec.Header().Get("X-Total-Count") != "1" {
				t.Errorf("notes total = %q, want 1: resources must not share storage", rec.Header().Get("X-Total-Count"))
			}
		})
	}
}

func TestSQLDatabaseNamesTablesAfterModels(t *testing.T) {
	t.Parallel()
	db := openSQLite(t)
	app := bridgr.New(bridgr.Config{Logger: quiet(), DB: bridgr.SQL(db, sqlstore.SQLite)})
	bridgr.CRUD[Note](app, "notes", nil)
	if err := app.CreateTables(t.Context()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
		t.Fatalf("table notes: %v", err)
	}
}

func TestStoreOptionOverridesDatabase(t *testing.T) {
	t.Parallel()
	seeded, err := brstore.NewMemory[Todo]()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seeded.Create(t.Context(), Todo{Title: "seeded"}); err != nil {
		t.Fatal(err)
	}
	for name, cfg := range map[string]bridgr.Config{
		"with a database": {Logger: quiet(), DB: bridgr.Memory()},
		"without one":     {Logger: quiet()},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			app := bridgr.New(cfg)
			bridgr.CRUD[Todo](app, "todos", &bridgr.CRUDOptions[Todo]{Store: seeded})
			if rec := do(t, app.Handler(), "GET", "/todos/1", ""); !strings.Contains(rec.Body.String(), "seeded") {
				t.Errorf("GET /todos/1 = %d %s, want the seeded store", rec.Code, rec.Body)
			}
		})
	}
}

func TestCRUDWithoutStorePanics(t *testing.T) {
	t.Parallel()
	type Map struct {
		ID    int64             `json:"id"`
		Attrs map[string]string `json:"attrs"`
	}
	tests := []struct {
		name     string
		register func()
		want     string
	}{
		{"no database", func() {
			bridgr.CRUD[Todo](bridgr.New(bridgr.Config{}), "todos", nil)
		}, "set Config.DB or CRUDOptions.Store"},
		{"model the database can't store", func() {
			app := bridgr.New(bridgr.Config{DB: bridgr.SQL(openSQLite(t), sqlstore.SQLite)})
			bridgr.CRUD[Map](app, "maps", nil)
		}, "can't be stored in a column"},
		{"SQL without a db", func() { bridgr.SQL(nil, sqlstore.SQLite) }, "db and dialect are required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), tt.want) {
					t.Errorf("recover() = %v, want a panic mentioning %q", r, tt.want)
				}
			}()
			tt.register()
		})
	}
}

func TestServeCreatesTablesWhenAsked(t *testing.T) {
	t.Parallel()
	db := openSQLite(t)
	app := bridgr.New(bridgr.Config{Logger: quiet(), DB: bridgr.SQL(db, sqlstore.SQLite), CreateTables: true})
	bridgr.CRUD[Note](app, "notes", nil)

	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, ln) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve = %v", err)
		}
	}()

	resp, err := http.Post("http://"+ln.Addr().String()+"/notes", "application/json", strings.NewReader(`{"body":"x"}`)) //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("POST /notes = %d, want the table created before serving", resp.StatusCode)
	}
}

func TestServeFailsWhenTablesCannotBeCreated(t *testing.T) {
	t.Parallel()
	db := openSQLite(t)
	app := bridgr.New(bridgr.Config{Logger: quiet(), DB: bridgr.SQL(db, sqlstore.SQLite), CreateTables: true})
	bridgr.CRUD[Note](app, "notes", nil)
	_ = db.Close()

	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Serve(t.Context(), ln); err == nil || !strings.Contains(err.Error(), "creating table notes") {
		t.Errorf("Serve = %v, want the table error", err)
	}
	if _, err := ln.Accept(); err == nil {
		t.Error("Serve left the listener open")
	}
}
