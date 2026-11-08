package bridgr

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/zaid223r/Bridgr/api"
	"github.com/zaid223r/Bridgr/core/brcontext"
	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brschema"
)

// NoContent as a handler's output type makes it respond 204 with no body.
type NoContent struct{}

// HandleOption customizes a typed handler.
type HandleOption func(*handleConfig)

type handleConfig struct {
	operationID string
	summary     string
	tags        []string
	status      int
	errors      []int
	middleware  []func(http.Handler) http.Handler
}

// WithOperationID sets the OpenAPI operationId, which client generators use as
// the function name. Default: method plus path, e.g. postTodosIdArchive.
func WithOperationID(id string) HandleOption { return func(c *handleConfig) { c.operationID = id } }

// WithSummary sets the OpenAPI summary.
func WithSummary(s string) HandleOption { return func(c *handleConfig) { c.summary = s } }

// WithTags groups the endpoint in the docs, e.g. under a CRUD resource's tag.
func WithTags(tags ...string) HandleOption { return func(c *handleConfig) { c.tags = tags } }

// WithStatus sets the success status. Default 200 (204 for NoContent).
func WithStatus(code int) HandleOption { return func(c *handleConfig) { c.status = code } }

// WithErrors documents error statuses the handler can return, such as 404 or
// 409, in addition to 400 and 500 (and 422 when the input validates itself).
func WithErrors(codes ...int) HandleOption {
	return func(c *handleConfig) { c.errors = append(c.errors, codes...) }
}

// WithMiddleware wraps this endpoint; the first is the outermost.
func WithMiddleware(mw ...func(http.Handler) http.Handler) HandleOption {
	return func(c *handleConfig) { c.middleware = append(c.middleware, mw...) }
}

// validator is implemented by inputs that check themselves; returning a
// brerrors.ValidationError gives clients field-level 422 errors.
type validator interface{ Validate() error }

// param is an In field filled from the path, query string or a header.
type param struct {
	index    int
	name     string
	in       string // "path", "query" or "header"
	required bool
	typ      reflect.Type
}

