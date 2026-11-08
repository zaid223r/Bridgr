// Package brcontext holds request-scoped helpers: writing JSON responses and
// carrying the server's logger and the request ID in the request context.
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

type (
	loggerKey    struct{}
	requestIDKey struct{}
)

// WithRequestID returns a copy of ctx carrying the request ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID returns the ID the server assigned to the request, or "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

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
