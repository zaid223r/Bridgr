// Package brcrud registers REST CRUD routes for a model type backed by a
// brstore.Store.
package brcrud

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/zaid223r/Bridgr/api"
	"github.com/zaid223r/Bridgr/core/brcap"
	"github.com/zaid223r/Bridgr/core/brcontext"
	"github.com/zaid223r/Bridgr/core/brerrors"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brschema"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// Options customizes the CRUD routes for one resource: middleware, input
// validation and an authorization check run before every handler.
//
// Auth returns nil to allow the request. Returning an error wrapping
// brerrors.ErrUnauthenticated responds 401 (set a WWW-Authenticate header in Auth
// or a middleware); any other error responds 403.
//
// A Validate error is sent to the client as 422 with the error text as detail.
type Options[T any] struct {
	// Store holds the records. bridgr.CRUD uses it instead of the app's
	// database; RegisterCRUD uses it when its store argument is nil.
	Store brstore.Store[T]

	// Middleware wraps this resource's routes; the first is the outermost.
	// App-wide middleware (Config.Middleware) runs before it.
	Middleware []func(http.Handler) http.Handler
	Validate   func(input T) error
	Auth       func(r *http.Request) error

	// DefaultLimit is the page size when a list request has no limit. Default 50.
	DefaultLimit int
	// MaxLimit is the largest limit a client may ask for. Default 200.
	MaxLimit int

	// Hooks run business rules around writes.
	Hooks Hooks[T]
	// Events, if set, receives a brcap.Event after every successful create,
	// replace, update and delete. bridgr.CRUD sets it to the app's event bus.
	Events brcap.EventBus
}

// Hooks run around the writes of one resource. Before hooks run after the body
// is decoded and before Validate, so they can fill in fields (timestamps,
// defaults, the owner from brcap.PrincipalFrom); an error rejects the request
// and is mapped like any other (brerrors.ValidationError → 422, ErrForbidden →
// 403, ErrConflict → 409, anything else → 500). After hooks run once the write
// has succeeded and can't fail it. Update hooks run for both PUT and PATCH.
type Hooks[T any] struct {
	BeforeCreate func(ctx context.Context, v *T) error
	AfterCreate  func(ctx context.Context, v T)
	BeforeUpdate func(ctx context.Context, id string, v *T) error
	AfterUpdate  func(ctx context.Context, v T)
	BeforeDelete func(ctx context.Context, id string) error
	AfterDelete  func(ctx context.Context, id string)
}

// RegisterCRUD mounts these routes for T under /path, backed by store (or
// opts.Store when store is nil), and documents them in the OpenAPI spec:
//
//	GET    /path       list (see parseQuery for filters, limit, offset and sort)
//	POST   /path       create
//	GET    /path/{id}  get
//	PUT    /path/{id}  replace: fields missing from the body are reset to zero
//	PATCH  /path/{id}  merge: only fields present in the body change
//	DELETE /path/{id}  delete
//
// PATCH reads the record, applies the body and writes it back, so two PATCHes to
// the same record at the same moment can overwrite each other's changes.
//
// A nil opts uses the defaults.
//
// Errors are written as RFC 9457 problem details; see brerrors.FromError for the
// status mapping.
//
// It panics if T is not a valid model (e.g. FilterableFields names a field T
// doesn't have) or has no primary key, so mistakes stop the program at startup.
func RegisterCRUD[T any](router *brhttp.Router, path string, store brstore.Store[T], opts *Options[T]) {
	model := mustModel[T](path)
	if model.PrimaryKey == nil {
		panic(fmt.Sprintf(`bridgr: registering /%s: %s has no primary key (tag a field db:",pk" or name it ID)`, path, model.Name))
	}
	if opts == nil {
		opts = &Options[T]{}
	}
	if store == nil {
		store = opts.Store
	}
	if store == nil {
		panic(fmt.Sprintf("bridgr: registering /%s: no store", path))
	}
	limits := pageLimits{def: cmp.Or(opts.DefaultLimit, 50), max: cmp.Or(opts.MaxLimit, 200)}
	if limits.def > limits.max {
		panic(fmt.Sprintf("bridgr: registering /%s: DefaultLimit %d is above MaxLimit %d", path, limits.def, limits.max))
	}
	for _, f := range model.Filterable {
		if reservedParams[f.JSONName] {
			panic(fmt.Sprintf("bridgr: registering /%s: field %q can't be filterable; limit, offset and sort are reserved", path, f.JSONName))
		}
	}

	base := "/" + path

	router.Docs().AddCRUD(path, model, api.CRUDDocs{
		Auth:         opts.Auth != nil,
		Validate:     opts.Validate != nil,
		DefaultLimit: limits.def,
		MaxLimit:     limits.max,
	})

	handle := func(method, pattern string, handler http.HandlerFunc) {
		var h http.Handler = withAuth(opts.Auth, handler)
		for _, mw := range slices.Backward(opts.Middleware) {
			h = mw(h)
		}
		router.AddRoute(method, pattern, h.ServeHTTP)
	}

	handle("GET", base, func(w http.ResponseWriter, r *http.Request) {
		q, err := parseQuery(model, r.URL.Query(), limits)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		items, err := store.List(r.Context(), q)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		total, err := store.Count(r.Context(), q)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		w.Header().Set("X-Total-Count", strconv.FormatInt(total, 10))
		w.Header().Set("Link", pageLinks(r.URL, q.Limit, q.Offset, total))
		brcontext.JSON(w, http.StatusOK, items)
	})

	emit := func(r *http.Request, action, id string, data any) {
		if opts.Events == nil {
			return
		}
		opts.Events.Publish(r.Context(), brcap.Event{
			Resource: path, Action: action, ID: id, Data: data,
			At: time.Now(), RequestID: brcontext.RequestID(r.Context()),
		})
	}
	keyOf := func(v T) string {
		return fmt.Sprint(reflect.ValueOf(v).FieldByIndex(model.PrimaryKey.Index).Interface())
	}
	updateHook := func(id string) func(context.Context, *T) error {
		if opts.Hooks.BeforeUpdate == nil {
			return nil
		}
		return func(ctx context.Context, v *T) error { return opts.Hooks.BeforeUpdate(ctx, id, v) }
	}
	afterUpdate := func(r *http.Request, id string, v T) {
		if opts.Hooks.AfterUpdate != nil {
			opts.Hooks.AfterUpdate(r.Context(), v)
		}
		emit(r, brcap.Updated, id, v)
	}

	handle("POST", base, func(w http.ResponseWriter, r *http.Request) {
		var zero T
		input, ok := decodeInto(w, r, zero, opts.Hooks.BeforeCreate, opts.Validate)
		if !ok {
			return
		}
		created, err := store.Create(r.Context(), input)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		if opts.Hooks.AfterCreate != nil {
			opts.Hooks.AfterCreate(r.Context(), created)
		}
		emit(r, brcap.Created, keyOf(created), created)
		brcontext.JSON(w, http.StatusCreated, created)
	})

	handle("GET", base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		item, err := store.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		brcontext.JSON(w, http.StatusOK, item)
	})

	handle("PUT", base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var zero T
		input, ok := decodeInto(w, r, zero, updateHook(id), opts.Validate)
		if !ok {
			return
		}
		updated, err := store.Update(r.Context(), id, input)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		afterUpdate(r, id, updated)
		brcontext.JSON(w, http.StatusOK, updated)
	})

	handle("PATCH", base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		current, err := store.Get(r.Context(), id)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		// Decoding into the stored value leaves fields absent from the body as
		// they are. A body id is ignored: Store.Update takes the key from id.
		merged, ok := decodeInto(w, r, current, updateHook(id), opts.Validate)
		if !ok {
			return
		}
		updated, err := store.Update(r.Context(), id, merged)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		afterUpdate(r, id, updated)
		brcontext.JSON(w, http.StatusOK, updated)
	})

	handle("DELETE", base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if before := opts.Hooks.BeforeDelete; before != nil {
			if err := before(r.Context(), id); err != nil {
				brerrors.WriteError(w, r, err)
				return
			}
		}
		if err := store.Delete(r.Context(), id); err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		if opts.Hooks.AfterDelete != nil {
			opts.Hooks.AfterDelete(r.Context(), id)
		}
		emit(r, brcap.Deleted, id, nil)
		w.WriteHeader(http.StatusNoContent)
	})
}

