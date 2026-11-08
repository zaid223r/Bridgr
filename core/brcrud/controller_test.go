package brcrud_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

type item struct {
	ID     int64   `json:"id"`
	Title  string  `json:"title"`
	Qty    int     `json:"qty"`
	Active bool    `json:"active"`
	Price  float64 `json:"price"`
}

func (item) FilterableFields() []string { return []string{"title", "qty", "active"} }

// fakeStore is an in-memory store that records what the handlers passed in and
// can simulate a storage failure.
type fakeStore struct {
	*brstore.Memory[item]

	mu        sync.Mutex
	fail      error
	lastCtx   context.Context
	lastQuery brstore.Query
	lastID    string
}

func newFakeStore(t *testing.T, seed ...item) *fakeStore {
	t.Helper()
	mem, err := brstore.NewMemory[item]()
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range seed {
		if _, err := mem.Create(t.Context(), it); err != nil {
			t.Fatal(err)
		}
	}
	return &fakeStore{Memory: mem}
}

func (f *fakeStore) record(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastCtx, f.lastID = ctx, id
	return f.fail
}

func (f *fakeStore) List(ctx context.Context, q brstore.Query) ([]item, error) {
	if err := f.record(ctx, ""); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.lastQuery = q
	f.mu.Unlock()
	return f.Memory.List(ctx, q)
}

func (f *fakeStore) Get(ctx context.Context, id string) (item, error) {
	if err := f.record(ctx, id); err != nil {
		return item{}, err
	}
	return f.Memory.Get(ctx, id)
}

func (f *fakeStore) Create(ctx context.Context, v item) (item, error) {
	if err := f.record(ctx, ""); err != nil {
		return v, err
	}
	return f.Memory.Create(ctx, v)
}

func (f *fakeStore) Update(ctx context.Context, id string, v item) (item, error) {
	if err := f.record(ctx, id); err != nil {
		return v, err
	}
	return f.Memory.Update(ctx, id, v)
}

func (f *fakeStore) Delete(ctx context.Context, id string) error {
	if err := f.record(ctx, id); err != nil {
		return err
	}
	return f.Memory.Delete(ctx, id)
}

var seedItems = []item{
	{Title: "Hex Bolt", Qty: 5, Active: true},
	{Title: "wing nut", Qty: 0},
	{Title: "Carriage bolt", Qty: 12, Active: true},
}

func newServer(t *testing.T, store *fakeStore, opts *brcrud.Options[item]) *brhttp.Router {
	t.Helper()
	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[item](router, "items", store, opts)
	return router
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

func titles(items []item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Title
	}
	return out
}

func TestCRUDRoutes(t *testing.T) {
	t.Parallel()

	t.Run("list", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, newServer(t, newFakeStore(t, seedItems...), nil), "GET", "/items", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if got := titles(decode[[]item](t, rec)); !slices.Equal(got, titles(seedItems)) {
			t.Errorf("items = %v", got)
		}
	})

	t.Run("create", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, newServer(t, newFakeStore(t, seedItems...), nil), "POST", "/items", `{"title":"new"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d", rec.Code)
		}
		if got := decode[item](t, rec); got.ID != 4 || got.Title != "new" {
			t.Errorf("created = %+v", got)
		}
	})

	t.Run("get", func(t *testing.T) {
		t.Parallel()
		store := newFakeStore(t, seedItems...)
		rec := serve(t, newServer(t, store, nil), "GET", "/items/2", "")
		if rec.Code != http.StatusOK || decode[item](t, rec).Title != "wing nut" {
			t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
		}
		if store.lastID != "2" {
			t.Errorf("id passed to store = %q", store.lastID)
		}
	})

	t.Run("update writes zero values", func(t *testing.T) {
		t.Parallel()
		store := newFakeStore(t, seedItems...)
		rec := serve(t, newServer(t, store, nil), "PUT", "/items/1", `{"title":"Hex Bolt","qty":0,"active":false}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		got, _ := store.Get(t.Context(), "1")
		if got.Qty != 0 || got.Active {
			t.Errorf("stored = %+v, want qty 0 and inactive", got)
		}
	})

	t.Run("delete", func(t *testing.T) {
		t.Parallel()
		store := newFakeStore(t, seedItems...)
		rec := serve(t, newServer(t, store, nil), "DELETE", "/items/3", "")
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("status = %d, body %q", rec.Code, rec.Body)
		}
		if _, err := store.Get(t.Context(), "3"); !errors.Is(err, brerrors.ErrNotFound) {
			t.Errorf("record still there: %v", err)
		}
	})

	t.Run("unsupported method", func(t *testing.T) {
		t.Parallel()
		rec := serve(t, newServer(t, newFakeStore(t), nil), "POST", "/items/1", `{}`)
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "DELETE, GET, HEAD, PATCH, PUT" {
			t.Fatalf("status = %d, Allow = %q", rec.Code, rec.Header().Get("Allow"))
		}
	})

	t.Run("trailing slash", func(t *testing.T) {
		t.Parallel()
		if rec := serve(t, newServer(t, newFakeStore(t), nil), "GET", "/items/", ""); rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d", rec.Code)
		}
	})
}

