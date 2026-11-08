package bridgr_test

import (
	"context"
	"strings"
	"testing"
	"time"

	bridgr "github.com/zaid223r/Bridgr"
	"github.com/zaid223r/Bridgr/core/brcap"
)

type greeterCap interface{ Greet() string }

type english struct{}

func (english) Greet() string { return "hello" }

func TestProvideAndLookup(t *testing.T) {
	t.Parallel()
	app := bridgr.New(bridgr.Config{Logger: quiet()})
	if _, ok := bridgr.Lookup[greeterCap](app); ok {
		t.Error("Lookup found a capability nobody provided")
	}
	if err := bridgr.Provide[greeterCap](app, english{}); err != nil {
		t.Fatal(err)
	}
	g, ok := bridgr.Lookup[greeterCap](app)
	if !ok || g.Greet() != "hello" {
		t.Errorf("Lookup = %v, %v", g, ok)
	}

	errs := map[string]error{
		"duplicate":     bridgr.Provide[greeterCap](app, english{}),
		"not interface": bridgr.Provide[english](app, english{}),
		"nil":           bridgr.Provide[greeterCap](app, nil),
	}
	want := map[string]string{
		"duplicate":     "already provided by another module",
		"not interface": "capabilities must be interface types",
		"nil":           "nil implementation",
	}
	for name, err := range errs {
		if err == nil || !strings.Contains(err.Error(), want[name]) {
			t.Errorf("%s: err = %v, want %q", name, err, want[name])
		}
	}

	_ = app.Handler()
	if err := bridgr.Provide[brcap.KV](app, brcap.NewMemoryKV()); err == nil || !strings.Contains(err.Error(), "already started") {
		t.Errorf("Provide after start: %v", err)
	}
	if _, ok := bridgr.Lookup[greeterCap](app); !ok {
		t.Error("Lookup must keep working after start")
	}
}

// kvModule provides the KV capability; counterModule uses whatever KV is
// available when it starts, the way real modules cooperate without importing
// each other.
type kvModule struct{ kv *brcap.MemoryKV }

func (kvModule) Name() string { return "kv" }

func (m kvModule) Register(app *bridgr.App) error {
	return bridgr.Provide[brcap.KV](app, m.kv)
}

type counterModule struct {
	app *bridgr.App
	kv  brcap.KV
}

func (*counterModule) Name() string { return "counter" }

func (m *counterModule) Register(app *bridgr.App) error {
	m.app = app
	return nil
}

func (m *counterModule) Start(ctx context.Context) error {
	kv, ok := bridgr.Lookup[brcap.KV](m.app)
	if !ok {
		kv = brcap.NewMemoryKV() // fall back when no module provides one
	}
	m.kv = kv
	_, err := kv.Incr(ctx, "starts", 1, time.Hour)
	return err
}

func TestModulesShareCapabilities(t *testing.T) {
	t.Parallel()
	shared := brcap.NewMemoryKV()
	counter := &counterModule{}
	app := bridgr.New(bridgr.Config{Logger: quiet()})
	// The consumer registers first; it looks the capability up in Start, after
	// every module has registered.
	if err := app.Use(counter, kvModule{kv: shared}); err != nil {
		t.Fatal(err)
	}
	if err := app.StartModules(t.Context()); err != nil {
		t.Fatal(err)
	}
	if n, _ := shared.Incr(t.Context(), "starts", 0, 0); n != 1 {
		t.Errorf("counter module wrote to %d stores; want the shared KV to have 1 start", n)
	}

	alone := &counterModule{}
	solo := bridgr.New(bridgr.Config{Logger: quiet()})
	_ = solo.Use(alone)
	if err := solo.StartModules(t.Context()); err != nil {
		t.Fatalf("fallback: %v", err)
	}
	if alone.kv == nil {
		t.Error("module without a provider should fall back to MemoryKV")
	}
}
