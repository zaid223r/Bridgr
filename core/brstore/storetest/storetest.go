// Package storetest is the contract every brstore.Store must pass. Backend tests
// call Run with a function that returns an empty store for the Widget model:
//
//	func TestContract(t *testing.T) {
//		storetest.Run(t, func(t *testing.T) brstore.Store[storetest.Widget] {
//			return newEmptyWidgetStore(t)
//		})
//	}
package storetest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// Widget is the model the contract runs against. It covers every filterable
// type: integer, float, string, bool, time and a nullable string.
type Widget struct {
	ID       int64     `json:"id" gorm:"primaryKey"`
	Name     string    `json:"name"`
	Price    float64   `json:"price"`
	Qty      int       `json:"qty"`
	Active   bool      `json:"active"`
	Released time.Time `json:"released"`
	Note     *string   `json:"note"`
}

// NewStore returns an empty store. It is called once per subtest.
type NewStore func(t *testing.T) brstore.Store[Widget]

var base = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func day(n int) time.Time { return base.AddDate(0, 0, n) }

func ptr(s string) *string { return &s }

// seed is the fixture most list tests use. Names are distinct; IDs are assigned
// by the store in this order.
var seed = []Widget{
	{Name: "Alpha Bolt", Price: 9.5, Qty: 10, Active: true, Released: day(0), Note: ptr("first")},
	{Name: "beta nut", Price: 2.25, Qty: 0, Active: false, Released: day(5)},
	{Name: "Gamma Gear", Price: 9.5, Qty: 3, Active: true, Released: day(10), Note: ptr("third")},
	{Name: "delta Bolt", Price: 120, Qty: 7, Active: false, Released: day(15)},
	{Name: "Epsilon Washer", Price: 0.5, Qty: 100, Active: true, Released: day(20), Note: ptr("fifth")},
}

// Run executes the whole contract as subtests of t.
func Run(t *testing.T, newStore NewStore) {
	t.Run("Create", func(t *testing.T) { testCreate(t, newStore) })
	t.Run("Get", func(t *testing.T) { testGet(t, newStore) })
	t.Run("Update", func(t *testing.T) { testUpdate(t, newStore) })
	t.Run("Delete", func(t *testing.T) { testDelete(t, newStore) })
	t.Run("ListOrder", func(t *testing.T) { testListOrder(t, newStore) })
	t.Run("Filters", func(t *testing.T) { testFilters(t, newStore) })
	t.Run("Sort", func(t *testing.T) { testSort(t, newStore) })
	t.Run("Paging", func(t *testing.T) { testPaging(t, newStore) })
	t.Run("InvalidQuery", func(t *testing.T) { testInvalidQuery(t, newStore) })
	t.Run("CancelledContext", func(t *testing.T) { testCancelled(t, newStore) })
	t.Run("ConcurrentCreates", func(t *testing.T) { testConcurrentCreates(t, newStore) })
}

func seeded(t *testing.T, newStore NewStore) (brstore.Store[Widget], []Widget) {
	t.Helper()
	s := newStore(t)
	created := make([]Widget, len(seed))
	for i, w := range seed {
		got, err := s.Create(t.Context(), w)
		if err != nil {
			t.Fatalf("seeding %q: %v", w.Name, err)
		}
		created[i] = got
	}
	return s, created
}

func id(w Widget) string { return fmt.Sprint(w.ID) }

func names(ws []Widget) []string {
	out := make([]string, len(ws))
	for i, w := range ws {
		out[i] = w.Name
	}
	return out
}

func assertSame(t *testing.T, got, want Widget) {
	t.Helper()
	if got.ID != want.ID || got.Name != want.Name || got.Price != want.Price || got.Qty != want.Qty ||
		got.Active != want.Active || !got.Released.Equal(want.Released) || !equalPtr(got.Note, want.Note) {
		t.Errorf("widget = %+v (note %v),\nwant %+v (note %v)", got, deref(got.Note), want, deref(want.Note))
	}
}

func equalPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func deref(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("err = %v, want one wrapping %v", err, target)
	}
}