// Handle registers a typed endpoint: fn receives In, built from the request,
// and its Out is written as JSON. It is documented in OpenAPI automatically.
//
// In is a struct. Fields tagged path:"name", query:"name" or header:"Name" are
// parsed from the request (add ",required" to a query or header tag to make it
// mandatory; pointer fields are optional); the remaining JSON fields are the
// request body. Supported parameter types are strings, booleans, integers,
// floats, time.Time (RFC 3339) and pointers to them. If In has a Validate()
// error method, it runs before fn. Errors from fn are mapped like everywhere
// else in Bridgr (brerrors.ErrNotFound → 404, …).
//
// Handle panics, at startup, if a path wildcard has no matching field or the
// other way round, if a parameter type is unsupported, if a GET or HEAD
// endpoint has body fields, or if the app has already started.
func Handle[In, Out any](a *App, method, path string, fn func(ctx context.Context, in In) (Out, error), opts ...HandleOption) {
	where := method + " " + path
	a.mustNotBeStarted("bridgr.Handle " + where)
	cfg := handleConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	inT := reflect.TypeFor[In]()
	if inT.Kind() != reflect.Struct {
		panic(fmt.Sprintf("bridgr.Handle %s: input type %s must be a struct", where, inT))
	}
	params := parseParams(where, inT)
	checkWildcards(where, path, params)

	bodyFields := bodyFieldNames(inT, params)
	hasBody := len(bodyFields) > 0
	if hasBody && (method == http.MethodGet || method == http.MethodHead) {
		panic(fmt.Sprintf("bridgr.Handle %s: %s requests have no body; tag fields %v with query:\"...\"", where, method, bodyFields))
	}
	_, validates := any(new(In)).(validator)
	if !validates {
		_, validates = any(*new(In)).(validator)
	}

	outT := reflect.TypeFor[Out]()
	noContent := outT == reflect.TypeFor[NoContent]()
	status := cfg.status
	if status == 0 {
		status = http.StatusOK
		if noContent {
			status = http.StatusNoContent
		}
	}

	op := api.Operation{
		Method: method, Path: path, Summary: cfg.summary, Tags: cfg.tags, Status: status,
		OperationID: cmp.Or(cfg.operationID, defaultOperationID(method, path)),
	}
	for _, p := range params {
		op.Params = append(op.Params, api.Param{Name: p.name, In: p.in, Required: p.required, Type: p.typ})
		op.BodyExclude = append(op.BodyExclude, inT.Field(p.index).Name)
	}
	if hasBody {
		op.Body = inT
	}
	if !noContent {
		op.Response = outT
	}
	errs := []int{http.StatusInternalServerError}
	if hasBody || len(params) > 0 {
		errs = append(errs, http.StatusBadRequest)
	}
	if hasBody {
		errs = append(errs, http.StatusRequestEntityTooLarge)
	}
	if validates {
		errs = append(errs, http.StatusUnprocessableEntity)
	}
	errs = append(errs, cfg.errors...)
	slices.Sort(errs)
	op.Errors = slices.Compact(errs)
	a.Docs().AddOperation(op)

	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in In
		if hasBody {
			if err := decodeBody(r, &in); err != nil {
				writeProblem(w, r, err)
				return
			}
		}
		if err := fillParams(r, reflect.ValueOf(&in).Elem(), params); err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		if v, ok := any(&in).(validator); ok {
			if err := v.Validate(); err != nil {
				writeValidation(w, r, err)
				return
			}
		}
		out, err := fn(r.Context(), in)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		if noContent {
			w.WriteHeader(status)
			return
		}
		brcontext.JSON(w, status, out)
	})
	for _, mw := range slices.Backward(cfg.middleware) {
		h = mw(h)
	}
	a.router.AddRoute(method, path, h.ServeHTTP)
}

func parseParams(where string, t reflect.Type) []param {
	var params []param
	for i := range t.NumField() {
		f := t.Field(i)
		for _, in := range []string{"path", "query", "header"} {
			tag, ok := f.Tag.Lookup(in)
			if !ok {
				continue
			}
			name, opts, _ := strings.Cut(tag, ",")
			if name == "" {
				name = f.Name
			}
			if !f.IsExported() {
				panic(fmt.Sprintf("bridgr.Handle %s: field %s must be exported to be filled from the %s", where, f.Name, in))
			}
			if !supportedParam(f.Type) {
				panic(fmt.Sprintf("bridgr.Handle %s: field %s has unsupported parameter type %s", where, f.Name, f.Type))
			}
			params = append(params, param{index: i, name: name, in: in, required: in == "path" || opts == "required", typ: f.Type})
		}
	}
	return params
}

var timeType = reflect.TypeFor[time.Time]()

func supportedParam(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool, reflect.Float32, reflect.Float64,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	default:
		return false
	}
}

func checkWildcards(where, path string, params []param) {
	var wildcards []string
	for seg := range strings.SplitSeq(path, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			name := strings.TrimSuffix(seg[1:len(seg)-1], "...")
			if name != "$" {
				wildcards = append(wildcards, name)
			}
		}
	}
	for _, w := range wildcards {
		if !slices.ContainsFunc(params, func(p param) bool { return p.in == "path" && p.name == w }) {
			panic(fmt.Sprintf("bridgr.Handle %s: wildcard {%s} has no field tagged path:%q", where, w, w))
		}
	}
	for _, p := range params {
		if p.in == "path" && !slices.Contains(wildcards, p.name) {
			panic(fmt.Sprintf("bridgr.Handle %s: field tagged path:%q has no {%s} in the path", where, p.name, p.name))
		}
	}
}

