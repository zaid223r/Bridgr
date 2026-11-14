package brcrud_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// nopStore satisfies brstore.Store for any T; registration tests never call it.
type nopStore[T any] struct{}

func (nopStore[T]) List(context.Context, brstore.Query) ([]T, error) { return nil, nil }

func (nopStore[T]) Get(context.Context, string) (T, error) {
	var zero T
	return zero, nil
}

func (nopStore[T]) Create(_ context.Context, in T) (T, error) { return in, nil }

func (nopStore[T]) Update(_ context.Context, _ string, in T) (T, error) { return in, nil }

func (nopStore[T]) Delete(context.Context, string) error { return nil }

func (nopStore[T]) Count(context.Context, brstore.Query) (int64, error) { return 0, nil }

type noPK struct {
	Name string `json:"name"`
}

func panicMessage(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	f()
	return ""
}

func TestRegistrationFailsFast(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		register func(*brhttp.Router)
		want     []string
	}{
		{
			name: "model is not a struct",
			register: func(r *brhttp.Router) {
				brcrud.RegisterCRUD[string](r, "strings", nopStore[string]{}, nil)
			},
			want: []string{"/strings", "not a struct"},
		},
		{
			name: "default limit above max",
			register: func(r *brhttp.Router) {
				brcrud.RegisterCRUD[typoFixed](r, "limits", nopStore[typoFixed]{}, &brcrud.Options[typoFixed]{DefaultLimit: 500, MaxLimit: 100})
			},
			want: []string{"/limits", "DefaultLimit 500 is above MaxLimit 100"},
		},
		{
			name: "model without primary key",
			register: func(r *brhttp.Router) {
				brcrud.RegisterCRUD[noPK](r, "nopk", nopStore[noPK]{}, nil)
			},
			want: []string{"/nopk", "noPK has no primary key"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			router := brhttp.NewRouter()
			msg := panicMessage(func() { tt.register(router) })
			if msg == "" {
				t.Fatal("registration did not panic")
			}
			for _, want := range tt.want {
				if !strings.Contains(msg, want) {
					t.Errorf("panic %q does not mention %q", msg, want)
				}
			}
		})
	}
}

func TestValidModelRegisters(t *testing.T) {
	t.Parallel()
	router := brhttp.NewRouter()
	if msg := panicMessage(func() {
		brcrud.RegisterCRUD[typoFixed](router, "fixed", nopStore[typoFixed]{}, nil)
	}); msg != "" {
		t.Fatalf("valid model panicked: %s", msg)
	}
}

type typoFixed struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type listModel struct {
	ID       int               `json:"id"`
	Title    string            `json:"title"`
	Qty      int               `json:"qty"`
	Active   bool              `json:"active"`
	Sort     string            `json:"sort"`
	Attrs    map[string]string `json:"attrs"`
	Password string            `json:"password,omitzero" bridgr:"writeonly"`
}

func TestListOptionsFailFast(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		filters brcrud.Filters
		sort    []string
		want    string
	}{
		{"unknown filter field", brcrud.Filters{"titel": brcrud.Exact}, nil, `Filters: "titel" is not a JSON field of listModel`},
		{"Go name instead of JSON name", brcrud.Filters{"Title": brcrud.Exact}, nil, `"Title" is not a JSON field`},
		{"reserved name", brcrud.Filters{"sort": brcrud.Exact}, nil, `"sort" is reserved`},
		{"writeonly field", brcrud.Filters{"password": brcrud.Exact}, nil, `"password" is writeonly`},
		{"unfilterable type", brcrud.Filters{"attrs": brcrud.All}, nil, "attrs has type map[string]string, which can't be filtered"},
		{"no operators", brcrud.Filters{"qty": 0}, nil, "qty allows no operators"},
		{"contains on a number", brcrud.Filters{"qty": brcrud.Contains}, nil, "Contains doesn't apply to qty (int); it allows eq, ne, gt, gte, lt, lte, in"},
		{"range on a bool", brcrud.Filters{"active": brcrud.Range}, nil, "Range doesn't apply to active"},
		{"range on a string", brcrud.Filters{"title": brcrud.Exact | brcrud.Range}, nil, "Range doesn't apply to title"},
		{"unknown sort field", nil, []string{"qty", "nope"}, `Sort: "nope" is not a JSON field`},
		{"unsortable type", nil, []string{"attrs"}, "Sort: attrs has type map[string]string, which can't be sorted"},
		{"writeonly sort", nil, []string{"password"}, `Sort: "password" is writeonly`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			msg := panicMessage(func() {
				brcrud.RegisterCRUD[listModel](brhttp.NewRouter(), "lists", nopStore[listModel]{}, &brcrud.Options[listModel]{Filters: tt.filters, Sort: tt.sort})
			})
			if !strings.Contains(msg, "/lists") || !strings.Contains(msg, tt.want) {
				t.Errorf("panic = %q, want it to mention /lists and %q", msg, tt.want)
			}
		})
	}
}