func TestListFilters(t *testing.T) {
	t.Parallel()
	store := newFakeStore(t, seedItems...)
	router := newServer(t, store, nil)

	tests := []struct {
		query string
		want  []string
	}{
		{"?title=BOLT", []string{"Hex Bolt", "Carriage bolt"}},
		{"?active=true", []string{"Hex Bolt", "Carriage bolt"}},
		{"?qty=12", []string{"Carriage bolt"}},
		{"?qty=0&qty=5", []string{"Hex Bolt", "wing nut"}},
		{"?title=bolt&qty=5", []string{"Hex Bolt"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			rec := serve(t, router, "GET", "/items"+tt.query, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			if got := titles(decode[[]item](t, rec)); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	serve(t, router, "GET", "/items?qty=0&qty=5&title=nut", "")
	want := []brstore.Filter{
		{Field: "qty", Op: brstore.In, Value: []any{int64(0), int64(5)}},
		{Field: "title", Op: brstore.Contains, Value: "nut"},
	}
	if got := fmt.Sprint(store.lastQuery.Filters); got != fmt.Sprint(want) {
		t.Errorf("query passed to store = %s, want %s", got, fmt.Sprint(want))
	}
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
			name: "filter on non-filterable field", method: "GET", path: "/items?price=1&secret=x", wantStatus: 400,
			wantDetail: "invalid filter fields: price, secret", wantInvalid: []string{"price", "secret"},
		},
		{
			name: "unparsable filter value", method: "GET", path: "/items?active=maybe", wantStatus: 400,
			wantDetail: `invalid value "maybe" for filter active: want boolean`, wantInvalid: []string{"active"},
		},
		{
			name: "malformed JSON on create", method: "POST", path: "/items", body: `{"title":`, wantStatus: 400,
			wantDetail: "invalid JSON body: unexpected EOF",
		},
		{name: "malformed JSON on update", method: "PUT", path: "/items/1", body: `[]`, wantStatus: 400},
		{name: "get missing", method: "GET", path: "/items/99", wantStatus: 404},
		{name: "id of the wrong type", method: "GET", path: "/items/abc", wantStatus: 404},
		{name: "update missing", method: "PUT", path: "/items/99", body: `{"title":"x"}`, wantStatus: 404},
		{name: "delete missing", method: "DELETE", path: "/items/99", wantStatus: 404},
		{name: "patch missing", method: "PATCH", path: "/items/99", body: `{"title":"x"}`, wantStatus: 404},
		{name: "patch with malformed JSON", method: "PATCH", path: "/items/1", body: `{"qty":`, wantStatus: 400},
		{
			name: "patch with an array", method: "PATCH", path: "/items/1", body: `[{"qty":1}]`, wantStatus: 400,
			wantDetail: "invalid JSON body: body must be a JSON object",
		},
		{
			name: "patch with null", method: "PATCH", path: "/items/1", body: `null`, wantStatus: 400,
			wantDetail: "invalid JSON body: body must be a JSON object",
		},
		{name: "patch with a wrong type", method: "PATCH", path: "/items/1", body: `{"qty":"many"}`, wantStatus: 400},
		{name: "put with null", method: "PUT", path: "/items/1", body: `null`, wantStatus: 400},
		{name: "duplicate id", method: "POST", path: "/items", body: `{"id":1,"title":"dup"}`, wantStatus: 409},
	}
	failure := errors.New(`pq: relation "items" does not exist`)
	for _, rt := range allRoutes {
		tests = append(tests, errorCase{
			name: "storage failure " + rt.method + " " + rt.path, method: rt.method, path: rt.path, body: rt.body,
			fail: failure, wantStatus: 500, wantDetail: "The server could not complete the request.",
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore(t, seedItems...)
			store.fail = tt.fail
			p := decodeProblem(t, serve(t, newServer(t, store, nil), tt.method, tt.path, tt.body), tt.wantStatus)

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

var allRoutes = []struct{ method, path, body string }{
	{"GET", "/items", ""},
	{"POST", "/items", `{"title":"b"}`},
	{"GET", "/items/1", ""},
	{"PUT", "/items/1", `{"title":"b"}`},
	{"PATCH", "/items/1", `{"title":"b"}`},
	{"DELETE", "/items/1", ""},
}

func TestOptions(t *testing.T) {
	t.Parallel()

	t.Run("auth", func(t *testing.T) {
		t.Parallel()
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
				opts := &brcrud.Options[item]{Auth: func(*http.Request) error { return tt.authErr }}
				router := newServer(t, newFakeStore(t, seedItems...), opts)
				for _, rt := range allRoutes {
					rec := serve(t, router, rt.method, rt.path, rt.body)
					if tt.wantStatus == 0 {
						if rec.Code >= 400 {
							t.Errorf("%s %s: status = %d, want success", rt.method, rt.path, rec.Code)
						}
						continue
					}
					if p := decodeProblem(t, rec, tt.wantStatus); p.Detail != "" {
						t.Errorf("%s %s: auth problem leaks detail %q", rt.method, rt.path, p.Detail)
					}
				}
			})
		}
	})

	t.Run("validation fails", func(t *testing.T) {
		t.Parallel()
		opts := &brcrud.Options[item]{Validate: func(in item) error {
			if in.Title == "" {
				return errors.New("title is required")
			}
			return nil
		}}
		router := newServer(t, newFakeStore(t, seedItems...), opts)
		for _, rt := range []struct{ method, path, body string }{
			{"POST", "/items", `{}`}, {"PUT", "/items/1", `{}`}, {"PATCH", "/items/1", `{"title":""}`},
		} {
			p := decodeProblem(t, serve(t, router, rt.method, rt.path, rt.body), http.StatusUnprocessableEntity)
			if p.Detail != "title is required" {
				t.Errorf("%s %s: detail = %q", rt.method, rt.path, p.Detail)
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
		opts := &brcrud.Options[item]{Middlewares: []func(http.HandlerFunc) http.HandlerFunc{mw("outer"), mw("inner")}}
		if rec := serve(t, newServer(t, newFakeStore(t), opts), "GET", "/items", ""); rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if got := fmt.Sprint(calls); got != "[outer inner]" {
			t.Errorf("middleware order = %s", got)
		}
	})
}

type ctxKey struct{}

func TestRequestContextReachesStore(t *testing.T) {
	t.Parallel()
	tag := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, "req-42")))
		}
	}
	for _, rt := range allRoutes {
		store := newFakeStore(t, seedItems...)
		opts := &brcrud.Options[item]{Middlewares: []func(http.HandlerFunc) http.HandlerFunc{tag}}
		serve(t, newServer(t, store, opts), rt.method, rt.path, rt.body)
		if store.lastCtx == nil || store.lastCtx.Value(ctxKey{}) != "req-42" {
			t.Errorf("%s %s: store did not receive the request context", rt.method, rt.path)
		}
	}
}

