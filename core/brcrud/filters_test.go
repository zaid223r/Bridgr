package brcrud_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// listServer registers items with exactly the given filters and sort, without
// the defaults the other tests use.
func listServer(t *testing.T, filters brcrud.Filters, sort []string) (*brhttp.Router, *fakeStore) {
	t.Helper()
	store := newFakeStore(t, seedItems...)
	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[item](router, "items", store, &brcrud.Options[item]{Filters: filters, Sort: sort})
	return router, store
}

func TestNothingIsFilterableByDefault(t *testing.T) {
	t.Parallel()
	router, _ := listServer(t, nil, nil)
	for query, detail := range map[string]string{
		"?title=bolt":  "invalid filter field: title",
		"?qty[gte]=1":  "invalid filter field: qty",
		"?sort=title":  `invalid sort "title": can't sort by "title"; use id`,
		"?sort=-title": `invalid sort "-title": can't sort by "title"; use id`,
	} {
		p := decodeProblem(t, serve(t, router, "GET", "/items"+query, ""), http.StatusBadRequest)
		if p.Detail != detail {
			t.Errorf("GET /items%s detail = %q, want %q", query, p.Detail, detail)
		}
	}
	if rec := serve(t, router, "GET", "/items?sort=-id&limit=2", ""); rec.Code != http.StatusOK {
		t.Errorf("sorting by the primary key = %d, want it always allowed", rec.Code)
	}
}

func TestFilterOperatorSets(t *testing.T) {
	t.Parallel()
	router, store := listServer(t, brcrud.Filters{
		"title":  brcrud.Exact | brcrud.Contains,
		"qty":    brcrud.Range,
		"active": brcrud.Exact | brcrud.Not,
	}, []string{"qty"})

	tests := []struct {
		query  string
		status int
		want   string // filters passed to the store, or the problem detail
	}{
		{"?title=Hex%20Bolt", 200, "[{title eq Hex Bolt}]"},
		{"?title[contains]=bolt", 200, "[{title contains bolt}]"},
		{"?qty[gte]=5&qty[lt]=20", 200, "[{qty gte 5} {qty lt 20}]"},
		{"?active[ne]=true", 200, "[{active ne true}]"},
		{"?sort=-qty", 200, "[]"},
		{"?title[ne]=x", 400, `operator "ne" is not supported for filter title; use one of: eq, contains`},
		{"?title=a&title=b", 400, `operator "in" is not supported for filter title; use one of: eq, contains`},
		{"?qty=5", 400, `operator "eq" is not supported for filter qty; use one of: gt, gte, lt, lte`},
		{"?qty[in]=1,2", 400, `operator "in" is not supported for filter qty; use one of: gt, gte, lt, lte`},
		{"?sort=title", 400, `invalid sort "title": can't sort by "title"; use id, qty`},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			rec := serve(t, router, "GET", "/items"+tt.query, "")
			if tt.status != http.StatusOK {
				if p := decodeProblem(t, rec, tt.status); p.Detail != tt.want {
					t.Errorf("detail = %q, want %q", p.Detail, tt.want)
				}
				return
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			if got := fmt.Sprint(store.lastQuery.Filters); got != tt.want {
				t.Errorf("filters = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestAllAllowsEveryOperatorOfTheType(t *testing.T) {
	t.Parallel()
	router, store := listServer(t, brcrud.Filters{"qty": brcrud.All, "title": brcrud.All}, nil)
	for _, query := range []string{"?qty[gt]=1", "?qty[in]=1,2", "?qty=1&qty=2", "?title[contains]=x", "?title[in]=a,b"} {
		if rec := serve(t, router, "GET", "/items"+query, ""); rec.Code != http.StatusOK {
			t.Errorf("GET /items%s = %d %s", query, rec.Code, rec.Body)
		}
	}
	serve(t, router, "GET", "/items?qty=1&qty=2", "")
	if got, want := fmt.Sprint(store.lastQuery.Filters), fmt.Sprint([]brstore.Filter{{Field: "qty", Op: brstore.In, Value: []any{int64(1), int64(2)}}}); got != want {
		t.Errorf("repeated plain value = %s, want %s", got, want)
	}
}
