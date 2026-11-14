package brcrud_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zaid223r/Bridgr/api"
	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

type user struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
}

type CreateUser struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (c CreateUser) Validate() error {
	errs := &brerrors.ValidationError{}
	if c.Email == "" {
		errs.Add("email", "is required")
	}
	return errs.OrNil()
}

type UpdateUser struct {
	Email *string `json:"email"`
}

type UserOut struct {
	ID    int64     `json:"id"`
	Email string    `json:"email"`
	Since time.Time `json:"since"`
}

var errTaken = errors.New("email taken")

func userServer(t *testing.T) (*brhttp.Router, *brstore.Memory[user]) {
	t.Helper()
	store, err := brstore.NewMemory[user]()
	if err != nil {
		t.Fatal(err)
	}
	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[user](router, "users", store, &brcrud.Options[user]{
		Create: brcrud.Input(func(_ context.Context, in CreateUser) (user, error) {
			if in.Email == "taken@x.io" {
				return user{}, errors.Join(errTaken, brerrors.ErrConflict)
			}
			return user{Email: in.Email, PasswordHash: "hash:" + in.Password}, nil
		}),
		Update: brcrud.Apply(func(_ context.Context, u user, in UpdateUser) (user, error) {
			if in.Email != nil {
				u.Email = *in.Email
			}
			return u, nil
		}),
		Response: brcrud.Output(func(u user) UserOut {
			return UserOut{ID: u.ID, Email: u.Email, Since: u.CreatedAt}
		}),
		Hooks: brcrud.Hooks[user]{
			BeforeCreate: func(_ context.Context, u *user) error {
				u.CreatedAt = created
				return nil
			},
		},
		Validate: func(u user) error {
			if !strings.Contains(u.Email, "@") {
				return errors.New("email must contain @")
			}
			return nil
		},
	})
	return router, store
}

func TestCustomInputAndOutput(t *testing.T) {
	t.Parallel()
	router, store := userServer(t)

	rec := serve(t, router, "POST", "/users", `{"email":"a@x.io","password":"pw","password_hash":"forged"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if got := decode[UserOut](t, rec); got.ID != 1 || got.Email != "a@x.io" || !got.Since.Equal(created) {
		t.Errorf("create = %+v, want the Output view with the hook's timestamp", got)
	}
	if strings.Contains(rec.Body.String(), "hash") {
		t.Errorf("create response leaks the hash: %s", rec.Body)
	}
	if stored, _ := store.Get(t.Context(), "1"); stored.PasswordHash != "hash:pw" {
		t.Errorf("stored hash = %q, want it built by Input, not taken from the body", stored.PasswordHash)
	}

	for _, step := range []struct{ method, body, wantEmail string }{
		{"PATCH", `{"email":"b@x.io"}`, "b@x.io"},
		{"PUT", `{}`, "b@x.io"}, // Apply decides; an absent pointer changes nothing
		{"PUT", `{"email":"c@x.io"}`, "c@x.io"},
	} {
		rec := serve(t, router, step.method, "/users/1", step.body)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: %d %s", step.method, step.body, rec.Code, rec.Body)
		}
		if got := decode[UserOut](t, rec); got.Email != step.wantEmail || !got.Since.Equal(created) {
			t.Errorf("%s %s = %+v, want email %s", step.method, step.body, got, step.wantEmail)
		}
	}
	if stored, _ := store.Get(t.Context(), "1"); stored.PasswordHash != "hash:pw" {
		t.Errorf("update changed the hash to %q", stored.PasswordHash)
	}

	for _, path := range []string{"/users", "/users/1"} {
		rec := serve(t, router, "GET", path, "")
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "hash") || !strings.Contains(rec.Body.String(), `"since"`) {
			t.Errorf("GET %s = %d %s, want the Output view", path, rec.Code, rec.Body)
		}
	}
	if list := decode[[]UserOut](t, serve(t, router, "GET", "/users", "")); len(list) != 1 || list[0].Email != "c@x.io" {
		t.Errorf("list = %+v", list)
	}
}

func TestCustomInputErrors(t *testing.T) {
	t.Parallel()
	router, _ := userServer(t)
	serve(t, router, "POST", "/users", `{"email":"a@x.io"}`)

	tests := []struct {
		name, method, path, body string
		status                   int
		detail                   string
	}{
		{"malformed create", "POST", "/users", `{"email":`, 400, "invalid JSON body"},
		{"create body of the wrong type", "POST", "/users", `{"email":1}`, 400, "invalid JSON body"},
		{"create body not an object", "POST", "/users", `[]`, 400, "body must be a JSON object"},
		{"input Validate", "POST", "/users", `{"password":"pw"}`, 422, "email: is required"},
		{"input fn error", "POST", "/users", `{"email":"taken@x.io"}`, 409, ""},
		{"record Validate after input", "POST", "/users", `{"email":"nope"}`, 422, "email must contain @"},
		{"update missing record", "PATCH", "/users/9", `{"email":"x@x.io"}`, 404, ""},
		{"malformed update", "PUT", "/users/1", `{"email":`, 400, "invalid JSON body"},
		{"record Validate after apply", "PATCH", "/users/1", `{"email":"nope"}`, 422, "email must contain @"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := decodeProblem(t, serve(t, router, tt.method, tt.path, tt.body), tt.status)
			if !strings.Contains(p.Detail, tt.detail) && !slices.ContainsFunc(p.Errors, func(e brerrors.FieldError) bool {
				return e.Field+": "+e.Message == tt.detail
			}) {
				t.Errorf("problem = %+v, want detail or field error %q", p, tt.detail)
			}
		})
	}
}

func TestCustomTypesInOpenAPI(t *testing.T) {
	t.Parallel()
	router, _ := userServer(t)
	raw, err := router.Docs().Build(api.Info{})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]json.RawMessage `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range doc.Components.Schemas {
		names = append(names, name)
	}
	slices.Sort(names)
	if want := []string{"CreateUser", "Problem", "UpdateUser", "UserOut"}; !slices.Equal(names, want) {
		t.Errorf("schemas = %v, want %v: the model's own schemas must not appear", names, want)
	}
	for _, check := range []struct{ path, method, want string }{
		{"/users", "post", `"#/components/schemas/CreateUser"`},
		{"/users", "get", `"items":{"$ref":"#/components/schemas/UserOut"}`},
		{"/users/{id}", "put", `"#/components/schemas/UpdateUser"`},
		{"/users/{id}", "patch", `"#/components/schemas/UpdateUser"`},
		{"/users/{id}", "get", `"#/components/schemas/UserOut"`},
		{"/users/{id}", "patch", `"422"`},
	} {
		if op := compactJSON(t, doc.Paths[check.path][check.method]); !strings.Contains(op, check.want) {
			t.Errorf("%s %s = %s, want %s", check.method, check.path, op, check.want)
		}
	}
}

func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(v)
	return string(out)
}

func TestResponseTypeWithWriteOnlyPanics(t *testing.T) {
	t.Parallel()
	msg := panicMessage(func() {
		brcrud.RegisterCRUD[user](brhttp.NewRouter(), "users", nopStore[user]{}, &brcrud.Options[user]{
			Response: brcrud.Output(func(u user) account { return account{} }),
		})
	})
	if !strings.Contains(msg, "Response type account has writeonly fields") {
		t.Errorf("panic = %q", msg)
	}
}
