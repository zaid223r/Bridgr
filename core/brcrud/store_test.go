package brcrud_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

type product struct {
	ID     int64   `json:"id"`
	Name   string  `json:"name"`
	Qty    int     `json:"qty"`
	Active bool    `json:"active"`
	Price  float64 `json:"price"`
	Secret string  `json:"secret"`
}

func (product) FilterableFields() []string { return []string{"name", "qty", "active"} }

func newStoreServer(t *testing.T) *brhttp.Router {
	t.Helper()
	store, err := brstore.NewMemory[product]()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []product{
		{Name: "Hex Bolt", Qty: 5, Active: true},
		{Name: "wing nut", Qty: 0},
		{Name: "Carriage bolt", Qty: 12, Active: true},
	} {
		if _, err := store.Create(t.Context(), p); err != nil {
			t.Fatal(err)
		}
	}
	router := brhttp.NewRouter()
	brcrud.RegisterStore[product](router, "products", store, nil)
	return router
}

func productNames(ps []product) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name
	}
	return out
}

func TestRegisterStoreList(t *testing.T) {
	t.Parallel()
	router := newStoreServer(t)

	tests := []struct {
		query string
		want  []string
	}{
		{"", []string{"Hex Bolt", "wing nut", "Carriage bolt"}},
		{"?name=BOLT", []string{"Hex Bolt", "Carriage bolt"}},
		{"?active=true", []string{"Hex Bolt", "Carriage bolt"}},
		{"?qty=12", []string{"Carriage bolt"}},
		{"?qty=0&qty=5", []string{"Hex Bolt", "wing nut"}},
		{"?name=bolt&qty=5", []string{"Hex Bolt"}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			t.Parallel()
			rec := serve(t, router, "GET", "/products"+tt.query, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
			}
			if got := productNames(decode[[]product](t, rec)); !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegisterStoreErrors(t *testing.T) {
	t.Parallel()
	router := newStoreServer(t)

	tests := []struct {
		name         string
		method, path string
		body         string
		wantStatus   int
		wantInvalid  []string
	}{
		{"filter on non-filterable field", "GET", "/products?secret=x&price=1", "", 400, []string{"price", "secret"}},
		{"unparsable filter value", "GET", "/products?active=maybe", "", 400, []string{"active"}},
		{"unparsable int filter", "GET", "/products?qty=lots", "", 400, []string{"qty"}},
		{"missing record", "GET", "/products/99", "", 404, nil},
		{"id of the wrong type", "GET", "/products/abc", "", 404, nil},
		{"duplicate id", "POST", "/products", `{"id":1,"name":"dup"}`, 409, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := decodeProblem(t, serve(t, router, tt.method, tt.path, tt.body), tt.wantStatus)
			if !slices.Equal(p.InvalidFields, tt.wantInvalid) {
				t.Errorf("invalid_fields = %v, want %v", p.InvalidFields, tt.wantInvalid)
			}
		})
	}
}

func TestRegisterStoreCRUD(t *testing.T) {
	t.Parallel()
	router := newStoreServer(t)

	created := decode[product](t, serve(t, router, "POST", "/products", `{"name":"washer","qty":40}`))
	if created.ID != 4 {
		t.Fatalf("created id = %d, want 4", created.ID)
	}
	updated := decode[product](t, serve(t, router, "PUT", "/products/4", `{"name":"washer","qty":0}`))
	if updated.Qty != 0 || updated.ID != 4 {
		t.Errorf("updated = %+v, want qty 0 (zero values are written)", updated)
	}
	if rec := serve(t, router, "DELETE", "/products/4", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
	decodeProblem(t, serve(t, router, "GET", "/products/4", ""), http.StatusNotFound)
}
