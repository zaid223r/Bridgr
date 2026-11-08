// Package brerrors defines the errors Bridgr understands and writes them to clients
// as RFC 9457 problem details (application/problem+json).
package brerrors

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zaid223r/Bridgr/core/brcontext"
)

// ContentType is the media type of problem detail responses.
const ContentType = "application/problem+json"

// Sentinel errors that storage and auth code return, possibly wrapped, to choose
// the response status. Check for them with errors.Is.
var (
	// ErrNotFound means the requested record does not exist (404).
	ErrNotFound = errors.New("not found")
	// ErrUnauthenticated means the request has no valid credentials (401).
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrForbidden means the caller is known but not allowed to do this (403).
	ErrForbidden = errors.New("forbidden")
	// ErrConflict means the write clashes with existing data, e.g. a duplicate key (409).
	ErrConflict = errors.New("conflict")
)

// Problem is an RFC 9457 problem details object. Type is omitted when empty,
// which clients must read as "about:blank".
type Problem struct {
	Type     string `json:"type,omitempty"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`

	// InvalidFields is an extension member listing rejected filter fields.
	InvalidFields []string `json:"invalid_fields,omitempty"`
}

// New returns a Problem for status with the standard status text as its title.
func New(status int, detail string) Problem {
	return Problem{Title: http.StatusText(status), Status: status, Detail: detail}
}

// Write sends p as the response. Encoding errors are dropped because the status
// line has already been sent.
func Write(w http.ResponseWriter, p Problem) {
	w.Header().Set("Content-Type", ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// FromError maps err to a Problem:
//
//   - *InvalidFilterFieldError, *InvalidFilterValueError, *InvalidFilterOperatorError
//     → 400 with the fields in invalid_fields
//   - ErrNotFound → 404
//   - ErrUnauthenticated → 401
//   - ErrForbidden → 403
//   - ErrConflict → 409
//   - anything else → 500 with a generic detail, so internal messages
//     (SQL, driver errors) never reach clients
//
// Instance is set to the request path.
func FromError(r *http.Request, err error) Problem {
	var p Problem
	if fieldErr, ok := errors.AsType[*InvalidFilterFieldError](err); ok {
		p = New(http.StatusBadRequest, fieldErr.Error())
		p.InvalidFields = fieldErr.Fields
	} else if valueErr, ok := errors.AsType[*InvalidFilterValueError](err); ok {
		p = New(http.StatusBadRequest, valueErr.Error())
		p.InvalidFields = []string{valueErr.Field}
	} else if opErr, ok := errors.AsType[*InvalidFilterOperatorError](err); ok {
		p = New(http.StatusBadRequest, opErr.Error())
		p.InvalidFields = []string{opErr.Field}
	} else {
		switch {
		case errors.Is(err, ErrNotFound):
			p = New(http.StatusNotFound, "")
		case errors.Is(err, ErrUnauthenticated):
			p = New(http.StatusUnauthorized, "")
		case errors.Is(err, ErrForbidden):
			p = New(http.StatusForbidden, "")
		case errors.Is(err, ErrConflict):
			p = New(http.StatusConflict, "")
		default:
			p = New(http.StatusInternalServerError, "The server could not complete the request.")
		}
	}
	p.Instance = r.URL.Path
	return p
}

// WriteError writes the Problem for err (see FromError). 5xx errors are logged with
// the original error, since the client only sees a generic message, using the
// logger in the request context (brcontext.Logger).
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	p := FromError(r, err)
	if p.Status >= http.StatusInternalServerError {
		brcontext.Logger(r.Context()).ErrorContext(r.Context(), "request failed",
			"method", r.Method, "path", r.URL.Path, "status", p.Status, "error", err)
	}
	Write(w, p)
}
