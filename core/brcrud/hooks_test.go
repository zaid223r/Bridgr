package brcrud_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/zaid223r/Bridgr/core/brcap"
	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// trace records hook calls and events in order.
type trace struct {
	mu    sync.Mutex
	calls []string
}

func (tr *trace) add(format string, args ...any) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.calls = append(tr.calls, fmt.Sprintf(format, args...))
}

func (tr *trace) list() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return slices.Clone(tr.calls)
}

func hookedServer(t *testing.T, store *fakeStore, tr *trace, opts brcrud.Options[item]) *brhttp.Router {
	t.Helper()
	bus := brcap.NewMemoryEventBus()
	bus.Subscribe(func(_ context.Context, e brcap.Event) {
		tr.add("event %s %s id=%s", e.Resource, e.Action, e.ID)
	})
	opts.Events = bus
	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[item](router, "items", store, withItemList(&opts))
	return router
}

func recordingHooks(tr *trace) brcrud.Hooks[item] {
	return brcrud.Hooks[item]{
		BeforeCreate: func(_ context.Context, v *item) error {
			tr.add("before create %q", v.Title)
			if v.Qty == 0 {
				v.Qty = 1 // a default filled in by the hook
			}
			return nil
		},
		AfterCreate: func(_ context.Context, v item) { tr.add("after create id=%d qty=%d", v.ID, v.Qty) },
		BeforeUpdate: func(_ context.Context, id string, v *item) error {
			tr.add("before update %s %q", id, v.Title)
			return nil
		},
		AfterUpdate:  func(_ context.Context, v item) { tr.add("after update id=%d", v.ID) },
		BeforeDelete: func(_ context.Context, id string) error { tr.add("before delete %s", id); return nil },
		AfterDelete:  func(_ context.Context, id string) { tr.add("after delete %s", id) },
	}
}

