package brhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zaid223r/Bridgr/core/brcontext"
	"github.com/zaid223r/Bridgr/core/brerrors"
)

// syncBuffer is a bytes.Buffer safe for the concurrent writes a server makes.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func jsonLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	return slog.New(slog.NewJSONHandler(buf, nil)), buf
}

func logLines(t *testing.T, buf *syncBuffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for line := range strings.Lines(buf.String()) {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q is not JSON: %v", line, err)
		}
		lines = append(lines, m)
	}
	return lines
}

func TestConfigDefaults(t *testing.T) {
	t.Parallel()
	s := NewServer(NewRouter(), Config{DisableDocs: true})

	checks := []struct {
		name      string
		got, want any
	}{
		{"Addr", s.srv.Addr, ":8080"},
		{"ReadHeaderTimeout", s.srv.ReadHeaderTimeout, 5 * time.Second},
		{"ReadTimeout", s.srv.ReadTimeout, 30 * time.Second},
		{"WriteTimeout", s.srv.WriteTimeout, 30 * time.Second},
		{"IdleTimeout", s.srv.IdleTimeout, 120 * time.Second},
		{"ShutdownTimeout", s.cfg.ShutdownTimeout, 15 * time.Second},
		{"MaxBodyBytes", s.cfg.MaxBodyBytes, int64(1 << 20)},
		{"Logger", s.cfg.Logger, slog.Default()},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}

	custom := NewServer(NewRouter(), Config{Addr: ":9000", WriteTimeout: time.Second, DisableDocs: true})
	if custom.srv.Addr != ":9000" || custom.srv.WriteTimeout != time.Second {
		t.Errorf("explicit values overridden: addr %q, write timeout %v", custom.srv.Addr, custom.srv.WriteTimeout)
	}
}

func TestBodyLimit(t *testing.T) {
	t.Parallel()
	router := NewRouter()
	router.AddRoute("POST", "/echo", func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.ReadAll(r.Body); err != nil {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
		}
	})

	tests := []struct {
		name    string
		limit   int64
		bodyLen int
		want    int
	}{
		{"under the limit", 10, 10, http.StatusOK},
		{"over the limit", 10, 11, http.StatusRequestEntityTooLarge},
		{"negative disables the limit", -1, 5 << 20, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, _ := jsonLogger()
			h := NewServer(router, Config{MaxBodyBytes: tt.limit, Logger: logger, DisableDocs: true}).Handler()
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest("POST", "/echo", strings.NewReader(strings.Repeat("x", tt.bodyLen))))
			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestRequestLogging(t *testing.T) {
	t.Parallel()
	router := NewRouter()
	router.AddRoute("GET", "/ok", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})
	router.AddRoute("GET", "/boom", func(w http.ResponseWriter, r *http.Request) {
		brerrors.WriteError(w, r, errors.New("db exploded"))
	})
	router.AddRoute("GET", "/ctx-logger", func(w http.ResponseWriter, r *http.Request) {
		brcontext.Logger(r.Context()).InfoContext(r.Context(), "from handler")
	})
	logger, buf := jsonLogger()
	h := NewServer(router, Config{Logger: logger, DisableDocs: true}).Handler()

	for _, path := range []string{"/ok", "/boom", "/ctx-logger"} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	}

	lines := logLines(t, buf)
	find := func(msg, path string) map[string]any {
		for _, l := range lines {
			if l["msg"] == msg && (path == "" || l["path"] == path) {
				return l
			}
		}
		t.Fatalf("no %q log line for %q in %v", msg, path, lines)
		return nil
	}

	ok := find("request", "/ok")
	if ok["level"] != "INFO" || ok["method"] != "GET" || ok["status"] != float64(200) || ok["bytes"] != float64(5) {
		t.Errorf("/ok log = %v", ok)
	}
	if _, has := ok["duration"]; !has {
		t.Error("request log has no duration")
	}
	if boom := find("request", "/boom"); boom["level"] != "ERROR" || boom["status"] != float64(500) {
		t.Errorf("/boom log = %v", boom)
	}
	if failed := find("request failed", "/boom"); failed["error"] != "db exploded" {
		t.Errorf("500 cause not logged through the server logger: %v", failed)
	}
	find("from handler", "")
}

