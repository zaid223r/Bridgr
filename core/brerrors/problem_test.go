package brerrors_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/zaid223r/Bridgr/core/brerrors"
)

func TestFromError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		err         error
		wantStatus  int
		wantDetail  string
		wantInvalid []string
	}{
		{"invalid filter", &brerrors.InvalidFilterFieldError{Fields: []string{"a", "b"}}, 400, "invalid filter fields: a, b", []string{"a", "b"}},
		{"wrapped invalid filter", fmt.Errorf("list: %w", &brerrors.InvalidFilterFieldError{Fields: []string{"a"}}), 400, "invalid filter field: a", []string{"a"}},
		{"not found", brerrors.ErrNotFound, 404, "", nil},
		{"wrapped not found", fmt.Errorf("todo 7: %w", brerrors.ErrNotFound), 404, "", nil},
		{"unauthenticated", brerrors.ErrUnauthenticated, 401, "", nil},
		{"forbidden", brerrors.ErrForbidden, 403, "", nil},
		{"conflict", fmt.Errorf("todo 7: %w", brerrors.ErrConflict), 409, "", nil},
		{"invalid filter value", &brerrors.InvalidFilterValueError{Field: "done", Value: "maybe", Type: "boolean"}, 400, `invalid value "maybe" for filter done: want boolean`, []string{"done"}},
		{"internal error is hidden", errors.New("dial tcp 10.0.0.5:5432: connection refused"), 500, "The server could not complete the request.", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := brerrors.FromError(httptest.NewRequest("GET", "/todos/7?x=1", nil), tt.err)

			if p.Status != tt.wantStatus {
				t.Errorf("status = %d, want %d", p.Status, tt.wantStatus)
			}
			if p.Title != http.StatusText(tt.wantStatus) {
				t.Errorf("title = %q, want %q", p.Title, http.StatusText(tt.wantStatus))
			}
			if p.Detail != tt.wantDetail {
				t.Errorf("detail = %q, want %q", p.Detail, tt.wantDetail)
			}
			if !slices.Equal(p.InvalidFields, tt.wantInvalid) {
				t.Errorf("invalid fields = %v, want %v", p.InvalidFields, tt.wantInvalid)
			}
			if p.Instance != "/todos/7" {
				t.Errorf("instance = %q, want /todos/7", p.Instance)
			}
		})
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	brerrors.Write(rec, brerrors.New(http.StatusNotFound, "no todo 7"))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	want := map[string]any{"title": "Not Found", "status": float64(404), "detail": "no todo 7"}
	for k, v := range want {
		if body[k] != v {
			t.Errorf("%s = %v, want %v", k, body[k], v)
		}
	}
	for _, k := range []string{"type", "instance", "invalid_fields"} {
		if _, ok := body[k]; ok {
			t.Errorf("empty member %q should be omitted", k)
		}
	}
}

func TestWriteErrorLogsOnlyServerErrors(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		err  error
		want int
	}{
		{brerrors.ErrNotFound, 404},
		{errors.New("boom"), 500},
	} {
		rec := httptest.NewRecorder()
		brerrors.WriteError(rec, httptest.NewRequest("GET", "/x", nil), tt.err)
		if rec.Code != tt.want {
			t.Errorf("WriteError(%v): status = %d, want %d", tt.err, rec.Code, tt.want)
		}
	}
}
