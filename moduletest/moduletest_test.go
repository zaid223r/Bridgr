package moduletest_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	bridgr "github.com/zaid223r/Bridgr"
	"github.com/zaid223r/Bridgr/moduletest"
)

// greeter adds a documented route.
type greeter struct{}

func (greeter) Name() string { return "greeter" }

func (greeter) Register(app *bridgr.App) error {
	app.Router().AddRoute("GET", "/hello", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})
	app.Docs().Transform(func(doc map[string]any) error {
		doc["paths"].(map[string]any)["/hello"] = map[string]any{
			"get": map[string]any{
				"operationId": "hello",
				"responses":   map[string]any{"200": map[string]any{"description": "A greeting."}},
			},
		}
		return nil
	})
	return nil
}

// ticker runs a background goroutine between Start and Stop.
type ticker struct {
	mu   sync.Mutex
	stop chan struct{}
	done chan struct{}
	runs atomic.Int64
}

func (*ticker) Name() string               { return "ticker" }
func (*ticker) Register(*bridgr.App) error { return nil }

func (k *ticker) Start(context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.stop, k.done = make(chan struct{}), make(chan struct{})
	go func(stop, done chan struct{}) {
		defer close(done)
		t := time.NewTicker(time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				k.runs.Add(1)
			}
		}
	}(k.stop, k.done)
	return nil
}

func (k *ticker) Stop(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stop == nil {
		return nil
	}
	close(k.stop)
	k.stop = nil
	select {
	case <-k.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestGoodModulesPass(t *testing.T) {
	t.Parallel()
	moduletest.Run(t, func() bridgr.Module { return greeter{} })
	moduletest.Run(t, func() bridgr.Module { return &ticker{} })
}

// configurable lets each case break one part of the contract.
type configurable struct {
	name          string
	registerErr   error
	transform     func(doc map[string]any) error
	startErr      error
	stopErr       func(calls int64) error
	stops         atomic.Int64
	nameCalls     atomic.Int64
	unstableNames bool
}

func (c *configurable) Name() string {
	if c.unstableNames {
		return c.name + "-" + string(rune('a'+c.nameCalls.Add(1)))
	}
	return c.name
}

func (c *configurable) Register(app *bridgr.App) error {
	if c.transform != nil {
		app.Docs().Transform(c.transform)
	}
	return c.registerErr
}

func (c *configurable) Start(context.Context) error { return c.startErr }

func (c *configurable) Stop(context.Context) error {
	n := c.stops.Add(1)
	if c.stopErr != nil {
		return c.stopErr(n)
	}
	return nil
}

func TestBrokenModulesAreCaught(t *testing.T) {
	t.Parallel()
	var unstable atomic.Int64
	tests := []struct {
		name   string
		module func() bridgr.Module
		want   string
	}{
		{"empty name", func() bridgr.Module { return &configurable{} }, `Name() = "": want a non-empty name`},
		{"unstable name", func() bridgr.Module {
			return &configurable{name: "m" + string(rune('0'+unstable.Add(1)%10))}
		}, "Name() is not stable"},
		{"register fails", func() bridgr.Module {
			return &configurable{name: "x", registerErr: errors.New("missing secret")}
		}, "missing secret"},
		{"invalid OpenAPI", func() bridgr.Module {
			return &configurable{name: "x", transform: func(doc map[string]any) error {
				doc["paths"].(map[string]any)["/broken"] = "not a path item"
				return nil
			}}
		}, "OpenAPI document is invalid"},
		{"drops other registrations", func() bridgr.Module {
			return &configurable{name: "x", transform: func(doc map[string]any) error {
				doc["paths"] = map[string]any{}
				return nil
			}}
		}, `OpenAPI document lost "/widgets"`},
		{"start fails", func() bridgr.Module {
			return &configurable{name: "x", startErr: errors.New("cannot bind")}
		}, "start: bridgr: starting module x: cannot bind"},
		{"stop fails", func() bridgr.Module {
			return &configurable{name: "x", stopErr: func(int64) error { return errors.New("flush failed") }}
		}, "stop: bridgr: stopping module x: flush failed"},
		{"second stop fails", func() bridgr.Module {
			return &configurable{name: "x", stopErr: func(n int64) error {
				if n == 2 {
					return errors.New("already stopped")
				}
				return nil
			}}
		}, "second stop: bridgr: stopping module x: already stopped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			problems := moduletest.Check(t.Context(), tt.module)
			var all []string
			for _, p := range problems {
				all = append(all, p.Error())
			}
			if !strings.Contains(strings.Join(all, "\n"), tt.want) {
				t.Errorf("problems = %q, want one containing %q", all, tt.want)
			}
		})
	}
}