// bodyFieldNames lists the JSON fields of t that are not parameters.
func bodyFieldNames(t reflect.Type, params []param) []string {
	model, err := brschema.For(t)
	if err != nil {
		return nil
	}
	var names []string
	for _, f := range model.Fields {
		isParam := len(f.Index) == 1 && slices.ContainsFunc(params, func(p param) bool { return p.index == f.Index[0] })
		if !isParam {
			names = append(names, f.JSONName)
		}
	}
	return names
}

// decodeBody reads a JSON object into in. An empty body counts as {}.
func decodeBody(r *http.Request, in any) error {
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return errors.New("body must be a JSON object")
	}
	return json.Unmarshal(raw, in)
}

// writeProblem writes a 400 for a malformed body, or 413 when it is too large.
func writeProblem(w http.ResponseWriter, r *http.Request, err error) {
	p := brerrors.New(http.StatusBadRequest, "invalid JSON body: "+err.Error())
	if tooBig, ok := errors.AsType[*http.MaxBytesError](err); ok {
		p = brerrors.New(http.StatusRequestEntityTooLarge, fmt.Sprintf("request body is larger than %d bytes", tooBig.Limit))
	}
	p.Instance = r.URL.Path
	p.RequestID = brcontext.RequestID(r.Context())
	brerrors.Write(w, p)
}

// writeValidation writes a Validate error as 422, with field errors when it is
// a brerrors.ValidationError.
func writeValidation(w http.ResponseWriter, r *http.Request, err error) {
	p := brerrors.FromError(r, err)
	if p.Status != http.StatusUnprocessableEntity {
		p = brerrors.New(http.StatusUnprocessableEntity, err.Error())
		p.Instance = r.URL.Path
		p.RequestID = brcontext.RequestID(r.Context())
	}
	brerrors.Write(w, p)
}

func fillParams(r *http.Request, in reflect.Value, params []param) error {
	for _, p := range params {
		var raw string
		var present bool
		switch p.in {
		case "path":
			raw = r.PathValue(p.name)
			present = true
		case "query":
			raw, present = r.URL.Query().Get(p.name), r.URL.Query().Has(p.name)
		case "header":
			raw = r.Header.Get(p.name)
			present = raw != ""
		}
		if !present {
			if p.required {
				return &brerrors.InvalidParamError{Param: p.name, Value: "", Reason: "is required"}
			}
			continue
		}
		v, err := parseParam(p.typ, raw)
		if err != nil {
			return &brerrors.InvalidParamError{Param: p.name, Value: raw, Reason: err.Error()}
		}
		in.Field(p.index).Set(v)
	}
	return nil
}

func parseParam(t reflect.Type, raw string) (reflect.Value, error) {
	if t.Kind() == reflect.Pointer {
		v, err := parseParam(t.Elem(), raw)
		if err != nil {
			return v, err
		}
		ptr := reflect.New(t.Elem())
		ptr.Elem().Set(v)
		return ptr, nil
	}
	v := reflect.New(t).Elem()
	if t == timeType {
		ts, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return v, errors.New("want an RFC 3339 time")
		}
		v.Set(reflect.ValueOf(ts))
		return v, nil
	}
	switch t.Kind() {
	case reflect.String:
		v.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return v, errors.New("want true or false")
		}
		v.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(raw, 10, t.Bits())
		if err != nil {
			return v, errors.New("want an integer")
		}
		v.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(raw, 10, t.Bits())
		if err != nil {
			return v, errors.New("want a non-negative integer")
		}
		v.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(raw, t.Bits())
		if err != nil {
			return v, errors.New("want a number")
		}
		v.SetFloat(f)
	}
	return v, nil
}

// defaultOperationID turns "POST /todos/{id}/archive" into postTodosIdArchive.
func defaultOperationID(method, path string) string {
	var b strings.Builder
	b.WriteString(strings.ToLower(method))
	upper := true
	for _, r := range path {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			upper = true
			continue
		}
		if upper {
			r = unicode.ToUpper(r)
			upper = false
		}
		b.WriteRune(r)
	}
	return b.String()
}