// withAuth runs auth before next. Errors that aren't ErrUnauthenticated become
// ErrForbidden, so an Auth func can return any error to deny access.
func withAuth(auth func(*http.Request) error, next http.HandlerFunc) http.HandlerFunc {
	if auth == nil {
		return next
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := auth(r); err != nil {
			status := http.StatusForbidden
			if errors.Is(err, brerrors.ErrUnauthenticated) {
				status = http.StatusUnauthorized
			}
			p := brerrors.New(status, "")
			p.Instance = r.URL.Path
			brerrors.Write(w, p)
			return
		}
		next(w, r)
	}
}

// decodeInto decodes the JSON body over input, so fields the body doesn't mention
// keep their values, runs the before hook, then validates the result. The body
// must be a JSON object. On failure it writes a 400, 413, 422 (or the hook's
// mapped error) and returns false.
func decodeInto[T any](w http.ResponseWriter, r *http.Request, input T, before func(context.Context, *T) error, validate func(T) error) (T, bool) {
	raw, err := readObject(r)
	if err == nil {
		err = json.Unmarshal(raw, &input)
	}
	if err != nil {
		p := brerrors.New(http.StatusBadRequest, "invalid JSON body: "+err.Error())
		if tooBig, ok := errors.AsType[*http.MaxBytesError](err); ok {
			p = brerrors.New(http.StatusRequestEntityTooLarge,
				fmt.Sprintf("request body is larger than %d bytes", tooBig.Limit))
		}
		p.Instance = r.URL.Path
		p.RequestID = brcontext.RequestID(r.Context())
		brerrors.Write(w, p)
		return input, false
	}
	if before != nil {
		if err := before(r.Context(), &input); err != nil {
			brerrors.WriteError(w, r, err)
			return input, false
		}
	}
	if validate != nil {
		if err := validate(input); err != nil {
			p := brerrors.FromError(r, err)
			if p.Status != http.StatusUnprocessableEntity {
				p = brerrors.New(http.StatusUnprocessableEntity, err.Error())
				p.Instance = r.URL.Path
				p.RequestID = brcontext.RequestID(r.Context())
			}
			brerrors.Write(w, p)
			return input, false
		}
	}
	return input, true
}

// readObject reads the body and checks it is a single JSON object. A bare null
// would otherwise decode as "no changes", and an array as a type error.
func readObject(r *http.Request) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 || raw[0] != '{' {
		return nil, errors.New("body must be a JSON object")
	}
	return raw, nil
}

// mustModel parses T's schema and panics with the resource path if it is invalid.
func mustModel[T any](path string) *brschema.Model {
	m, err := brschema.Of[T]()
	if err != nil {
		panic(fmt.Sprintf("bridgr: registering /%s: %v", path, err))
	}
	return m
}
