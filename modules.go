package bridgr

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/zaid223r/Bridgr/api"
)

// Module is an add-on that plugs into an App, such as auth, scheduled jobs or
// rate limiting. Modules live in their own repositories and talk to the core
// only through its public API.
//
// Register is called once, from App.Use, before the app starts. It may add
// routes (app.Router()), OpenAPI additions (app.Docs()) and, through the core's
// capability interfaces, things other modules can use. A module that runs in
// the background also implements Starter and Stopper.
type Module interface {
	// Name identifies the module in errors and logs. It must be unique within an
	// app and contain no spaces, e.g. "auth" or "jobs".
	Name() string
	Register(app *App) error
}

// Starter is implemented by modules with background work. Start is called by
// Run and Serve before the server accepts requests, in the order modules were
// added. ctx is cancelled when the app shuts down.
type Starter interface {
	Start(ctx context.Context) error
}

// Stopper is implemented by modules that must release resources. Stop is called
// after the server has stopped, in reverse order, with a context bounded by
// Config.ShutdownTimeout. It must be safe to call without a prior Start.
type Stopper interface {
	Stop(ctx context.Context) error
}

// defaultShutdownTimeout mirrors brhttp's default for Config.ShutdownTimeout.
const defaultShutdownTimeout = 15 * time.Second

// Use registers modules in order. It returns an error for a nil module, an empty
// or duplicate name, a module added after the app started, or a Register
// failure; after an error the app may be partly configured and should not be
// started.
func (a *App) Use(mods ...Module) error {
	for _, m := range mods {
		if m == nil {
			return errors.New("bridgr: Use: nil module")
		}
		name := m.Name()
		if name == "" || strings.ContainsAny(name, " \t\n") {
			return fmt.Errorf("bridgr: Use: invalid module name %q", name)
		}
		a.mu.Lock()
		started := a.started
		dup := slices.ContainsFunc(a.modules, func(o Module) bool { return o.Name() == name })
		a.mu.Unlock()
		if started {
			return fmt.Errorf("bridgr: Use(%s): the app has already started", name)
		}
		if dup {
			return fmt.Errorf("bridgr: Use(%s): a module with this name is already registered", name)
		}
		if err := m.Register(a); err != nil {
			return fmt.Errorf("bridgr: registering module %s: %w", name, err)
		}
		a.mu.Lock()
		a.modules = append(a.modules, m)
		a.mu.Unlock()
	}
	return nil
}

// Modules returns the registered modules in registration order.
func (a *App) Modules() []Module {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.modules)
}

// Docs returns the OpenAPI registry, for modules that document routes they add
// or extend the document (see api.Registry.Transform).
func (a *App) Docs() *api.Registry {
	return a.router.Docs()
}

// StartModules starts every module that implements Starter, in order. If one
// fails, the ones already started are stopped in reverse order and the error
// is returned. Run and Serve call it; call it yourself only when serving
// Handler() with your own server.
func (a *App) StartModules(ctx context.Context) error {
	mods := a.Modules()
	for i, m := range mods {
		s, ok := m.(Starter)
		if !ok {
			continue
		}
		if err := s.Start(ctx); err != nil {
			startErr := fmt.Errorf("bridgr: starting module %s: %w", m.Name(), err)
			return errors.Join(startErr, a.stop(ctx, mods[:i]))
		}
	}
	return nil
}

// StopModules stops every module that implements Stopper, in reverse order,
// and returns all their errors joined.
func (a *App) StopModules(ctx context.Context) error {
	return a.stop(ctx, a.Modules())
}

func (a *App) stop(ctx context.Context, mods []Module) error {
	var errs []error
	for _, m := range slices.Backward(mods) {
		if s, ok := m.(Stopper); ok {
			if err := s.Stop(ctx); err != nil {
				errs = append(errs, fmt.Errorf("bridgr: stopping module %s: %w", m.Name(), err))
			}
		}
	}
	return errors.Join(errs...)
}

// runWithModules starts modules, runs serve until it returns, then stops the
// modules with a fresh deadline (ctx is usually cancelled by then).
func (a *App) runWithModules(ctx context.Context, serve func(context.Context) error) error {
	moduleCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := a.StartModules(moduleCtx); err != nil {
		return err
	}
	serveErr := serve(ctx)
	cancel()

	stopCtx, stopCancel := context.WithTimeout(context.WithoutCancel(ctx), cmp.Or(a.cfg.ShutdownTimeout, defaultShutdownTimeout))
	defer stopCancel()
	return errors.Join(serveErr, a.StopModules(stopCtx))
}