func testCreate(t *testing.T, newStore NewStore) {
	s, created := seeded(t, newStore)
	for i, w := range created {
		if w.ID <= 0 {
			t.Fatalf("created[%d].ID = %d, want a positive generated id", i, w.ID)
		}
		if i > 0 && w.ID <= created[i-1].ID {
			t.Errorf("ids not increasing: %d after %d", w.ID, created[i-1].ID)
		}
	}

	explicit := Widget{ID: 1000, Name: "explicit", Released: day(1)}
	got, err := s.Create(t.Context(), explicit)
	if err != nil {
		t.Fatalf("create with explicit id: %v", err)
	}
	assertSame(t, got, explicit)

	next, err := s.Create(t.Context(), Widget{Name: "after explicit", Released: day(2)})
	if err != nil {
		t.Fatal(err)
	}
	if next.ID <= explicit.ID {
		t.Errorf("generated id %d after explicit %d, want it larger", next.ID, explicit.ID)
	}

	_, err = s.Create(t.Context(), Widget{ID: 1000, Name: "duplicate", Released: day(3)})
	wantErr(t, err, brerrors.ErrConflict)
}

func testGet(t *testing.T, newStore NewStore) {
	s, created := seeded(t, newStore)
	for _, w := range created {
		got, err := s.Get(t.Context(), id(w))
		if err != nil {
			t.Fatalf("get %d: %v", w.ID, err)
		}
		assertSame(t, got, w)
	}
	for _, missing := range []string{"999999", "abc", "", "1.5", "-1"} {
		_, err := s.Get(t.Context(), missing)
		wantErr(t, err, brerrors.ErrNotFound)
	}
}

func testUpdate(t *testing.T, newStore NewStore) {
	s, created := seeded(t, newStore)
	target := created[1]

	replacement := Widget{
		ID:       424242, // ignored: the key comes from the id argument
		Name:     "renamed",
		Price:    0,
		Qty:      0,
		Active:   false,
		Released: day(99),
		Note:     nil,
	}
	got, err := s.Update(t.Context(), id(target), replacement)
	if err != nil {
		t.Fatal(err)
	}
	want := replacement
	want.ID = target.ID
	assertSame(t, got, want)

	stored, err := s.Get(t.Context(), id(target))
	if err != nil {
		t.Fatal(err)
	}
	assertSame(t, stored, want)

	if _, err := s.Get(t.Context(), "424242"); !errors.Is(err, brerrors.ErrNotFound) {
		t.Errorf("update created a record under the body's id: %v", err)
	}

	// Zero values must be written too.
	updated, err := s.Update(t.Context(), id(created[0]), Widget{Name: created[0].Name, Released: created[0].Released})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Active || updated.Qty != 0 || updated.Note != nil {
		t.Errorf("zero values not applied: %+v", updated)
	}

	_, err = s.Update(t.Context(), "999999", replacement)
	wantErr(t, err, brerrors.ErrNotFound)
	_, err = s.Update(t.Context(), "abc", replacement)
	wantErr(t, err, brerrors.ErrNotFound)
}

