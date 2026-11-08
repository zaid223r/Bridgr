package brhttp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/zaid223r/Bridgr/api"
	"github.com/zaid223r/Bridgr/core/brcontext"
)

// Config configures a Server. Zero values use the defaults listed per field.
type Config struct {
	// Addr is the TCP address to listen on. Default ":8080".
	Addr string
	// ReadHeaderTimeout bounds reading request headers. Default 5s.
	ReadHeaderTimeout time.Duration
	// ReadTimeout bounds reading the whole request. Default 30s.
	ReadTimeout time.Duration
	// WriteTimeout bounds writing the response. Default 30s.
	WriteTimeout time.Duration
	// IdleTimeout bounds keep-alive connections waiting for the next request. Default 120s.
	IdleTimeout time.Duration
	// ShutdownTimeout is how long Run waits for in-flight requests after its
	// context is cancelled. Default 15s.
	ShutdownTimeout time.Duration
	// MaxBodyBytes caps request bodies; larger bodies get 413. Default 1 MiB.
	// Negative means no limit.
	MaxBodyBytes int64
	// Logger receives request and error logs. Default slog.Default().
	Logger *slog.Logger
	// DisableDocs skips registering GET /openapi.json and GET /docs.
	DisableDocs bool
	// Title, Version and Description fill the OpenAPI info object. Defaults
	// "Bridgr API" and "1.0.0".
	Title       string
	Version     string
	Description string
}

func (c Config) withDefaults() Config {
	setDefault(&c.Addr, ":8080")
	setDefault(&c.ReadHeaderTimeout, 5*time.Second)
	setDefault(&c.ReadTimeout, 30*time.Second)
	setDefault(&c.WriteTimeout, 30*time.Second)
	setDefault(&c.IdleTimeout, 120*time.Second)
	setDefault(&c.ShutdownTimeout, 15*time.Second)
	setDefault(&c.MaxBodyBytes, 1<<20)
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

func setDefault[T comparable](v *T, def T) {
	var zero T
	if *v == zero {
		*v = def
	}
}

// Server runs a Router with production defaults: timeouts, a request body limit,
// structured request logging and graceful shutdown.
type Server struct {
	cfg     Config
	handler http.Handler
	srv     *http.Server
}

// NewServer wraps router in a Server. Unless cfg.DisableDocs is set it registers
// GET /openapi.json and GET /docs on router, so call it once per router, after
// registering resources: the document is built on the first request and cached.
func NewServer(router *Router, cfg Config) *Server {
	cfg = cfg.withDefaults()
	if !cfg.DisableDocs {
		info := api.Info{Title: cfg.Title, Version: cfg.Version, Description: cfg.Description}
		router.AddRoute("GET", "/openapi.json", serveSpec(sync.OnceValues(func() ([]byte, error) {
			return router.Docs().Build(info)
		})))
		router.AddRoute("GET", "/docs", api.ServeSwaggerUI())
	}
	handler := withRequestContext(cfg.Logger, cfg.MaxBodyBytes, router)
	return &Server{
		cfg:     cfg,
		handler: handler,
		srv: &http.Server{
			Addr:              cfg.Addr,
			Handler:           handler,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			ErrorLog:          slog.NewLogLogger(cfg.Logger.Handler(), slog.LevelWarn),
		},
	}
}

// Handler returns the full handler chain (logging, body limit, routes), for use
// with httptest or another server.
func (s *Server) Handler() http.Handler {
	return s.handler
}

// ListenAndServe runs the server until SIGINT or SIGTERM, then shuts down
// gracefully. It returns nil after a clean shutdown.
func (s *Server) ListenAndServe() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return s.Run(ctx)
}

// Run listens on cfg.Addr and serves until ctx is cancelled, then stops accepting
// connections and waits up to ShutdownTimeout for in-flight requests.
func (s *Server) Run(ctx context.Context) error {
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", s.cfg.Addr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", s.cfg.Addr, err)
	}
	return s.Serve(ctx, ln)
}

// Serve is Run on an existing listener. It takes ownership of ln.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	s.cfg.Logger.InfoContext(ctx, "bridgr listening", "addr", ln.Addr().String())

	errCh := make(chan error, 1)
	go func() { errCh <- s.srv.Serve(ln) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	s.cfg.Logger.InfoContext(ctx, "bridgr shutting down", "timeout", s.cfg.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.ShutdownTimeout)
	defer cancel()
	shutdownErr := s.srv.Shutdown(shutdownCtx)
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	if shutdownErr != nil {
		return fmt.Errorf("graceful shutdown: %w", shutdownErr)
	}
	return nil
}

func serveSpec(build func() ([]byte, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		spec, err := build()
		if err != nil {
			brcontext.Logger(r.Context()).ErrorContext(r.Context(), "building OpenAPI spec", "error", err)
			http.Error(w, "could not build the OpenAPI document", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(spec)
	}
}

// withRequestContext puts logger in the request context, limits the body to
// maxBody bytes (when positive) and logs one line per request.
func withRequestContext(logger *slog.Logger, maxBody int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		if maxBody > 0 {
			r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		}
		r = r.WithContext(brcontext.WithLogger(r.Context(), logger))
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		if rec.status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		logger.LogAttrs(r.Context(), level, "request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int64("bytes", rec.bytes),
			slog.Duration("duration", time.Since(start)),
		)
	})
}

// statusRecorder captures the status code and body size for the request log.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wroteHeader = true
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer (Flush,
// deadlines, hijacking).
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
