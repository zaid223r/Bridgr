package bridgr_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"

	bridgr "github.com/zaid223r/Bridgr"
	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brstore"
)

type ArchiveIn struct {
	ID     int64      `path:"id" json:"-"`
	Notify bool       `query:"notify" json:"-"`
	Tenant string     `header:"X-Tenant" json:"-"`
	Until  *time.Time `query:"until" json:"-"`
	Reason string     `json:"reason"`
}

func (in ArchiveIn) Validate() error {
	errs := &brerrors.ValidationError{}
	if len(in.Reason) > 20 {
		errs.Add("reason", "must be at most 20 characters")
	}
	return errs.OrNil()
}

type ArchiveOut struct {
	Todo   Todo       `json:"todo"`
	Reason string     `json:"reason"`
	Notify bool       `json:"notify"`
	Tenant string     `json:"tenant"`
	Until  *time.Time `json:"until"`
}

type SearchIn struct {
	Q     string `query:"q,required"`
	Limit *int   `query:"limit"`
}

type CacheIn struct{}

func typedApp(t *testing.T) *bridgr.App {
	t.Helper()
	store, err := brstore.NewMemory[Todo]()
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"buy milk", "walk dog", "milk the cow"} {
		if _, err := store.Create(t.Context(), Todo{Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	app := bridgr.New(bridgr.Config{Title: "Typed", Logger: quiet()})
	bridgr.CRUD[Todo](app, "todos", &bridgr.CRUDOptions[Todo]{Store: store})

	bridgr.Handle(app, "POST", "/todos/{id}/archive", func(ctx context.Context, in ArchiveIn) (ArchiveOut, error) {
		todo, err := store.Get(ctx, fmt.Sprint(in.ID))
		if err != nil {
			return ArchiveOut{}, err
		}
		return ArchiveOut{Todo: todo, Reason: in.Reason, Notify: in.Notify, Tenant: in.Tenant, Until: in.Until}, nil
	}, bridgr.WithOperationID("archiveTodo"), bridgr.WithSummary("Archive a todo"), bridgr.WithTags("todos"), bridgr.WithErrors(http.StatusNotFound))

	bridgr.Handle(app, "GET", "/search", func(ctx context.Context, in SearchIn) ([]Todo, error) {
		all, err := store.List(ctx, brstore.Query{})
		if err != nil {
			return nil, err
		}
		var out []Todo
		for _, t := range all {
			if strings.Contains(t.Title, in.Q) {
				out = append(out, t)
			}
		}
		if in.Limit != nil && *in.Limit < len(out) {
			out = out[:*in.Limit]
		}
		return out, nil
	})

	bridgr.Handle(app, "DELETE", "/cache", func(context.Context, CacheIn) (bridgr.NoContent, error) {
		return bridgr.NoContent{}, nil
	})
	return app
}

func call(t *testing.T, h http.Handler, method, path, body string, header map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTypedHandlerInputs(t *testing.T) {
	t.Parallel()
	h := typedApp(t).Handler()

	rec := call(t, h, "POST", "/todos/2/archive?notify=true&until=2026-12-01T00:00:00Z", `{"reason":"done"}`, map[string]string{"X-Tenant": "acme"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out ArchiveOut
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Todo.Title != "walk dog" || out.Reason != "done" || !out.Notify || out.Tenant != "acme" || out.Until == nil || out.Until.Month() != 12 {
		t.Errorf("out = %+v", out)
	}

	empty := call(t, h, "POST", "/todos/1/archive", "", nil)
	_ = json.Unmarshal(empty.Body.Bytes(), &out)
	if empty.Code != http.StatusOK || out.Reason != "" || out.Notify || out.Until != nil {
		t.Errorf("empty body and no optional params: %d %+v", empty.Code, out)
	}

	search := call(t, h, "GET", "/search?q=milk&limit=1", "", nil)
	var found []Todo
	_ = json.Unmarshal(search.Body.Bytes(), &found)
	if search.Code != http.StatusOK || len(found) != 1 || found[0].Title != "buy milk" {
		t.Errorf("search = %d %s", search.Code, search.Body)
	}

	del := call(t, h, "DELETE", "/cache", "", nil)
	if del.Code != http.StatusNoContent || del.Body.Len() != 0 {
		t.Errorf("NoContent = %d %q", del.Code, del.Body)
	}
}

func TestTypedHandlerErrors(t *testing.T) {
	t.Parallel()
	h := typedApp(t).Handler()
	tests := []struct {
		name, method, path, body string
		wantStatus               int
		wantField                string
		wantDetail               string
	}{
		{"path param not an int", "POST", "/todos/abc/archive", `{}`, 400, "id", `invalid id "abc": want an integer`},
		{"bad bool query", "POST", "/todos/1/archive?notify=maybe", `{}`, 400, "notify", `invalid notify "maybe": want true or false`},
		{"bad time query", "POST", "/todos/1/archive?until=tomorrow", `{}`, 400, "until", `invalid until "tomorrow": want an RFC 3339 time`},
		{"missing required query", "GET", "/search", "", 400, "q", `invalid q "": is required`},
		{"body is not an object", "POST", "/todos/1/archive", `["x"]`, 400, "", "invalid JSON body: body must be a JSON object"},
		{"malformed body", "POST", "/todos/1/archive", `{"reason":`, 400, "", ""},
		{"validation", "POST", "/todos/1/archive", `{"reason":"this reason is far too long"}`, 422, "", "reason: must be at most 20 characters"},
		{"handler returns not found", "POST", "/todos/99/archive", `{}`, 404, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := call(t, h, tt.method, tt.path, tt.body, nil)
			var p brerrors.Problem
			_ = json.Unmarshal(rec.Body.Bytes(), &p)
			if rec.Code != tt.wantStatus || rec.Header().Get("Content-Type") != brerrors.ContentType {
				t.Fatalf("status %d (%s), want %d problem", rec.Code, rec.Body, tt.wantStatus)
			}
			if tt.wantField != "" && !slices.Equal(p.InvalidFields, []string{tt.wantField}) {
				t.Errorf("invalid_fields = %v", p.InvalidFields)
			}
			if tt.wantDetail != "" && p.Detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", p.Detail, tt.wantDetail)
			}
			if tt.wantStatus == 422 && (len(p.Errors) != 1 || p.Errors[0].Field != "reason") {
				t.Errorf("errors = %+v", p.Errors)
			}
		})
	}
}

type noWildcardField struct {
	Name string `json:"name"`
}

type extraPathField struct {
	ID   int `path:"id" json:"-"`
	Slug int `path:"slug" json:"-"`
}

type unsupportedParam struct {
	Tags []string `query:"tags" json:"-"`
}

type getWithBody struct {
	Q    string `query:"q" json:"-"`
	Body string `json:"body"`
}

func TestTypedHandlerRegistrationPanics(t *testing.T) {
	t.Parallel()
	fn := func(context.Context, noWildcardField) (bridgr.NoContent, error) { return bridgr.NoContent{}, nil }
	tests := []struct {
		name     string
		register func(app *bridgr.App)
		want     string
	}{
		{"wildcard without field", func(app *bridgr.App) { bridgr.Handle(app, "POST", "/things/{id}", fn) }, "wildcard {id} has no field tagged path"},
		{"path field without wildcard", func(app *bridgr.App) {
			bridgr.Handle(app, "POST", "/things/{id}", func(context.Context, extraPathField) (bridgr.NoContent, error) { return bridgr.NoContent{}, nil })
		}, `field tagged path:"slug" has no {slug} in the path`},
		{"unsupported parameter type", func(app *bridgr.App) {
			bridgr.Handle(app, "GET", "/things", func(context.Context, unsupportedParam) (bridgr.NoContent, error) { return bridgr.NoContent{}, nil })
		}, "unsupported parameter type []string"},
		{"GET with body fields", func(app *bridgr.App) {
			bridgr.Handle(app, "GET", "/things", func(context.Context, getWithBody) (bridgr.NoContent, error) { return bridgr.NoContent{}, nil })
		}, "GET requests have no body"},
		{"input not a struct", func(app *bridgr.App) {
			bridgr.Handle(app, "GET", "/things", func(context.Context, string) (bridgr.NoContent, error) { return bridgr.NoContent{}, nil })
		}, "input type string must be a struct"},
		{"after start", func(app *bridgr.App) {
			_ = app.Handler()
			bridgr.Handle(app, "POST", "/things", fn)
		}, "after the app started"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				r := recover()
				if r == nil || !strings.Contains(fmt.Sprint(r), tt.want) {
					t.Errorf("panic = %v, want it to contain %q", r, tt.want)
				}
			}()
			tt.register(bridgr.New(bridgr.Config{Logger: quiet()}))
		})
	}
}

func TestTypedHandlerOpenAPI(t *testing.T) {
	t.Parallel()
	app := typedApp(t)
	raw, err := app.OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	_ = json.Unmarshal(raw, &doc)

	var archive struct {
		OperationID string   `json:"operationId"`
		Tags        []string `json:"tags"`
		Parameters  []struct {
			Name, In string
			Required bool
			Schema   map[string]any
		} `json:"parameters"`
		RequestBody struct {
			Content map[string]struct {
				Schema struct {
					Properties map[string]any `json:"properties"`
				} `json:"schema"`
			} `json:"content"`
		} `json:"requestBody"`
		Responses map[string]any `json:"responses"`
	}
	if err := json.Unmarshal(doc.Paths["/todos/{id}/archive"]["post"], &archive); err != nil {
		t.Fatal(err)
	}
	if archive.OperationID != "archiveTodo" || !slices.Equal(archive.Tags, []string{"todos"}) {
		t.Errorf("operation = %+v", archive)
	}
	var params []string
	for _, p := range archive.Parameters {
		params = append(params, fmt.Sprintf("%s:%s:%v:%v", p.In, p.Name, p.Required, p.Schema["type"]))
	}
	if want := "[path:id:true:integer query:notify:false:boolean header:X-Tenant:false:string query:until:false:string]"; fmt.Sprint(params) != want {
		t.Errorf("parameters = %v, want %s", params, want)
	}
	if props := archive.RequestBody.Content["application/json"].Schema.Properties; len(props) != 1 || props["reason"] == nil {
		t.Errorf("body properties = %v, want only reason", props)
	}
	var codes []string
	for c := range archive.Responses {
		codes = append(codes, c)
	}
	slices.Sort(codes)
	if want := []string{"200", "400", "404", "413", "422", "500"}; !slices.Equal(codes, want) {
		t.Errorf("responses = %v, want %v", codes, want)
	}
	if doc.Paths["/todos/{id}"]["get"] == nil {
		t.Error("custom path displaced the CRUD path items")
	}
	if !bytes.Contains(doc.Paths["/search"]["get"], []byte(`"operationId": "getSearch"`)) || !bytes.Contains(doc.Paths["/cache"]["delete"], []byte(`"204"`)) {
		t.Error("default operationId or 204 response missing")
	}
}

// TestTypedHandlerTrafficMatchesSpec validates the document and real
// requests and responses against it.
func TestTypedHandlerTrafficMatchesSpec(t *testing.T) {
	t.Parallel()
	app := typedApp(t)
	raw, err := app.OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := libopenapi.NewDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	v, errs := validator.NewValidator(doc)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	if ok, verrs := v.ValidateDocument(); !ok {
		for _, e := range verrs {
			t.Errorf("document: %s: %s", e.Message, e.Reason)
		}
	}

	srv := httptest.NewServer(app.Handler())
	defer srv.Close()
	steps := []struct{ method, path, body string }{
		{"POST", "/todos/2/archive?notify=true", `{"reason":"done"}`},
		{"POST", "/todos/99/archive", `{"reason":"x"}`},
		{"POST", "/todos/1/archive", `{"reason":"this reason is far too long"}`},
		{"GET", "/search?q=milk", ""},
		{"GET", "/search", ""},
		{"DELETE", "/cache", ""},
	}
	for _, s := range steps {
		build := func() *http.Request {
			var body io.Reader
			if s.body != "" {
				body = strings.NewReader(s.body)
			}
			req, _ := http.NewRequestWithContext(t.Context(), s.method, srv.URL+s.path, body)
			if s.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			return req
		}
		resp, err := srv.Client().Do(build())
		if err != nil {
			t.Fatal(err)
		}
		payload, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(payload))
		if ok, verrs := v.ValidateHttpResponse(build(), resp); !ok {
			for _, e := range verrs {
				t.Errorf("%s %s -> %d: %s", s.method, s.path, resp.StatusCode, e.Error())
			}
		}
	}
}
