package brmw_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zaid223r/Bridgr/core/brmw"
)

var ok = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("X-Total-Count", "3")
	_, _ = w.Write([]byte("body"))
})

func do(h http.Handler, method, origin string, extra map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/todos", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCORSSimpleRequests(t *testing.T) {
	t.Parallel()
	h := brmw.CORS(brmw.CORSOptions{AllowedOrigins: []string{"https://app.example.com"}})(ok)

	allowed := do(h, "GET", "https://APP.example.com", nil) // origins compare case-insensitively
	if allowed.Header().Get("Access-Control-Allow-Origin") != "https://APP.example.com" {
		t.Errorf("allowed origin not echoed: %v", allowed.Header())
	}
	if got := allowed.Header().Get("Access-Control-Expose-Headers"); got != "X-Total-Count, Link, X-Request-ID" {
		t.Errorf("exposed headers = %q", got)
	}
	if allowed.Header().Get("Access-Control-Allow-Credentials") != "" || allowed.Body.String() != "body" {
		t.Error("credentials header set without AllowCredentials, or handler skipped")
	}

	for _, origin := range []string{"https://evil.example", ""} {
		rec := do(h, "GET", origin, nil)
		if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Body.String() != "body" {
			t.Errorf("origin %q: got CORS headers %v or no body", origin, rec.Header())
		}
		if !strings.Contains(strings.Join(rec.Header().Values("Vary"), ","), "Origin") {
			t.Errorf("origin %q: missing Vary: Origin (caches must not mix origins)", origin)
		}
	}
}

func TestCORSPreflight(t *testing.T) {
	t.Parallel()
	var reached atomic.Int64
	h := brmw.CORS(brmw.CORSOptions{
		AllowedOrigins:   []string{"https://app.example.com"},
		AllowCredentials: true,
		MaxAge:           time.Hour,
	})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached.Add(1) }))

	rec := do(h, "OPTIONS", "https://app.example.com", map[string]string{
		"Access-Control-Request-Method":  "PATCH",
		"Access-Control-Request-Headers": "content-type",
	})
	want := map[string]string{
		"Access-Control-Allow-Origin":      "https://app.example.com",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Allow-Methods":     "GET, POST, PUT, PATCH, DELETE",
		"Access-Control-Allow-Headers":     "Content-Type, Authorization, X-Request-ID",
		"Access-Control-Max-Age":           "3600",
	}
	if rec.Code != http.StatusNoContent || reached.Load() != 0 {
		t.Errorf("status = %d, handler calls = %d: an allowed preflight must be answered by CORS", rec.Code, reached.Load())
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	// A preflight from a disallowed origin falls through to the app (405 there).
	blocked := do(h, "OPTIONS", "https://evil.example", map[string]string{"Access-Control-Request-Method": "GET"})
	if blocked.Header().Get("Access-Control-Allow-Origin") != "" || reached.Load() != 1 {
		t.Errorf("disallowed origin: CORS answered it (handler calls %d)", reached.Load())
	}
}

func TestCORSWildcardAndCustom(t *testing.T) {
	t.Parallel()
	h := brmw.CORS(brmw.CORSOptions{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET"},
		ExposedHeaders: []string{"X-Custom"},
	})(ok)
	rec := do(h, "GET", "https://anything.example", nil)
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Access-Control-Expose-Headers") != "X-Custom" {
		t.Errorf("headers = %v", rec.Header())
	}
	pre := do(h, "OPTIONS", "https://anything.example", map[string]string{"Access-Control-Request-Method": "GET"})
	if pre.Header().Get("Access-Control-Allow-Methods") != "GET" || pre.Header().Get("Access-Control-Max-Age") != "600" {
		t.Errorf("preflight = %v", pre.Header())
	}
}

func TestCORSRejectsUnsafeConfig(t *testing.T) {
	t.Parallel()
	for name, opts := range map[string]brmw.CORSOptions{
		"no origins":         {},
		"credentials with *": {AllowedOrigins: []string{"*"}, AllowCredentials: true},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: CORS accepted an unsafe config", name)
				}
			}()
			brmw.CORS(opts)
		}()
	}
}