func TestHooksAndEventsOrder(t *testing.T) {
	t.Parallel()
	tr := &trace{}
	opts := brcrud.Options[item]{
		Hooks: recordingHooks(tr),
		Validate: func(v item) error {
			tr.add("validate qty=%d", v.Qty)
			return nil
		},
	}
	store := newFakeStore(t)
	router := hookedServer(t, store, tr, opts)

	created := decode[item](t, serve(t, router, "POST", "/items", `{"title":"bolt"}`))
	if created.Qty != 1 {
		t.Errorf("BeforeCreate default not stored: %+v", created)
	}
	serve(t, router, "PUT", "/items/1", `{"title":"bolt v2"}`)
	serve(t, router, "PATCH", "/items/1", `{"title":"bolt v3"}`)
	serve(t, router, "DELETE", "/items/1", "")

	want := []string{
		`before create "bolt"`, "validate qty=1", "after create id=1 qty=1", "event items created id=1",
		// PUT replaces the record, so qty (not in the body) becomes 0; PATCH keeps it.
		`before update 1 "bolt v2"`, "validate qty=0", "after update id=1", "event items updated id=1",
		`before update 1 "bolt v3"`, "validate qty=0", "after update id=1", "event items updated id=1",
		"before delete 1", "after delete 1", "event items deleted id=1",
	}
	if got := tr.list(); !slices.Equal(got, want) {
		t.Errorf("calls:\n  %s\nwant:\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestBeforeHooksReject(t *testing.T) {
	t.Parallel()
	denied := fmt.Errorf("only owners: %w", brerrors.ErrForbidden)
	tests := []struct {
		name       string
		hooks      brcrud.Hooks[item]
		method     string
		path, body string
		wantStatus int
	}{
		{
			"create forbidden",
			brcrud.Hooks[item]{BeforeCreate: func(context.Context, *item) error { return denied }},
			"POST", "/items", `{"title":"x"}`, 403,
		},
		{"create with field errors", brcrud.Hooks[item]{BeforeCreate: func(context.Context, *item) error {
			errs := &brerrors.ValidationError{}
			errs.Add("title", "is taken")
			return errs
		}}, "POST", "/items", `{"title":"x"}`, 422},
		{"replace conflict", brcrud.Hooks[item]{BeforeUpdate: func(context.Context, string, *item) error {
			return brerrors.ErrConflict
		}}, "PUT", "/items/1", `{"title":"x"}`, 409},
		{"update with a plain error", brcrud.Hooks[item]{BeforeUpdate: func(context.Context, string, *item) error {
			return errors.New("db down")
		}}, "PATCH", "/items/1", `{"title":"x"}`, 500},
		{
			"delete forbidden",
			brcrud.Hooks[item]{BeforeDelete: func(context.Context, string) error { return denied }},
			"DELETE", "/items/1", "", 403,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tr := &trace{}
			hooks := tt.hooks
			hooks.AfterCreate = func(context.Context, item) { tr.add("after create") }
			hooks.AfterUpdate = func(context.Context, item) { tr.add("after update") }
			hooks.AfterDelete = func(context.Context, string) { tr.add("after delete") }
			store := newFakeStore(t, item{Title: "original", Qty: 5})
			router := hookedServer(t, store, tr, brcrud.Options[item]{Hooks: hooks})

			decodeProblem(t, serve(t, router, tt.method, tt.path, tt.body), tt.wantStatus)
			if got := tr.list(); len(got) != 0 {
				t.Errorf("a rejected write ran after hooks or published events: %v", got)
			}
			if stored, err := store.Get(t.Context(), "1"); err != nil || stored.Title != "original" {
				t.Errorf("record changed by a rejected write: %+v, %v", stored, err)
			}
			if tt.method == "POST" {
				if n, _ := store.Count(t.Context(), brstore.Query{}); n != 1 {
					t.Errorf("rejected create stored a record: count %d", n)
				}
			}
		})
	}
}

func TestNoAfterHooksOrEventsWhenStoreFails(t *testing.T) {
	t.Parallel()
	tr := &trace{}
	store := newFakeStore(t, item{Title: "x"})
	store.fail = errors.New("disk full")
	hooks := recordingHooks(tr)
	router := hookedServer(t, store, tr, brcrud.Options[item]{Hooks: hooks})
	for _, rt := range allRoutes {
		serve(t, router, rt.method, rt.path, rt.body)
	}
	for _, c := range tr.list() {
		if strings.HasPrefix(c, "after") || strings.HasPrefix(c, "event") {
			t.Errorf("%q ran although the store failed", c)
		}
	}
}

func TestEventPayload(t *testing.T) {
	t.Parallel()
	bus := brcap.NewMemoryEventBus()
	var events []brcap.Event
	bus.Subscribe(func(_ context.Context, e brcap.Event) { events = append(events, e) })
	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[item](router, "items", newFakeStore(t), &brcrud.Options[item]{Events: bus})

	serve(t, router, "POST", "/items", `{"title":"bolt","qty":3}`)
	serve(t, router, "DELETE", "/items/1", "")
	if len(events) != 2 {
		t.Fatalf("got %d events", len(events))
	}
	created, deleted := events[0], events[1]
	if data, ok := created.Data.(item); !ok || data.Title != "bolt" || data.ID != 1 || created.At.IsZero() {
		t.Errorf("created event = %+v", created)
	}
	if deleted.Data != nil || deleted.ID != "1" || deleted.Resource != "items" {
		t.Errorf("deleted event = %+v", deleted)
	}
}

func TestHooksSeeThePrincipal(t *testing.T) {
	t.Parallel()
	setOwner := func(ctx context.Context, v *item) error {
		p, ok := brcap.PrincipalFrom(ctx)
		if !ok {
			return brerrors.ErrUnauthenticated
		}
		v.Title = v.Title + " (by " + p.ID + ")"
		return nil
	}
	auth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(brcap.WithPrincipal(r.Context(), brcap.Principal{ID: "zaid"})))
		})
	}
	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[item](router, "items", newFakeStore(t), &brcrud.Options[item]{
		Middleware: []func(http.Handler) http.Handler{auth},
		Hooks:      brcrud.Hooks[item]{BeforeCreate: setOwner},
	})
	if got := decode[item](t, serve(t, router, "POST", "/items", `{"title":"note"}`)); got.Title != "note (by zaid)" {
		t.Errorf("created = %+v", got)
	}
}