func TestStatusRecorderUnwrap(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec, status: http.StatusOK}
	if err := http.NewResponseController(sr).Flush(); err != nil {
		t.Errorf("Flush through statusRecorder: %v", err)
	}
	if !rec.Flushed {
		t.Error("underlying writer was not flushed")
	}
}

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func TestGracefulShutdown(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	router := NewRouter()
	router.AddRoute("GET", "/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("done"))
	})

	logger, _ := jsonLogger()
	s := NewServer(router, Config{Logger: logger, ShutdownTimeout: 5 * time.Second, DisableDocs: true})
	ln := listen(t)
	ctx, cancel := context.WithCancel(t.Context())
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(ctx, ln) }()

	type result struct {
		body string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/slow") //nolint:noctx // plain client is enough here
		if err != nil {
			got <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		got <- result{string(b), err}
	}()

	<-started
	cancel()
	select {
	case err := <-serveErr:
		t.Fatalf("Serve returned before the in-flight request finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)

	if r := <-got; r.err != nil || r.body != "done" {
		t.Fatalf("in-flight request = %q, %v; want it to complete", r.body, r.err)
	}
	if err := <-serveErr; err != nil {
		t.Fatalf("Serve after clean shutdown = %v, want nil", err)
	}

	resp, err := http.Get("http://" + ln.Addr().String() + "/slow") //nolint:noctx // plain client is enough here
	if err == nil {
		_ = resp.Body.Close()
		t.Error("server still accepts connections after shutdown")
	}
}

func TestShutdownTimeout(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	router := NewRouter()
	router.AddRoute("GET", "/stuck", func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	})
	logger, _ := jsonLogger()
	s := NewServer(router, Config{Logger: logger, ShutdownTimeout: 50 * time.Millisecond, DisableDocs: true})
	ln := listen(t)
	ctx, cancel := context.WithCancel(t.Context())
	serveErr := make(chan error, 1)
	go func() { serveErr <- s.Serve(ctx, ln) }()
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/stuck") //nolint:noctx // plain client is enough here
		if err == nil {
			_ = resp.Body.Close()
		}
	}()

	<-started
	cancel()
	if err := <-serveErr; !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Serve = %v, want a shutdown deadline error", err)
	}
}

func TestRunReportsListenError(t *testing.T) {
	t.Parallel()
	taken := listen(t)
	defer taken.Close()
	s := NewServer(NewRouter(), Config{Addr: taken.Addr().String(), DisableDocs: true})
	if err := s.Run(t.Context()); err == nil || !strings.Contains(err.Error(), "listening on") {
		t.Errorf("Run on a busy port = %v, want a listen error", err)
	}
}

func TestDocsRoutes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		disable     bool
		wantSpec    int
		wantDocs    int
		contentType string
	}{
		{"enabled by default", false, http.StatusOK, http.StatusOK, "application/json"},
		{"disabled", true, http.StatusNotFound, http.StatusNotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			logger, _ := jsonLogger()
			h := NewServer(NewRouter(), Config{Logger: logger, DisableDocs: tt.disable}).Handler()

			spec := httptest.NewRecorder()
			h.ServeHTTP(spec, httptest.NewRequest("GET", "/openapi.json", nil))
			if spec.Code != tt.wantSpec {
				t.Errorf("/openapi.json status = %d, want %d", spec.Code, tt.wantSpec)
			}
			if tt.contentType != "" && spec.Header().Get("Content-Type") != tt.contentType {
				t.Errorf("/openapi.json Content-Type = %q", spec.Header().Get("Content-Type"))
			}
			docs := httptest.NewRecorder()
			h.ServeHTTP(docs, httptest.NewRequest("GET", "/docs", nil))
			if docs.Code != tt.wantDocs {
				t.Errorf("/docs status = %d, want %d", docs.Code, tt.wantDocs)
			}
		})
	}
}

func TestSpecUsesConfigInfo(t *testing.T) {
	t.Parallel()
	logger, _ := jsonLogger()
	h := NewServer(NewRouter(), Config{Logger: logger, Title: "Shop API", Version: "3.2.1", Description: "Things to buy"}).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/openapi.json", nil))
	var doc struct {
		OpenAPI string            `json:"openapi"`
		Info    map[string]string `json:"info"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.OpenAPI != "3.1.0" || doc.Info["title"] != "Shop API" || doc.Info["version"] != "3.2.1" || doc.Info["description"] != "Things to buy" {
		t.Errorf("spec header = %s %v", doc.OpenAPI, doc.Info)
	}
}
