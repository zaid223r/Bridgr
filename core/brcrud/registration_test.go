package brcrud_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
)

// noopModel satisfies brcrud.BridgrModel for any T; registration tests never
// call it.
type noopModel[T any] struct{}

func (noopModel[T]) List(context.Context, map[string][]string) ([]T, error) { return nil, nil }

func (noopModel[T]) Get(context.Context, string) (T, error) {
	var zero T
	return zero, nil
}

func (noopModel[T]) Create(_ context.Context, in T) (T, error) { return in, nil }

func (noopModel[T]) Update(_ context.Context, _ string, in T) (T, error) { return in, nil }

func (noopModel[T]) Delete(context.Context, string) error { return nil }

type typoFilter struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

func (typoFilter) FilterableFields() []string { return []string{"titel"} }

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
			name: "unknown filterable field",
			register: func(r *brhttp.Router) {
				brcrud.RegisterCRUDRoutes[typoFilter](r, "typos", noopModel[typoFilter]{}, nil)
			},
			want: []string{"/typos", `"titel"`, "typoFilter"},
		},
		{
			name: "model is not a struct",
			register: func(r *brhttp.Router) {
				brcrud.RegisterCRUDRoutes[string](r, "strings", noopModel[string]{}, nil)
			},
			want: []string{"/strings", "not a struct"},
		},
		{
			name: "GORM model without primary key",
			register: func(r *brhttp.Router) {
				brcrud.RegisterCRUD[noPK](r, "nopk", nil, nil)
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
		brcrud.RegisterCRUD[typoFixed](router, "fixed", nil, nil)
	}); msg != "" {
		t.Fatalf("valid model panicked: %s", msg)
	}
}

type typoFixed struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

func (typoFixed) FilterableFields() []string { return []string{"title"} }
