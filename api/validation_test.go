package api_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

type Owner struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

type Product struct {
	ID       int64             `json:"id"`
	Name     string            `json:"name"`
	Qty      int               `json:"qty"`
	Active   bool              `json:"active"`
	Price    float64           `json:"price"`
	Tags     []string          `json:"tags"`
	Attrs    map[string]string `json:"attrs"`
	Released *time.Time        `json:"released,omitempty"`
	Owner    Owner             `json:"owner"`
	Backup   *Owner            `json:"backup"`
}

func (Product) FilterableFields() []string { return []string{"name", "qty", "active", "released"} }

type Sku struct {
	Code string `json:"code" db:",pk"`
	Qty  uint16 `json:"qty"`
}

// testServer runs a real server with two resources, auth and validation, and
// returns it with its OpenAPI document.
func testServer(t *testing.T) (*httptest.Server, []byte) {
	t.Helper()
	products, err := brstore.NewMemory[Product]()
	if err != nil {
		t.Fatal(err)
	}
	skus, err := brstore.NewMemory[Sku]()
	if err != nil {
		t.Fatal(err)
	}
	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[Product](router, "products", products, &brcrud.Options[Product]{
		Auth: func(r *http.Request) error {
			switch r.Header.Get("Authorization") {
			case "Bearer admin":
				return nil
			case "Bearer guest":
				return brerrors.ErrForbidden
			default:
				return brerrors.ErrUnauthenticated
			}
		},
		Validate: func(p Product) error {
			if p.Name == "" {
				return errors.New("name is required")
			}
			return nil
		},
		DefaultLimit: 10,
		MaxLimit:     50,
	})
	brcrud.RegisterCRUD[Sku](router, "skus", skus, nil)

	srv := httptest.NewServer(brhttp.NewServer(router, brhttp.Config{
		Title:        "Validation test",
		MaxBodyBytes: 512,
		Logger:       slog.New(slog.DiscardHandler),
	}).Handler())
	t.Cleanup(srv.Close)

	resp, err := srv.Client().Get(srv.URL + "/openapi.json") //nolint:noctx // test helper
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	spec, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return srv, spec
}

func newValidator(t *testing.T, spec []byte) validator.Validator {
	t.Helper()
	doc, err := libopenapi.NewDocument(spec)
	if err != nil {
		t.Fatalf("parsing the document: %v", err)
	}
	v, errs := validator.NewValidator(doc)
	if len(errs) > 0 {
		t.Fatalf("building the validator: %v", errs)
	}
	return v
}

func TestDocumentIsValidOpenAPI31(t *testing.T) {
	t.Parallel()
	_, spec := testServer(t)
	v := newValidator(t, spec)
	if ok, errs := v.ValidateDocument(); !ok {
		for _, e := range errs {
			t.Errorf("%s: %s", e.Message, e.Reason)
		}
	}
}

type exchange struct {
	name       string
	method     string
	path       string
	body       string
	auth       string
	wantStatus int
	// validRequest marks requests that should themselves match the spec; the
	// others are deliberately malformed, so only their response is checked.
	validRequest bool
}

// TestResponsesMatchSpec sends real requests and checks every response, and
// every well-formed request, against the generated document.
func TestResponsesMatchSpec(t *testing.T) {
	t.Parallel()
	srv, spec := testServer(t)
	v := newValidator(t, spec)

	big := `{"name":"` + strings.Repeat("x", 600) + `"}`
	steps := []exchange{
		{"create product", "POST", "/products", `{"name":"Bolt","qty":3,"active":true,"price":1.5,"tags":["a"],"attrs":{"k":"v"},"owner":{"name":"Zaid"}}`, "admin", 201, true},
		{"create second product", "POST", "/products", `{"name":"Nut","released":"2026-01-02T03:04:05Z","backup":{"name":"B","email":"b@x.io"}}`, "admin", 201, true},
		{"list", "GET", "/products", "", "admin", 200, true},
		{"list with paging and sort", "GET", "/products?limit=1&offset=1&sort=-qty", "", "admin", 200, true},
		{"list with filters", "GET", "/products?name=bolt&active=true", "", "admin", 200, true},
		{"list with an operator", "GET", "/products?qty%5Bgte%5D=1", "", "admin", 200, false},
		{"get", "GET", "/products/1", "", "admin", 200, true},
		{"replace", "PUT", "/products/1", `{"name":"Bolt M6","qty":0}`, "admin", 200, true},
		{"update", "PATCH", "/products/2", `{"qty":9}`, "admin", 200, true},
		{"bad filter value", "GET", "/products?active=maybe", "", "admin", 400, false},
		{"limit above max", "GET", "/products?limit=500", "", "admin", 400, false},
		{"malformed body", "POST", "/products", `{"name":`, "admin", 400, false},
		{"validation failure", "POST", "/products", `{"qty":1}`, "admin", 422, true},
		{"body too large", "POST", "/products", big, "admin", 413, true},
		{"missing record", "GET", "/products/999", "", "admin", 404, true},
		{"unauthenticated", "GET", "/products", "", "", 401, true},
		{"forbidden", "DELETE", "/products/1", "", "guest", 403, true},
		{"delete", "DELETE", "/products/1", "", "admin", 204, true},
		{"create sku", "POST", "/skus", `{"code":"A-1","qty":2}`, "", 201, true},
		{"duplicate sku", "POST", "/skus", `{"code":"A-1","qty":2}`, "", 409, true},
		{"get sku", "GET", "/skus/A-1", "", "", 200, true},
		{"list skus", "GET", "/skus", "", "", 200, true},
	}
	// Steps share the server's state, so they run in order.
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			req, resp := send(t, srv, step)
			defer resp.Body.Close()
			if resp.StatusCode != step.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, step.wantStatus)
			}
			var ok bool
			var errs []*validationError
			if step.validRequest {
				ok, errs = wrap(v.ValidateHttpRequestResponse(req, resp))
			} else {
				ok, errs = wrap(v.ValidateHttpResponse(req, resp))
			}
			if !ok {
				for _, e := range errs {
					t.Errorf("%s", e)
				}
			}
		})
	}
}

type validationError struct{ msg string }

func (e *validationError) String() string { return e.msg }

func wrap[E interface {
	Error() string
}](ok bool, errs []E) (bool, []*validationError) {
	out := make([]*validationError, len(errs))
	for i, e := range errs {
		out[i] = &validationError{msg: e.Error()}
	}
	return ok, out
}

// send performs the request and returns a fresh copy of it (the original's body
// was consumed) together with a response whose body can be read again.
func send(t *testing.T, srv *httptest.Server, step exchange) (*http.Request, *http.Response) {
	t.Helper()
	build := func() *http.Request {
		var body io.Reader
		if step.body != "" {
			body = strings.NewReader(step.body)
		}
		req, err := http.NewRequestWithContext(t.Context(), step.method, srv.URL+step.path, body)
		if err != nil {
			t.Fatal(err)
		}
		if step.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if step.auth != "" {
			req.Header.Set("Authorization", "Bearer "+step.auth)
		}
		return req
	}
	resp, err := srv.Client().Do(build())
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(payload))
	t.Cleanup(func() {
		if t.Failed() {
			fmt.Printf("%s %s -> %d %s\n", step.method, step.path, resp.StatusCode, payload)
		}
	})
	return build(), resp
}
