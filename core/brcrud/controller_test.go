package brcrud_test

import (
	"context"
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
	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brhttp"
)

type item struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

var errNotFound = fmt.Errorf("item: %w", brerrors.ErrNotFound)

// memModel is an in-memory brcrud.BridgrModel that also records the arguments
// it was called with, so tests can check what the handlers passed through.
type memModel struct {
	mu          sync.Mutex
	items       map[string]item
	nextID      int
	lastFilters map[string][]string
	lastID      string
	lastCtx     context.Context
	// fail, when set, is returned by every method to simulate a storage failure.
	fail error
}

func newMemModel(seed ...item) *memModel {
	m := &memModel{items: map[string]item{}, nextID: 1}
	for _, it := range seed {
		m.items[strconv.Itoa(it.ID)] = it
		m.nextID = max(m.nextID, it.ID+1)
	}
	return m
}

func (m *memModel) List(ctx context.Context, filters map[string][]string) ([]item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCtx = ctx
	if m.fail != nil {
		return nil, m.fail
	}
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

func (m *memModel) Get(ctx context.Context, id string) (item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCtx = ctx
	if m.fail != nil {
		return item{}, m.fail
	}
	m.lastID = id
	it, ok := m.items[id]
	if !ok {
		return item{}, errNotFound
	}
	return it, nil
}

func (m *memModel) Create(ctx context.Context, input item) (item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCtx = ctx
	if m.fail != nil {
		return item{}, m.fail
	}
	input.ID = m.nextID
	m.nextID++
	m.items[strconv.Itoa(input.ID)] = input
	return input, nil
}

func (m *memModel) Update(ctx context.Context, id string, input item) (item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCtx = ctx
	if m.fail != nil {
		return item{}, m.fail
	}
	m.lastID = id
	if _, ok := m.items[id]; !ok {
		return item{}, errNotFound
	}
	input.ID, _ = strconv.Atoi(id)
	m.items[id] = input
	return input, nil
}

func (m *memModel) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastCtx = ctx
	m.lastID = id
	if m.fail != nil {
		return m.fail
	}
	if _, ok := m.items[id]; !ok {
		return errNotFound
	}
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

func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) brerrors.Problem {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body %q)", rec.Code, wantStatus, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != brerrors.ContentType {
		t.Fatalf("Content-Type = %q, want %q", ct, brerrors.ContentType)
	}
	p := decode[brerrors.Problem](t, rec)
	if p.Status != wantStatus || p.Title != http.StatusText(wantStatus) {
		t.Errorf("problem status/title = %d/%q, want %d/%q", p.Status, p.Title, wantStatus, http.StatusText(wantStatus))
	}
	return p
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
		if rec.Body.Len() != 0 {
			t.Errorf("204 response has body %q", rec.Body.String())
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

	t.Run("auth", func(t *testing.T) {
		t.Parallel()
		routes := []struct{ method, path string }{
			{"GET", "/items"}, {"POST", "/items"}, {"GET", "/items/1"}, {"PUT", "/items/1"}, {"DELETE", "/items/1"},
		}
		tests := []struct {
			name       string
			authErr    error
			wantStatus int
		}{
			{"allowed", nil, 0},
			{"unauthenticated", fmt.Errorf("no token: %w", brerrors.ErrUnauthenticated), http.StatusUnauthorized},
			{"forbidden sentinel", brerrors.ErrForbidden, http.StatusForbidden},
			{"any other error is forbidden", errors.New("not your item"), http.StatusForbidden},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				opts := &brcrud.BridgrOptions[item]{Auth: func(*http.Request) error { return tt.authErr }}
				router := newServer(t, newMemModel(item{ID: 1, Title: "a"}), opts)
				for _, rt := range routes {
					rec := serve(t, router, rt.method, rt.path, `{"title":"b"}`)
					if tt.wantStatus == 0 {
						if rec.Code >= 400 {
							t.Errorf("%s %s: status = %d, want success", rt.method, rt.path, rec.Code)
						}
						continue
					}
					p := decodeProblem(t, rec, tt.wantStatus)
					if p.Detail != "" {
						t.Errorf("%s %s: auth problem leaks detail %q", rt.method, rt.path, p.Detail)
					}
				}
			})
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
			p := decodeProblem(t, serve(t, router, rt.method, rt.path, `{}`), http.StatusUnprocessableEntity)
			if p.Detail != "title is required" {
				t.Errorf("%s %s: detail = %q, want the validation message", rt.method, rt.path, p.Detail)
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

type errorCase struct {
	name         string
	method, path string
	body         string
	fail         error
	wantStatus   int
	wantDetail   string
	wantInvalid  []string
}

func TestCRUDErrors(t *testing.T) {
	t.Parallel()

	tests := []errorCase{
		{
			name: "invalid filter", method: "GET", path: "/items?secret=x", wantStatus: 400,
			wantDetail: "invalid filter field: secret", wantInvalid: []string{"secret"},
		},
		{
			name: "malformed JSON on create", method: "POST", path: "/items", body: `{"title":`, wantStatus: 400,
			wantDetail: "invalid JSON body: unexpected EOF",
		},
		{name: "malformed JSON on update", method: "PUT", path: "/items/1", body: `[]`, wantStatus: 400},
		{name: "get missing", method: "GET", path: "/items/99", wantStatus: 404},
		{name: "update missing", method: "PUT", path: "/items/99", body: `{"title":"x"}`, wantStatus: 404},
		{name: "delete missing", method: "DELETE", path: "/items/99", wantStatus: 404},
	}
	failure := errors.New(`pq: relation "items" does not exist`)
	for _, rt := range []struct{ method, path, body string }{
		{"GET", "/items", ""}, {"POST", "/items", `{}`}, {"GET", "/items/1", ""}, {"PUT", "/items/1", `{}`}, {"DELETE", "/items/1", ""},
	} {
		tests = append(tests, errorCase{
			name: "storage failure " + rt.method + " " + rt.path, method: rt.method, path: rt.path, body: rt.body,
			fail: failure, wantStatus: 500, wantDetail: "The server could not complete the request.",
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			model := newMemModel(item{ID: 1, Title: "a"})
			model.fail = tt.fail
			p := decodeProblem(t, serve(t, newServer(t, model, nil), tt.method, tt.path, tt.body), tt.wantStatus)

			if tt.wantDetail != "" && p.Detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", p.Detail, tt.wantDetail)
			}
			if !slices.Equal(p.InvalidFields, tt.wantInvalid) {
				t.Errorf("invalid_fields = %v, want %v", p.InvalidFields, tt.wantInvalid)
			}
			if wantInstance, _, _ := strings.Cut(tt.path, "?"); p.Instance != wantInstance {
				t.Errorf("instance = %q, want %q", p.Instance, wantInstance)
			}
		})
	}
}

type ctxKey struct{}

func TestRequestContextReachesModel(t *testing.T) {
	t.Parallel()
	tag := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, "req-42")))
		}
	}
	for _, rt := range []struct{ method, path, body string }{
		{"GET", "/items", ""}, {"POST", "/items", `{}`}, {"GET", "/items/1", ""}, {"PUT", "/items/1", `{}`}, {"DELETE", "/items/1", ""},
	} {
		model := newMemModel(item{ID: 1})
		opts := &brcrud.BridgrOptions[item]{Middlewares: []func(http.HandlerFunc) http.HandlerFunc{tag}}
		serve(t, newServer(t, model, opts), rt.method, rt.path, rt.body)
		if model.lastCtx == nil || model.lastCtx.Value(ctxKey{}) != "req-42" {
			t.Errorf("%s %s: model did not receive the request context", rt.method, rt.path)
		}
	}
}

func TestBodyTooLarge(t *testing.T) {
	t.Parallel()
	router := newServer(t, newMemModel(item{ID: 1}), nil)
	limited := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16)
		router.ServeHTTP(w, r)
	})
	big := `{"title":"` + strings.Repeat("x", 64) + `"}`
	for _, rt := range []struct{ method, path string }{{"POST", "/items"}, {"PUT", "/items/1"}} {
		p := decodeProblem(t, serve(t, limited, rt.method, rt.path, big), http.StatusRequestEntityTooLarge)
		if p.Detail != "request body is larger than 16 bytes" {
			t.Errorf("%s: detail = %q", rt.method, p.Detail)
		}
	}
}
