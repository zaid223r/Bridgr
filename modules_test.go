package bridgr_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	bridgr "github.com/zaid223r/Bridgr"
)

// recorder logs lifecycle calls from every module that shares it.
type recorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
}

func (r *recorder) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.calls)
}

type lifecycleModule struct {
	name        string
	rec         *recorder
	startErr    error
	stopErr     error
	registerErr error
}

func (m *lifecycleModule) Name() string { return m.name }

func (m *lifecycleModule) Register(*bridgr.App) error {
	m.rec.add("register " + m.name)
	return m.registerErr
}

func (m *lifecycleModule) Start(context.Context) error {
	m.rec.add("start " + m.name)
	return m.startErr
}

func (m *lifecycleModule) Stop(context.Context) error {
	m.rec.add("stop " + m.name)
	return m.stopErr
}

// plainModule has no Start or Stop.
type plainModule struct{ name string }

func (m plainModule) Name() string             { return m.name }
func (plainModule) Register(*bridgr.App) error { return nil }

func TestUseErrors(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	tests := []struct {
		name string
		use  func(app *bridgr.App) error
		want string
	}{
		{"nil module", func(app *bridgr.App) error { return app.Use(nil) }, "nil module"},
		{"empty name", func(app *bridgr.App) error { return app.Use(plainModule{""}) }, `invalid module name ""`},
		{"name with a space", func(app *bridgr.App) error { return app.Use(plainModule{"rate limit"}) }, `invalid module name "rate limit"`},
		{"duplicate name", func(app *bridgr.App) error { return app.Use(plainModule{"auth"}, plainModule{"auth"}) }, "Use(auth): a module with this name is already registered"},
		{"register fails", func(app *bridgr.App) error {
			return app.Use(&lifecycleModule{name: "jobs", rec: rec, registerErr: errors.New("no queue")})
		}, "registering module jobs: no queue"},
		{"after start", func(app *bridgr.App) error {
			_ = app.Handler()
			return app.Use(plainModule{"late"})
		}, "Use(late): the app has already started"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.use(bridgr.New(bridgr.Config{Logger: quiet()}))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestModuleLifecycleOrder(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	app := bridgr.New(bridgr.Config{Logger: quiet()})
	if err := app.Use(&lifecycleModule{name: "a", rec: rec}, plainModule{"plain"}, &lifecycleModule{name: "b", rec: rec}); err != nil {
		t.Fatal(err)
	}
	if got := len(app.Modules()); got != 3 {
		t.Errorf("Modules() has %d entries, want 3", got)
	}
	if err := app.StartModules(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := app.StopModules(t.Context()); err != nil {
		t.Fatal(err)
	}
	want := []string{"register a", "register b", "start a", "start b", "stop b", "stop a"}
	if got := rec.list(); !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

func TestStartFailureRollsBack(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	app := bridgr.New(bridgr.Config{Logger: quiet()})
	err := app.Use(
		&lifecycleModule{name: "a", rec: rec},
		&lifecycleModule{name: "b", rec: rec, startErr: errors.New("port in use")},
		&lifecycleModule{name: "c", rec: rec},
	)
	if err != nil {
		t.Fatal(err)
	}
	err = app.StartModules(t.Context())
	if err == nil || !strings.Contains(err.Error(), "starting module b: port in use") {
		t.Fatalf("err = %v", err)
	}
	want := []string{"register a", "register b", "register c", "start a", "start b", "stop a"}
	if got := rec.list(); !slices.Equal(got, want) {
		t.Errorf("calls = %v, want %v (only a is stopped; c never started)", got, want)
	}
}

func TestStopErrorsAreJoined(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	app := bridgr.New(bridgr.Config{Logger: quiet()})
	_ = app.Use(
		&lifecycleModule{name: "a", rec: rec, stopErr: errors.New("flush failed")},
		&lifecycleModule{name: "b", rec: rec, stopErr: errors.New("conn reset")},
	)
	err := app.StopModules(t.Context())
	for _, want := range []string{"stopping module a: flush failed", "stopping module b: conn reset"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to contain %q", err, want)
		}
	}
	if got := rec.list(); !slices.Equal(got[len(got)-2:], []string{"stop b", "stop a"}) {
		t.Errorf("both modules must be stopped despite errors: %v", got)
	}
}

func TestServeRunsModuleLifecycle(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	app := newApp(t)
	if err := app.Use(&lifecycleModule{name: "jobs", rec: rec}); err != nil {
		t.Fatal(err)
	}
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
	_ = resp.Body.Close()
	if got := rec.list(); !slices.Contains(got, "start jobs") {
		t.Errorf("module not started before serving: %v", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
	if got := rec.list(); got[len(got)-1] != "stop jobs" {
		t.Errorf("module not stopped after shutdown: %v", got)
	}
}

func TestServeReturnsStartError(t *testing.T) {
	t.Parallel()
	app := newApp(t)
	_ = app.Use(&lifecycleModule{name: "broken", rec: &recorder{}, startErr: errors.New("bad config")})
	ln, _ := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	defer ln.Close()
	if err := app.Serve(t.Context(), ln); err == nil || !strings.Contains(err.Error(), "bad config") {
		t.Errorf("Serve = %v, want the start error", err)
	}
}

type docsModule struct{}

func (docsModule) Name() string { return "docs-extra" }

func (docsModule) Register(app *bridgr.App) error {
	app.Docs().Transform(func(doc map[string]any) error {
		components := doc["components"].(map[string]any)
		components["securitySchemes"] = map[string]any{"bearer": map[string]any{"type": "http", "scheme": "bearer"}}
		return nil
	})
	return nil
}

type failingDocsModule struct{}

func (failingDocsModule) Name() string { return "docs-broken" }

func (failingDocsModule) Register(app *bridgr.App) error {
	app.Docs().Transform(func(map[string]any) error { return errors.New("no components") })
	return nil
}

func TestDocsTransform(t *testing.T) {
	t.Parallel()
	app := newApp(t)
	if err := app.Use(docsModule{}); err != nil {
		t.Fatal(err)
	}
	doc, err := app.OpenAPI()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), `"securitySchemes"`) || !strings.Contains(string(doc), `"/todos"`) {
		t.Errorf("transform not applied or document damaged")
	}

	broken := newApp(t)
	_ = broken.Use(failingDocsModule{})
	if _, err := broken.OpenAPI(); err == nil || !strings.Contains(err.Error(), "no components") {
		t.Errorf("OpenAPI() = %v, want the transform error", err)
	}
}
