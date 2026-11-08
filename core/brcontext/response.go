// Package brcontext holds helpers for writing HTTP responses.
package brcontext

import (
	"encoding/json"
	"net/http"
)

// JSON writes data as a JSON body with the given status code.
// Encoding errors are dropped because the status line has already been sent.
func JSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
