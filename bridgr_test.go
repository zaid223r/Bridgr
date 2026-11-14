package bridgr_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	bridgr "github.com/zaid223r/Bridgr"
)

type Todo struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

func (Todo) GenericFiltering() bool { return true }

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

func newApp(t *testing.T) *bridgr.App {
	t.Helper()
	app := bridgr.New(bridgr.Config{Title: "Todo API", Version: "2.0.0", Logger: quiet(), DB: bridgr.Memory()})
	bridgr.CRUD[Todo](app, "todos", nil)
	return app
}

func TestAppServesCRUDAndDocs(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(newApp(t).Handler())
	defer srv.Close()

	resp, err := srv.Client().Post(srv.URL+"/todos", "application/json", strings.NewReader(`{"title":"ship"}`)) //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create: status %d", resp.StatusCode)
	}

	for path, want := range map[string]int{
		"/todos?done=false": http.StatusOK,
		"/todos/1":          http.StatusOK,
		"/openapi.json":     http.StatusOK,
		"/docs":             http.StatusOK,
		"/nope":             http.StatusNotFound,
	} {
		resp, err := srv.Client().Get(srv.URL + path) //nolint:noctx // test
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("GET %s: status %d, want %d", path, resp.StatusCode, want)
		}
	}

	resp, err = srv.Client().Get(srv.URL + "/openapi.json") //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var doc struct {
		Info  map[string]string `json:"info"`
		Paths map[string]any    `json:"paths"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.Info["title"] != "Todo API" || doc.Info["version"] != "2.0.0" || doc.Paths["/todos"] == nil {
		t.Errorf("document = %v, paths %v", doc.Info, doc.Paths)
	}
}

func TestRegisteringAfterStartPanics(t *testing.T) {
	t.Parallel()
	app := newApp(t)
	_ = app.Handler()

	defer func() {
		r := recover()
		if r == nil || !strings.Contains(fmt.Sprint(r), "after the app started") {
			t.Errorf("recover() = %v, want a clear panic", r)
		}
	}()
	bridgr.CRUD[Todo](app, "late", nil)
}

func TestHandlerIsBuiltOnce(t *testing.T) {
	t.Parallel()
	app := newApp(t)
	first, second := app.Handler(), app.Handler()
	if fmt.Sprintf("%p", first) != fmt.Sprintf("%p", second) {
		t.Error("Handler built the server twice")
	}
}

func TestServeShutsDownOnCancel(t *testing.T) {
	t.Parallel()
	app := newApp(t)
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, ln) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/todos") //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "[]\n" {
		t.Fatalf("GET /todos = %d %q", resp.StatusCode, body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve = %v, want nil after a clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
}

func TestRouterEscapeHatch(t *testing.T) {
	t.Parallel()
	app := newApp(t)
	app.Router().AddRoute("GET", "/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/health", nil))
	if rec.Body.String() != "ok" {
		t.Errorf("custom route = %q", rec.Body.String())
	}
}

func TestOpenAPIWithoutStarting(t *testing.T) {
	t.Parallel()
	app := newApp(t)
	doc, err := app.OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), `"title": "Todo API"`) || !strings.Contains(string(doc), `"/todos/{id}"`) {
		t.Errorf("document missing title or paths")
	}
	bridgr.CRUD[Todo](app, "still-open", nil) // OpenAPI must not freeze registration
}

func TestAddMiddleware(t *testing.T) {
	t.Parallel()
	app := newApp(t)
	app.AddMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-From-Module", "yes")
			next.ServeHTTP(w, r)
		})
	})
	rec := httptest.NewRecorder()
	app.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/todos", nil))
	if rec.Header().Get("X-From-Module") != "yes" || rec.Header().Get("X-Request-ID") == "" {
		t.Errorf("headers = %v", rec.Header())
	}
	defer func() {
		if recover() == nil {
			t.Error("AddMiddleware after start must panic")
		}
	}()
	app.AddMiddleware(func(h http.Handler) http.Handler { return h })
}
