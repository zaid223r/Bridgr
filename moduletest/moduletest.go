// Package moduletest is the contract every Bridgr module must pass. A module
// repository runs it in its own tests:
//
//	func TestContract(t *testing.T) {
//		moduletest.Run(t, func() bridgr.Module { return auth.New(auth.Config{...}) })
//	}
//
// It checks that the module registers cleanly next to a CRUD resource and
// another module, keeps the OpenAPI document valid, doesn't break existing
// routes, and starts and stops within a deadline.
package moduletest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"

	bridgr "github.com/zaid223r/Bridgr"
)

// Timeout bounds each Start and Stop call.
const Timeout = 10 * time.Second

// Run checks newModule against the contract and reports each problem with
// t.Error. newModule must return a fresh, unregistered instance on every call.
func Run(t testing.TB, newModule func() bridgr.Module) {
	t.Helper()
	for _, err := range Check(context.Background(), newModule) {
		t.Error(err)
	}
}

// Widget is the CRUD resource every contract app has next to the module.
type Widget struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// probe is a second module, so the contract also checks the module under test
// coexists with others.
type probe struct{}

const probePath = "/moduletest/probe"

func (probe) Name() string { return "moduletest-probe" }

func (probe) Register(app *bridgr.App) error {
	app.Router().AddRoute("GET", probePath, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	app.Docs().Transform(func(doc map[string]any) error {
		doc["x-moduletest"] = true
		return nil
	})
	return nil
}

// Check runs the contract and returns every problem found; nil means the
// module passes. Run is the usual entry point; Check exists so the contract
// itself can be tested.
func Check(ctx context.Context, newModule func() bridgr.Module) []error {
	var problems []error
	fail := func(format string, args ...any) { problems = append(problems, fmt.Errorf(format, args...)) }

	m := newModule()
	if m == nil {
		return []error{errors.New("newModule returned nil")}
	}
	name := m.Name()
	if name == "" || strings.ContainsAny(name, " \t\n") {
		fail("Name() = %q: want a non-empty name without spaces", name)
	}
	if again := newModule().Name(); again != name {
		fail("Name() is not stable: %q, then %q", name, again)
	}

	app := bridgr.New(bridgr.Config{Title: "moduletest", Logger: slog.New(slog.DiscardHandler), DB: bridgr.Memory()})
	bridgr.CRUD[Widget](app, "widgets", nil)
	if err := app.Use(m, probe{}); err != nil {
		return append(problems, fmt.Errorf("registering next to a CRUD resource and another module: %w", err))
	}

	problems = append(problems, checkDocument(app)...)

	startCtx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	if err := app.StartModules(startCtx); err != nil {
		return append(problems, fmt.Errorf("start: %w", err))
	}
	problems = append(problems, checkRoutes(ctx, app.Handler())...)

	problems = append(problems, stopWithin(ctx, "stop", app.StopModules)...)
	problems = append(problems, stopWithin(ctx, "second stop", app.StopModules)...)

	fresh := bridgr.New(bridgr.Config{Logger: slog.New(slog.DiscardHandler)})
	if err := fresh.Use(newModule()); err != nil {
		fail("registering a second instance in a new app: %v", err)
	} else {
		problems = append(problems, stopWithin(ctx, "stop without start", fresh.StopModules)...)
	}
	return problems
}

func checkDocument(app *bridgr.App) []error {
	raw, err := app.OpenAPI()
	if err != nil {
		return []error{fmt.Errorf("building the OpenAPI document: %w", err)}
	}
	var problems []error
	for _, want := range []string{`"/widgets"`, `"/widgets/{id}"`, `"x-moduletest": true`} {
		if !strings.Contains(string(raw), want) {
			problems = append(problems, fmt.Errorf("OpenAPI document lost %s: the module must keep other registrations", want))
		}
	}
	doc, err := libopenapi.NewDocument(raw)
	if err != nil {
		return append(problems, fmt.Errorf("parsing the OpenAPI document: %w", err))
	}
	v, errs := validator.NewValidator(doc)
	if len(errs) > 0 {
		return append(problems, fmt.Errorf("building the OpenAPI validator: %w", errors.Join(errs...)))
	}
	if ok, verrs := v.ValidateDocument(); !ok {
		for _, e := range verrs {
			problems = append(problems, fmt.Errorf("OpenAPI document is invalid after registering: %s: %s", e.Message, e.Reason))
		}
	}
	return problems
}

// checkRoutes sends concurrent requests to routes the module doesn't own; they
// must keep working.
func checkRoutes(ctx context.Context, h http.Handler) []error {
	var mu sync.Mutex
	var problems []error
	var wg sync.WaitGroup
	for range 10 {
		for _, path := range []string{"/widgets", probePath, "/openapi.json"} {
			wg.Go(func() {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequestWithContext(ctx, "GET", path, nil))
				body, _ := io.ReadAll(rec.Body)
				if rec.Code != http.StatusOK {
					mu.Lock()
					problems = append(problems, fmt.Errorf("GET %s with the module registered: status %d %s", path, rec.Code, body))
					mu.Unlock()
				}
			})
		}
	}
	wg.Wait()
	return dedupe(problems)
}

func stopWithin(ctx context.Context, what string, stop func(context.Context) error) []error {
	stopCtx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	start := time.Now()
	if err := stop(stopCtx); err != nil {
		return []error{fmt.Errorf("%s: %w (after %s)", what, err, time.Since(start).Round(time.Millisecond))}
	}
	return nil
}

func dedupe(errs []error) []error {
	seen := map[string]bool{}
	var out []error
	for _, e := range errs {
		if !seen[e.Error()] {
			seen[e.Error()] = true
			out = append(out, e)
		}
	}
	return out
}
