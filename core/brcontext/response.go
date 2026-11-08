// Package brcontext holds request-scoped helpers: writing JSON responses and
// carrying the server's logger in the request context.
package brcontext

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
)

// JSON writes data as a JSON body with the given status code.
// Encoding errors are dropped because the status line has already been sent.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

type loggerKey struct{}

// WithLogger returns a copy of ctx that carries logger.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// Logger returns the logger stored by WithLogger, or slog.Default() if none.
func Logger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
