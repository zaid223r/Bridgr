package brstore_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brschema"
	"github.com/zaid223r/Bridgr/core/brstore"
	"github.com/zaid223r/Bridgr/core/brstore/storetest"
)

func TestMemoryContract(t *testing.T) {
	t.Parallel()
	storetest.Run(t, func(t *testing.T) brstore.Store[storetest.Widget] {
		s, err := brstore.NewMemory[storetest.Widget]()
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func TestNewMemoryRejectsBadModels(t *testing.T) {
	t.Parallel()
	type noPK struct {
		Name string `json:"name"`
	}
	type floatPK struct {
		ID float64 `json:"id"`
	}
	if _, err := brstore.NewMemory[noPK](); err == nil || !strings.Contains(err.Error(), "no primary key") {
		t.Errorf("no primary key: err = %v", err)
	}
	if _, err := brstore.NewMemory[floatPK](); err == nil || !strings.Contains(err.Error(), "string or integer") {
		t.Errorf("float primary key: err = %v", err)
	}
	if _, err := brstore.NewMemory[string](); err == nil {
		t.Error("non-struct model accepted")
	}
}

func TestMemoryStringKey(t *testing.T) {
	t.Parallel()
	type sku struct {
		Code string `json:"code" gorm:"primaryKey"`
		Name string `json:"name"`
	}
	s, err := brstore.NewMemory[sku]()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(t.Context(), sku{Name: "no code"}); err == nil {
		t.Error("created a record with an empty string key")
	}
	if _, err := s.Create(t.Context(), sku{Code: "A-1", Name: "first"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(t.Context(), "A-1")
	if err != nil || got.Name != "first" {
		t.Errorf("Get(A-1) = %+v, %v", got, err)
	}
}

func TestMemoryUnsignedKey(t *testing.T) {
	t.Parallel()
	type item struct {
		ID   uint32 `json:"id"`
		Name string `json:"name"`
	}
	s, err := brstore.NewMemory[item]()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.Create(t.Context(), item{Name: "a"})
	b, _ := s.Create(t.Context(), item{Name: "b"})
	if a.ID != 1 || b.ID != 2 {
		t.Errorf("ids = %d, %d; want 1, 2", a.ID, b.ID)
	}
	if _, err := s.Get(t.Context(), "-1"); !errors.Is(err, brerrors.ErrNotFound) {
		t.Errorf("negative id on unsigned key: %v", err)
	}
}

func TestMemoryInvalidOperators(t *testing.T) {
	t.Parallel()
	s, err := brstore.NewMemory[storetest.Widget]()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(t.Context(), storetest.Widget{Name: "x", Qty: 1}); err != nil {
		t.Fatal(err)
	}
	bad := []brstore.Filter{
		{Field: "qty", Op: brstore.Contains, Value: "1"},
		{Field: "qty", Op: brstore.In, Value: int64(1)},
		{Field: "qty", Op: "between", Value: int64(1)},
		{Field: "qty", Op: brstore.Eq, Value: "one"},
		{Field: "released", Op: brstore.Gt, Value: "2026-01-01"},
	}
	for _, f := range bad {
		if _, err := s.List(t.Context(), brstore.Query{Filters: []brstore.Filter{f}}); !errors.Is(err, brstore.ErrInvalidQuery) {
			t.Errorf("%+v: err = %v, want ErrInvalidQuery", f, err)
		}
	}
}

func TestParseValue(t *testing.T) {
	t.Parallel()
	type all struct {
		I   int       `json:"i"`
		I8  int8      `json:"i8"`
		U   uint      `json:"u"`
		F   float64   `json:"f"`
		B   bool      `json:"b"`
		S   string    `json:"s"`
		T   time.Time `json:"t"`
		P   *int      `json:"p"`
		Arr []int     `json:"arr"`
	}
	m, err := brschema.Of[all]()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		field, in string
		want      any
		wantErr   bool
	}{
		{"i", "-42", int64(-42), false},
		{"i", "x", nil, true},
		{"i8", "127", int64(127), false},
		{"i8", "128", nil, true},
		{"u", "7", uint64(7), false},
		{"u", "-7", nil, true},
		{"f", "1.5", 1.5, false},
		{"f", "abc", nil, true},
		{"b", "true", true, false},
		{"b", "0", false, false},
		{"b", "maybe", nil, true},
		{"s", "anything", "anything", false},
		{"t", "2026-03-04T05:06:07Z", time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC), false},
		{"t", "yesterday", nil, true},
		{"p", "3", int64(3), false},
	}
	for _, tt := range tests {
		f, _ := m.FieldByJSON(tt.field)
		got, err := brstore.ParseValue(f, tt.in)
		if tt.wantErr {
			valueErr, ok := errors.AsType[*brerrors.InvalidFilterValueError](err)
			if !ok || valueErr.Field != tt.field || valueErr.Value != tt.in {
				t.Errorf("ParseValue(%s, %q) err = %v, want InvalidFilterValueError", tt.field, tt.in, err)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseValue(%s, %q) = %#v, %v; want %#v", tt.field, tt.in, got, err, tt.want)
		}
	}

	arr, _ := m.FieldByJSON("arr")
	if _, err := brstore.ParseValue(arr, "1"); !errors.Is(err, brstore.ErrInvalidQuery) {
		t.Errorf("slice field: err = %v, want ErrInvalidQuery", err)
	}
}
