package brhttp_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zaid223r/Bridgr/core/brhttp"
)

func newTestRouter() *brhttp.Router {
	r := brhttp.NewRouter()
	r.AddRoute("GET", "/todos", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("list"))
	})
	r.AddRoute("POST", "/todos", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})
	r.AddRoute("GET", "/todos/{id}", func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte("get " + req.PathValue("id")))
	})
	r.AddRoute("DELETE", "/todos/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	r.AddRoute("", "/any", func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte(req.Method))
	})
	return r
}

func TestRouter(t *testing.T) {
	t.Parallel()
	router := newTestRouter()

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantBody   string
		wantAllow  string
	}{
		{name: "static route", method: "GET", path: "/todos", wantStatus: 200, wantBody: "list"},
		{name: "same path other method", method: "POST", path: "/todos", wantStatus: 201},
		{name: "path value", method: "GET", path: "/todos/42", wantStatus: 200, wantBody: "get 42"},
		{name: "path value is unescaped", method: "GET", path: "/todos/a%20b", wantStatus: 200, wantBody: "get a b"},
		{name: "HEAD served by GET route", method: "HEAD", path: "/todos", wantStatus: 200},
		{name: "trailing slash does not match", method: "GET", path: "/todos/", wantStatus: 404},
		{name: "extra segment does not match", method: "GET", path: "/todos/1/extra", wantStatus: 404},
		{name: "unknown path", method: "GET", path: "/nope", wantStatus: 404},
		{name: "wrong method", method: "PUT", path: "/todos/1", wantStatus: 405, wantAllow: "DELETE, GET, HEAD"},
		{name: "empty method matches any", method: "PATCH", path: "/any", wantStatus: 200, wantBody: "PATCH"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantBody != "" && rec.Body.String() != tt.wantBody {
				t.Errorf("body = %q, want %q", rec.Body.String(), tt.wantBody)
			}
			if tt.wantAllow != "" && rec.Header().Get("Allow") != tt.wantAllow {
				t.Errorf("Allow = %q, want %q", rec.Header().Get("Allow"), tt.wantAllow)
			}
		})
	}
}

func TestRouterPanicsOnConflictingRoute(t *testing.T) {
	t.Parallel()
	router := brhttp.NewRouter()
	noop := func(http.ResponseWriter, *http.Request) {}
	router.AddRoute("GET", "/todos/{id}", noop)

	defer func() {
		if recover() == nil {
			t.Fatal("registering a conflicting route did not panic")
		}
	}()
	router.AddRoute("GET", "/todos/{key}", noop)
}
