package brcrud_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zaid223r/Bridgr/core/brcap"
	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

type account struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Password  string    `json:"password,omitzero" bridgr:"writeonly"`
	Role      string    `json:"role" bridgr:"readonly"`
	CreatedAt time.Time `json:"created_at" bridgr:"readonly"`
	Tags      []string  `json:"tags" bridgr:"readonly"`
}

var created = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// accountServer registers accounts with hooks that fill the readonly fields and
// "hash" the password, and collects the published events.
func accountServer(t *testing.T) (*brhttp.Router, *brstore.Memory[account], func() []brcap.Event) {
	t.Helper()
	store, err := brstore.NewMemory[account]()
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var events []brcap.Event
	bus := brcap.NewMemoryEventBus()
	bus.Subscribe(func(_ context.Context, e brcap.Event) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, e)
	})
	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[account](router, "accounts", store, &brcrud.Options[account]{
		Events: bus,
		Hooks: brcrud.Hooks[account]{
			BeforeCreate: func(_ context.Context, a *account) error {
				a.Role, a.CreatedAt, a.Tags = "member", created, []string{"new"}
				a.Password = "hashed:" + a.Password
				return nil
			},
		},
	})
	return router, store, func() []brcap.Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]brcap.Event(nil), events...)
	}
}

func TestReadOnlyAndWriteOnlyFields(t *testing.T) {
	t.Parallel()
	router, store, events := accountServer(t)

	rec := serve(t, router, "POST", "/accounts", `{"email":"a@x.io","password":"pw","role":"admin","created_at":"2000-01-01T00:00:00Z","tags":["vip"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "password") || strings.Contains(rec.Body.String(), "pw") {
		t.Errorf("create response leaks the password: %s", rec.Body)
	}
	got := decode[account](t, rec)
	if got.Role != "member" || !got.CreatedAt.Equal(created) || fmt.Sprint(got.Tags) != "[new]" {
		t.Errorf("create = %+v: readonly fields must come from the hook, not the body", got)
	}
	stored, err := store.Get(t.Context(), "1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Password != "hashed:pw" {
		t.Errorf("stored password = %q, want the hooked value", stored.Password)
	}

	for _, step := range []struct{ method, body string }{
		{"PUT", `{"email":"b@x.io","password":"pw2","role":"admin","created_at":"2000-01-01T00:00:00Z","tags":["vip"]}`},
		{"PUT", `{"email":"c@x.io"}`},
		{"PATCH", `{"role":"admin","tags":null}`},
	} {
		rec := serve(t, router, step.method, "/accounts/1", step.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", step.method, step.body, rec.Code, rec.Body)
		}
		if strings.Contains(rec.Body.String(), "password") {
			t.Errorf("%s response leaks the password: %s", step.method, rec.Body)
		}
		got := decode[account](t, rec)
		if got.Role != "member" || !got.CreatedAt.Equal(created) || fmt.Sprint(got.Tags) != "[new]" {
			t.Errorf("%s %s = %+v: readonly fields must keep their stored values", step.method, step.body, got)
		}
	}
	stored, _ = store.Get(t.Context(), "1")
	if stored.Email != "c@x.io" || stored.Password != "" {
		t.Errorf("after PUT without a password, stored = %+v; PUT resets writable fields", stored)
	}

	for _, path := range []string{"/accounts", "/accounts/1"} {
		if rec := serve(t, router, "GET", path, ""); strings.Contains(rec.Body.String(), "password") {
			t.Errorf("GET %s leaks the password: %s", path, rec.Body)
		}
	}
	for _, e := range events() {
		if a, ok := e.Data.(account); ok && a.Password != "" {
			t.Errorf("%s event carries the password", e.Action)
		}
	}
}

func TestPutWithReadOnlyFieldsOnMissingRecord(t *testing.T) {
	t.Parallel()
	router, _, _ := accountServer(t)
	if rec := serve(t, router, "PUT", "/accounts/9", `{"email":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("PUT missing = %d, want 404", rec.Code)
	}
}

func TestNestedWriteOnlyPanics(t *testing.T) {
	t.Parallel()
	type team struct {
		ID    int64     `json:"id"`
		Owner account   `json:"owner"`
		Staff []account `json:"staff"`
	}
	store, err := brstore.NewMemory[team]()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "team.Owner has writeonly fields") {
			t.Errorf("recover() = %v, want a panic naming the nested field", r)
		}
	}()
	brcrud.RegisterCRUD[team](brhttp.NewRouter(), "teams", store, nil)
}