func TestBodyTooLarge(t *testing.T) {
	t.Parallel()
	router := newServer(t, newFakeStore(t, seedItems...), nil)
	limited := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16)
		router.ServeHTTP(w, r)
	})
	big := `{"title":"` + strings.Repeat("x", 64) + `"}`
	for _, rt := range []struct{ method, path string }{{"POST", "/items"}, {"PUT", "/items/1"}, {"PATCH", "/items/1"}} {
		p := decodeProblem(t, serve(t, limited, rt.method, rt.path, big), http.StatusRequestEntityTooLarge)
		if p.Detail != "request body is larger than 16 bytes" {
			t.Errorf("%s: detail = %q", rt.method, p.Detail)
		}
	}
}

func TestPutAndPatchSemantics(t *testing.T) {
	t.Parallel()
	original := item{Title: "Hex Bolt", Qty: 5, Active: true, Price: 1.25}

	tests := []struct {
		name   string
		method string
		body   string
		want   item
	}{
		{
			"PUT replaces: missing fields become zero", "PUT", `{"title":"Renamed"}`,
			item{ID: 1, Title: "Renamed"},
		},
		{
			"PUT writes explicit zero values", "PUT", `{"title":"Hex Bolt","qty":0,"active":false,"price":0}`,
			item{ID: 1, Title: "Hex Bolt"},
		},
		{
			"PATCH changes only the fields sent", "PATCH", `{"qty":9}`,
			item{ID: 1, Title: "Hex Bolt", Qty: 9, Active: true, Price: 1.25},
		},
		{
			"PATCH writes explicit zero values", "PATCH", `{"qty":0,"active":false}`,
			item{ID: 1, Title: "Hex Bolt", Qty: 0, Active: false, Price: 1.25},
		},
		{
			"PATCH with an empty object changes nothing", "PATCH", `{}`,
			item{ID: 1, Title: "Hex Bolt", Qty: 5, Active: true, Price: 1.25},
		},
		{
			"PATCH ignores an id in the body", "PATCH", `{"id":77,"title":"Moved?"}`,
			item{ID: 1, Title: "Moved?", Qty: 5, Active: true, Price: 1.25},
		},
		{"PUT ignores an id in the body", "PUT", `{"id":77,"title":"Moved?"}`, item{ID: 1, Title: "Moved?"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			store := newFakeStore(t, original)
			rec := serve(t, newServer(t, store, nil), tt.method, "/items/1", tt.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			if got := decode[item](t, rec); got != tt.want {
				t.Errorf("response = %+v, want %+v", got, tt.want)
			}
			stored, err := store.Get(t.Context(), "1")
			if err != nil || stored != tt.want {
				t.Errorf("stored = %+v (%v), want %+v", stored, err, tt.want)
			}
			if _, err := store.Get(t.Context(), "77"); !errors.Is(err, brerrors.ErrNotFound) {
				t.Errorf("a record was written under the body's id")
			}
		})
	}
}

func TestPatchValidatesTheMergedValue(t *testing.T) {
	t.Parallel()
	var seen item
	opts := &brcrud.Options[item]{Validate: func(in item) error {
		seen = in
		if in.Qty > 100 {
			return errors.New("qty must be at most 100")
		}
		return nil
	}}
	store := newFakeStore(t, item{Title: "Hex Bolt", Qty: 5})
	router := newServer(t, store, opts)

	decodeProblem(t, serve(t, router, "PATCH", "/items/1", `{"qty":500}`), http.StatusUnprocessableEntity)
	if seen.Title != "Hex Bolt" || seen.Qty != 500 {
		t.Errorf("validator saw %+v, want the stored record with the patch applied", seen)
	}
	if stored, _ := store.Get(t.Context(), "1"); stored.Qty != 5 {
		t.Errorf("rejected patch was stored: %+v", stored)
	}
}

func TestFilterOperators(t *testing.T) {
	t.Parallel()
	store := newFakeStore(t, seedItems...) // Hex Bolt qty 5 active, wing nut qty 0, Carriage bolt qty 12 active
	router := newServer(t, store, nil)

	tests := []struct {
		query string
		want  []string
	}{
		{"?qty[gt]=0", []string{"Hex Bolt", "Carriage bolt"}},
		{"?qty[gte]=5", []string{"Hex Bolt", "Carriage bolt"}},
		{"?qty[lt]=5", []string{"wing nut"}},
		{"?qty[lte]=5", []string{"Hex Bolt", "wing nut"}},
		{"?qty[ne]=5", []string{"wing nut", "Carriage bolt"}},
		{"?qty[eq]=12", []string{"Carriage bolt"}},
		{"?qty[gte]=1&qty[lte]=10", []string{"Hex Bolt"}},
		{"?qty[in]=0,12", []string{"wing nut", "Carriage bolt"}},
		{"?qty[in]=0&qty[in]=5", []string{"Hex Bolt", "wing nut"}},
		{"?title[eq]=Hex%20Bolt", []string{"Hex Bolt"}},
		{"?title[eq]=hex%20bolt", []string{}},
		{"?title[contains]=BOLT", []string{"Hex Bolt", "Carriage bolt"}},
		{"?title[ne]=wing%20nut", []string{"Hex Bolt", "Carriage bolt"}},
		{"?title[in]=wing%20nut,Hex%20Bolt", []string{"Hex Bolt", "wing nut"}},
		{"?active[ne]=true", []string{"wing nut"}},
		{"?active[eq]=true&qty[gt]=6", []string{"Carriage bolt"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			rec := serve(t, router, "GET", "/items"+tt.query, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			if got := titles(decode[[]item](t, rec)); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}

	serve(t, router, "GET", "/items?qty[gte]=1&qty[in]=2,3&title[eq]=x", "")
	want := []brstore.Filter{
		{Field: "qty", Op: brstore.Gte, Value: int64(1)},
		{Field: "qty", Op: brstore.In, Value: []any{int64(2), int64(3)}},
		{Field: "title", Op: brstore.Eq, Value: "x"},
	}
	if got := fmt.Sprint(store.lastQuery.Filters); got != fmt.Sprint(want) {
		t.Errorf("query = %s, want %s", got, fmt.Sprint(want))
	}
}

func TestFilterOperatorErrors(t *testing.T) {
	t.Parallel()
	router := newServer(t, newFakeStore(t, seedItems...), nil)

	tests := []struct {
		query       string
		wantDetail  string
		wantInvalid []string
	}{
		{"?qty[between]=1", `operator "between" is not supported for filter qty; use one of: eq, ne, gt, gte, lt, lte, in`, []string{"qty"}},
		{"?qty[contains]=1", `operator "contains" is not supported for filter qty; use one of: eq, ne, gt, gte, lt, lte, in`, []string{"qty"}},
		{"?title[gt]=a", `operator "gt" is not supported for filter title; use one of: eq, ne, contains, in`, []string{"title"}},
		{"?active[in]=true", `operator "in" is not supported for filter active; use one of: eq, ne`, []string{"active"}},
		{"?qty[]=1", `operator "" is not supported for filter qty; use one of: eq, ne, gt, gte, lt, lte, in`, []string{"qty"}},
		{"?qty[gt][lt]=1", `operator "gt][lt" is not supported for filter qty; use one of: eq, ne, gt, gte, lt, lte, in`, []string{"qty"}},
		{"?qty[gt]=1&qty[gt]=2", `invalid value "1,2" for filter qty: want a single value (use [in] for several)`, []string{"qty"}},
		{"?qty[in]=1,x", `invalid value "x" for filter qty: want integer (int64)`, []string{"qty"}},
		{"?qty[gte]=lots", `invalid value "lots" for filter qty: want integer (int64)`, []string{"qty"}},
		{"?price[gt]=1", `invalid filter field: price`, []string{"price"}},
		{"?nope[eq]=1&nope=2", `invalid filter field: nope`, []string{"nope"}},
		{"?qty[gt=1", `invalid filter field: qty[gt`, []string{"qty[gt"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()
			p := decodeProblem(t, serve(t, router, "GET", "/items"+tt.query, ""), http.StatusBadRequest)
			if p.Detail != tt.wantDetail {
				t.Errorf("detail = %q,\nwant %q", p.Detail, tt.wantDetail)
			}
			if !slices.Equal(p.InvalidFields, tt.wantInvalid) {
				t.Errorf("invalid_fields = %v, want %v", p.InvalidFields, tt.wantInvalid)
			}
		})
	}
}
