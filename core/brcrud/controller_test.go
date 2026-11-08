package brcrud_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
	brerrors "github.com/zaid223r/Bridgr/errors"
)

type item struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

var errNotFound = errors.New("not found")

// memModel is an in-memory brcrud.BridgrModel that also records the arguments
// it was called with, so tests can check what the handlers passed through.
type memModel struct {
	mu          sync.Mutex
	items       map[string]item
	nextID      int
	lastFilters map[string][]string
	lastID      string
}

func newMemModel(seed ...item) *memModel {
	m := &memModel{items: map[string]item{}, nextID: 1}
	for _, it := range seed {
		m.items[strconv.Itoa(it.ID)] = it
		m.nextID = max(m.nextID, it.ID+1)
	}
	return m
}

func (m *memModel) List(filters map[string][]string) ([]item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastFilters = filters
	for key := range filters {
		if key != "title" {
			return nil, &brerrors.InvalidFilterFieldError{Fields: []string{key}}
		}
	}
	keys := slices.Sorted(maps.Keys(m.items))
	out := make([]item, 0, len(keys))
	for _, k := range keys {
		out = append(out, m.items[k])
	}
	return out, nil
}

func (m *memModel) Get(id string) (item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastID = id
	it, ok := m.items[id]
	if !ok {
		return item{}, errNotFound
	}
	return it, nil
}

func (m *memModel) Create(input item) (item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	input.ID = m.nextID
	m.nextID++
	m.items[strconv.Itoa(input.ID)] = input
	return input, nil
}

func (m *memModel) Update(id string, input item) (item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastID = id
	if _, ok := m.items[id]; !ok {
		return item{}, errNotFound
	}
	input.ID, _ = strconv.Atoi(id)
	m.items[id] = input
	return input, nil
}

func (m *memModel) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastID = id
	delete(m.items, id)
	return nil
}

func serve(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decoding body %q: %v", rec.Body.String(), err)
	}
	return v
}

func newServer(t *testing.T, model *memModel, opts *brcrud.BridgrOptions[item]) *brhttp.Router {
	t.Helper()
	router := brhttp.NewRouter()
	brcrud.RegisterCRUDRoutes[item](router, "items", model, opts)
	return router
}

func TestCRUDRoutes(t *testing.T) {
	t.Parallel()

	t.Run("list", func(t *testing.T) {
		t.Parallel()
		model := newMemModel(item{ID: 1, Title: "a"}, item{ID: 2, Title: "b"})
		rec := serve(t, newServer(t, model, nil), "GET", "/items?title=a", "")

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if got := decode[[]item](t, rec); len(got) != 2 {
			t.Errorf("got %d items, want 2", len(got))
		}
		if got := model.lastFilters["title"]; !slices.Equal(got, []string{"a"}) {
			t.Errorf("filters passed to model = %v, want [a]", got)
		}
	})

	t.Run("list with invalid filter", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, newServer(t, newMemModel(), nil), "GET", "/items?secret=x", "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("create", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, newServer(t, newMemModel(), nil), "POST", "/items", `{"title":"new"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201", rec.Code)
		}
		if got := decode[item](t, rec); got.ID != 1 || got.Title != "new" {
			t.Errorf("created = %+v, want {ID:1 Title:new}", got)
		}
	})

	t.Run("create with malformed JSON", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, newServer(t, newMemModel(), nil), "POST", "/items", `{"title":`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("get", func(t *testing.T) {
		t.Parallel()
		model := newMemModel(item{ID: 7, Title: "seven"})
		rec := serve(t, newServer(t, model, nil), "GET", "/items/7", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := decode[item](t, rec); got.Title != "seven" {
			t.Errorf("title = %q, want seven", got.Title)
		}
		if model.lastID != "7" {
			t.Errorf("id passed to model = %q, want 7", model.lastID)
		}
	})

	t.Run("get missing", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, newServer(t, newMemModel(), nil), "GET", "/items/99", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})

	t.Run("update", func(t *testing.T) {
		t.Parallel()
		model := newMemModel(item{ID: 3, Title: "old"})
		rec := serve(t, newServer(t, model, nil), "PUT", "/items/3", `{"title":"new"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := decode[item](t, rec); got.ID != 3 || got.Title != "new" {
			t.Errorf("updated = %+v, want {ID:3 Title:new}", got)
		}
		if model.lastID != "3" {
			t.Errorf("id passed to model = %q, want 3", model.lastID)
		}
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()
		model := newMemModel(item{ID: 4})
		rec := serve(t, newServer(t, model, nil), "DELETE", "/items/4", "")
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want 204", rec.Code)
		}
		if model.lastID != "4" {
			t.Errorf("id passed to model = %q, want 4", model.lastID)
		}
	})

	t.Run("unsupported method", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, newServer(t, newMemModel(), nil), "PATCH", "/items/1", `{}`)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
		if allow := rec.Header().Get("Allow"); allow != "DELETE, GET, HEAD, PUT" {
			t.Errorf("Allow = %q", allow)
		}
	})

	t.Run("trailing slash", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, newServer(t, newMemModel(), nil), "GET", "/items/", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestCRUDOptions(t *testing.T) {
	t.Parallel()

	t.Run("auth rejects", func(t *testing.T) {
		t.Parallel()
		opts := &brcrud.BridgrOptions[item]{Auth: func(*http.Request) bool { return false }}
		router := newServer(t, newMemModel(item{ID: 1}), opts)
		for _, rt := range []struct{ method, path string }{
			{"GET", "/items"}, {"POST", "/items"}, {"GET", "/items/1"}, {"PUT", "/items/1"}, {"DELETE", "/items/1"},
		} {
			if rec := serve(t, router, rt.method, rt.path, `{}`); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s: status = %d, want 403", rt.method, rt.path, rec.Code)
			}
		}
	})

	t.Run("validation fails", func(t *testing.T) {
		t.Parallel()
		opts := &brcrud.BridgrOptions[item]{Validate: func(in item) error {
			if in.Title == "" {
				return errors.New("title is required")
			}
			return nil
		}}
		router := newServer(t, newMemModel(item{ID: 1}), opts)
		for _, rt := range []struct{ method, path string }{{"POST", "/items"}, {"PUT", "/items/1"}} {
			if rec := serve(t, router, rt.method, rt.path, `{}`); rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s %s: status = %d, want 422", rt.method, rt.path, rec.Code)
			}
		}
	})

	t.Run("middlewares run in registration order", func(t *testing.T) {
		t.Parallel()
		var mu sync.Mutex
		var calls []string
		mw := func(name string) func(http.HandlerFunc) http.HandlerFunc {
			return func(next http.HandlerFunc) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					mu.Lock()
					calls = append(calls, name)
					mu.Unlock()
					next(w, r)
				}
			}
		}
		opts := &brcrud.BridgrOptions[item]{Middlewares: []func(http.HandlerFunc) http.HandlerFunc{mw("outer"), mw("inner")}}
		rec := serve(t, newServer(t, newMemModel(), opts), "GET", "/items", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if got := fmt.Sprint(calls); got != "[outer inner]" {
			t.Errorf("middleware order = %s, want [outer inner]", got)
		}
	})
}
