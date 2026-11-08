// Package bridgr is the bridge from a Go struct to a production API.
//
// An App turns structs into documented REST endpoints:
//
//	app := bridgr.New(bridgr.Config{Addr: ":8080", Title: "Todo API"})
//	bridgr.CRUD[Todo](app, "todos", store, nil)
//	if err := app.ListenAndServe(); err != nil {
//		log.Fatal(err)
//	}
//
// The packages under core/ (brcrud, brhttp, brstore, ...) remain available for
// finer control; App wires them together.
package bridgr

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/zaid223r/Bridgr/api"
	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// Config configures an App: server timeouts, body limit, logging, and the
// OpenAPI title, version and docs page. See brhttp.Config for every field.
type Config = brhttp.Config

// CRUDOptions customizes one resource: auth, validation, middleware, page sizes.
type CRUDOptions[T any] = brcrud.Options[T]

// App holds the routes of an API and runs it. Register everything before the
// first call to Handler, Run or ListenAndServe; registering afterwards panics,
// because the OpenAPI document and route table are fixed from then on.
type App struct {
	cfg    Config
	router *brhttp.Router

	mu      sync.Mutex
	started bool
	server  *brhttp.Server
}

// New returns an App with no routes.
func New(cfg Config) *App {
	return &App{cfg: cfg, router: brhttp.NewRouter()}
}

// Router returns the underlying router, for routes and documentation the App
// doesn't cover yet. It follows the same register-before-start rule.
func (a *App) Router() *brhttp.Router {
	return a.router
}

// CRUD mounts list, create, get, replace, update and delete endpoints for T
// under /path, backed by store, and documents them. It panics on an invalid
// model or when the app has already started; see brcrud.RegisterCRUD.
func CRUD[T any](a *App, path string, store brstore.Store[T], opts *CRUDOptions[T]) {
	a.mustNotBeStarted("bridgr.CRUD /" + path)
	brcrud.RegisterCRUD(a.router, path, store, opts)
}

func (a *App) mustNotBeStarted(what string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.started {
		panic(what + " called after the app started; register everything before Handler, Run or ListenAndServe")
	}
}

// start freezes registration and builds the server once.
func (a *App) start() *brhttp.Server {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.started {
		a.started = true
		a.server = brhttp.NewServer(a.router, a.cfg)
	}
	return a.server
}

// OpenAPI returns the app's OpenAPI 3.1 document, with the title, version and
// description from Config. It doesn't start the app, so it suits code
// generation (e.g. a -spec flag that writes the document and exits).
func (a *App) OpenAPI() ([]byte, error) {
	return a.router.Docs().Build(api.Info{Title: a.cfg.Title, Version: a.cfg.Version, Description: a.cfg.Description})
}

// Handler returns the complete HTTP handler (logging, body limit, routes,
// OpenAPI and docs), for tests or for mounting under another server.
func (a *App) Handler() http.Handler {
	return a.start().Handler()
}

// Run serves on Config.Addr until ctx is cancelled, then shuts down gracefully.
func (a *App) Run(ctx context.Context) error {
	return a.start().Run(ctx)
}

// Serve is Run on an existing listener, which it takes ownership of.
func (a *App) Serve(ctx context.Context, ln net.Listener) error {
	return a.start().Serve(ctx, ln)
}

// ListenAndServe runs the app until SIGINT or SIGTERM and returns nil after a
// clean shutdown.
func (a *App) ListenAndServe() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.Run(ctx)
}