func testDelete(t *testing.T, newStore NewStore) {
	s, created := seeded(t, newStore)
	if err := s.Delete(t.Context(), id(created[2])); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(t.Context(), id(created[2]))
	wantErr(t, err, brerrors.ErrNotFound)
	wantErr(t, s.Delete(t.Context(), id(created[2])), brerrors.ErrNotFound)
	wantErr(t, s.Delete(t.Context(), "abc"), brerrors.ErrNotFound)

	all, err := s.List(t.Context(), brstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(seed)-1 {
		t.Errorf("after delete: %d records, want %d", len(all), len(seed)-1)
	}
}

func testListOrder(t *testing.T, newStore NewStore) {
	s := newStore(t)
	empty, err := s.List(t.Context(), brstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("empty store List = %#v, want a non-nil empty slice", empty)
	}

	// Insert out of id order to make sure List sorts by key, not insertion.
	for _, idv := range []int64{30, 10, 20} {
		if _, err := s.Create(t.Context(), Widget{ID: idv, Name: fmt.Sprint(idv), Released: day(0)}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.List(t.Context(), brstore.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(all); !slices.Equal(got, []string{"10", "20", "30"}) {
		t.Errorf("default order = %v, want by id ascending", got)
	}
}

func testFilters(t *testing.T, newStore NewStore) {
	s, _ := seeded(t, newStore)
	f := func(field string, op brstore.Op, v any) brstore.Filter {
		return brstore.Filter{Field: field, Op: op, Value: v}
	}

	tests := []struct {
		name    string
		filters []brstore.Filter
		want    []string
	}{
		{"int eq", []brstore.Filter{f("qty", brstore.Eq, int64(3))}, []string{"Gamma Gear"}},
		{"int ne", []brstore.Filter{f("qty", brstore.Ne, int64(0))}, []string{"Alpha Bolt", "Gamma Gear", "delta Bolt", "Epsilon Washer"}},
		{"int gt", []brstore.Filter{f("qty", brstore.Gt, int64(7))}, []string{"Alpha Bolt", "Epsilon Washer"}},
		{"int gte", []brstore.Filter{f("qty", brstore.Gte, int64(7))}, []string{"Alpha Bolt", "delta Bolt", "Epsilon Washer"}},
		{"int lt", []brstore.Filter{f("qty", brstore.Lt, int64(3))}, []string{"beta nut"}},
		{"int lte", []brstore.Filter{f("qty", brstore.Lte, int64(3))}, []string{"beta nut", "Gamma Gear"}},
		{"float eq", []brstore.Filter{f("price", brstore.Eq, 9.5)}, []string{"Alpha Bolt", "Gamma Gear"}},
		{"float lt", []brstore.Filter{f("price", brstore.Lt, 2.5)}, []string{"beta nut", "Epsilon Washer"}},
		{"bool eq true", []brstore.Filter{f("active", brstore.Eq, true)}, []string{"Alpha Bolt", "Gamma Gear", "Epsilon Washer"}},
		{"bool eq false", []brstore.Filter{f("active", brstore.Eq, false)}, []string{"beta nut", "delta Bolt"}},
		{"string eq is exact", []brstore.Filter{f("name", brstore.Eq, "beta nut")}, []string{"beta nut"}},
		{"string eq is case-sensitive", []brstore.Filter{f("name", brstore.Eq, "BETA NUT")}, []string{}},
		{"string contains is case-insensitive", []brstore.Filter{f("name", brstore.Contains, "bolt")}, []string{"Alpha Bolt", "delta Bolt"}},
		{"time gte", []brstore.Filter{f("released", brstore.Gte, day(10))}, []string{"Gamma Gear", "delta Bolt", "Epsilon Washer"}},
		{"time lt", []brstore.Filter{f("released", brstore.Lt, day(5))}, []string{"Alpha Bolt"}},
		{"in", []brstore.Filter{f("qty", brstore.In, []any{int64(0), int64(100), int64(42)})}, []string{"beta nut", "Epsilon Washer"}},
		{"in strings", []brstore.Filter{f("name", brstore.In, []any{"Gamma Gear", "nope"})}, []string{"Gamma Gear"}},
		{"nullable eq skips nulls", []brstore.Filter{f("note", brstore.Eq, "third")}, []string{"Gamma Gear"}},
		{"filters are ANDed", []brstore.Filter{f("active", brstore.Eq, true), f("price", brstore.Eq, 9.5)}, []string{"Alpha Bolt", "Gamma Gear"}},
		{"no match", []brstore.Filter{f("qty", brstore.Gt, int64(1000))}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.List(t.Context(), brstore.Query{Filters: tt.filters})
			if err != nil {
				t.Fatal(err)
			}
			// Compare as sets so a failure shows membership, not ordering.
			gotNames, wantNames := names(got), slices.Clone(tt.want)
			slices.Sort(gotNames)
			slices.Sort(wantNames)
			if !slices.Equal(gotNames, wantNames) {
				t.Errorf("got %v, want %v", names(got), tt.want)
			}
		})
	}
}

func testSort(t *testing.T, newStore NewStore) {
	s, _ := seeded(t, newStore)
	tests := []struct {
		name string
		sort []brstore.Sort
		want []string
	}{
		{"qty ascending", []brstore.Sort{{Field: "qty"}}, []string{"beta nut", "Gamma Gear", "delta Bolt", "Alpha Bolt", "Epsilon Washer"}},
		{"released descending", []brstore.Sort{{Field: "released", Desc: true}}, []string{"Epsilon Washer", "delta Bolt", "Gamma Gear", "beta nut", "Alpha Bolt"}},
		{"ties broken by id", []brstore.Sort{{Field: "price", Desc: true}}, []string{"delta Bolt", "Alpha Bolt", "Gamma Gear", "beta nut", "Epsilon Washer"}},
		{"two keys", []brstore.Sort{{Field: "active"}, {Field: "qty", Desc: true}}, []string{"delta Bolt", "beta nut", "Epsilon Washer", "Alpha Bolt", "Gamma Gear"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.List(t.Context(), brstore.Query{Sort: tt.sort})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(names(got), tt.want) {
				t.Errorf("got %v, want %v", names(got), tt.want)
			}
		})
	}
}

func testPaging(t *testing.T, newStore NewStore) {
	s, _ := seeded(t, newStore)
	all := names(seed)
	tests := []struct {
		name          string
		limit, offset int
		want          []string
	}{
		{"no limit", 0, 0, all},
		{"limit", 2, 0, all[:2]},
		{"offset", 0, 3, all[3:]},
		{"limit and offset", 2, 1, all[1:3]},
		{"limit past the end", 10, 4, all[4:]},
		{"offset past the end", 2, 50, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.List(t.Context(), brstore.Query{Limit: tt.limit, Offset: tt.offset})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(names(got), tt.want) {
				t.Errorf("got %v, want %v", names(got), tt.want)
			}
		})
	}

	filtered, err := s.List(t.Context(), brstore.Query{
		Filters: []brstore.Filter{{Field: "active", Op: brstore.Eq, Value: true}},
		Sort:    []brstore.Sort{{Field: "qty", Desc: true}},
		Limit:   2,
		Offset:  1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Alpha Bolt", "Gamma Gear"}; !slices.Equal(names(filtered), want) {
		t.Errorf("filter+sort+page = %v, want %v", names(filtered), want)
	}
}

func testInvalidQuery(t *testing.T, newStore NewStore) {
	s, _ := seeded(t, newStore)
	queries := map[string]brstore.Query{
		"unknown filter field": {Filters: []brstore.Filter{{Field: "nope", Op: brstore.Eq, Value: "x"}}},
		"unknown sort field":   {Sort: []brstore.Sort{{Field: "nope"}}},
		"Go name, not JSON":    {Filters: []brstore.Filter{{Field: "Name", Op: brstore.Eq, Value: "x"}}},
	}
	for name, q := range queries {
		t.Run(name, func(t *testing.T) {
			_, err := s.List(t.Context(), q)
			wantErr(t, err, brstore.ErrInvalidQuery)
		})
	}
}

func testCancelled(t *testing.T, newStore NewStore) {
	s, created := seeded(t, newStore)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := s.List(ctx, brstore.Query{})
	wantErr(t, err, context.Canceled)
	_, err = s.Get(ctx, id(created[0]))
	wantErr(t, err, context.Canceled)
	_, err = s.Create(ctx, Widget{Name: "x", Released: day(0)})
	wantErr(t, err, context.Canceled)
	_, err = s.Update(ctx, id(created[0]), Widget{Name: "x", Released: day(0)})
	wantErr(t, err, context.Canceled)
	wantErr(t, s.Delete(ctx, id(created[0])), context.Canceled)

	if got, err := s.Get(t.Context(), id(created[0])); err != nil || got.Name != created[0].Name {
		t.Errorf("cancelled calls changed data: %+v, %v", got, err)
	}
}

func testConcurrentCreates(t *testing.T, newStore NewStore) {
	s := newStore(t)
	const n = 50
	ids := make([]int64, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			w, err := s.Create(t.Context(), Widget{Name: fmt.Sprint("w", i), Released: day(0)})
			ids[i], errs[i] = w.ID, err
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		t.Fatal(err)
	}
	slices.Sort(ids)
	if len(slices.Compact(ids)) != n {
		t.Errorf("concurrent creates produced duplicate ids: %v", ids)
	}
}
