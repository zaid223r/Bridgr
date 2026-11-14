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

	// Filters lists the fields list requests may filter on and the operators
	// each allows, e.g. Filters{"title": Exact | Contains, "qty": Range}.
	// Nothing else is filterable.
	Filters Filters
	// Sort lists the fields list requests may sort on, besides the primary key.
	Sort []string

	// DefaultLimit is the page size when a list request has no limit. Default 50.
	DefaultLimit int
	// MaxLimit is the largest limit a client may ask for. Default 200.
	MaxLimit int

	// Create, Update and Response replace the record type in request and
	// response bodies; see Input, Apply and Output. Unset, bodies are T
	// (shaped by its readonly and writeonly tags).
	Create   *CreateInput[T]
	Update   *UpdateInput[T]
	Response *Response[T]

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
// It panics if T is not a valid model, has no primary key, or Filters or Sort
// name a field T doesn't have or an operator that doesn't fit its type, so
// mistakes stop the program at startup.
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
	where := "bridgr: registering /" + path
	if opts.Response != nil {
		checkResponseType(where, opts.Response.typ)
	} else if at, ok := brschema.NestedWriteOnly(model.Type); ok {
		panic(fmt.Sprintf("%s: %s has writeonly fields, which are only hidden at the top level of a resource; drop the tag or set Response", where, at))
	}
	acc := newAccess[T](model)
	// view is what clients receive for a stored record.
	view := func(v T) any { return v }
	if opts.Response != nil {
		view = opts.Response.view
	}
	list := resolveList(where, model, opts.Filters, opts.Sort, limits)

	base := "/" + path

	router.Docs().AddCRUD(path, model, api.CRUDDocs{
		Auth:         opts.Auth != nil,
		Validate:     opts.Validate != nil || opts.Create != nil || opts.Update != nil,
		DefaultLimit: limits.def,
		MaxLimit:     limits.max,
		Filters:      list.docs(),
		Sort:         list.sortable,
		CreateBody:   typeOf(opts.Create),
		UpdateBody:   typeOf(opts.Update),
		Response:     typeOf(opts.Response),
	})

	handle := func(method, pattern string, handler http.HandlerFunc) {
		var h http.Handler = withAuth(opts.Auth, handler)
		for _, mw := range slices.Backward(opts.Middleware) {
			h = mw(h)
		}
		router.AddRoute(method, pattern, h.ServeHTTP)
	}

	handle("GET", base, func(w http.ResponseWriter, r *http.Request) {
		q, err := parseQuery(list, r.URL.Query())
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
		acc.hideAll(items)
		if opts.Response == nil {
			brcontext.JSON(w, http.StatusOK, items)
			return
		}
		out := make([]any, len(items))
		for i, v := range items {
			out[i] = view(v)
		}
		brcontext.JSON(w, http.StatusOK, out)
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
	// afterUpdate runs the hook on the full record and returns the visible one.
	afterUpdate := func(r *http.Request, id string, v T) T {
		if opts.Hooks.AfterUpdate != nil {
			opts.Hooks.AfterUpdate(r.Context(), v)
		}
		v = acc.hide(v)
		emit(r, brcap.Updated, id, v)
		return v
	}
	// applyUpdate serves PUT and PATCH through Options.Update.
	applyUpdate := func(w http.ResponseWriter, r *http.Request, id string) {
		current, err := store.Get(r.Context(), id)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		next, ok := buildInto(w, r, func(raw json.RawMessage) (T, error) { return opts.Update.build(r.Context(), current, raw) },
			updateHook(id), opts.Validate)
		if !ok {
			return
		}
		updated, err := store.Update(r.Context(), id, next)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		brcontext.JSON(w, http.StatusOK, view(afterUpdate(r, id, updated)))
	}

	handle("POST", base, func(w http.ResponseWriter, r *http.Request) {
		var zero T
		var input T
		var ok bool
		if opts.Create != nil {
			input, ok = buildInto(w, r, func(raw json.RawMessage) (T, error) { return opts.Create.build(r.Context(), raw) },
				opts.Hooks.BeforeCreate, opts.Validate)
		} else {
			input, ok = decodeInto(w, r, acc, zero, zero, opts.Hooks.BeforeCreate, opts.Validate)
		}
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
		created = acc.hide(created)
		emit(r, brcap.Created, keyOf(created), created)
		brcontext.JSON(w, http.StatusCreated, view(created))
	})

	handle("GET", base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		item, err := store.Get(r.Context(), r.PathValue("id"))
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		brcontext.JSON(w, http.StatusOK, view(acc.hide(item)))
	})

	handle("PUT", base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if opts.Update != nil {
			applyUpdate(w, r, id)
			return
		}
		var zero T
		// PUT resets every field the body leaves out, except readonly ones,
		// which keep their stored values.
		base := zero
		if len(model.ReadOnly) > 0 {
			current, err := store.Get(r.Context(), id)
			if err != nil {
				brerrors.WriteError(w, r, err)
				return
			}
			base = current
		}
		input, ok := decodeInto(w, r, acc, zero, base, updateHook(id), opts.Validate)
		if !ok {
			return
		}
		updated, err := store.Update(r.Context(), id, input)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		brcontext.JSON(w, http.StatusOK, view(afterUpdate(r, id, updated)))
	})

	handle("PATCH", base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if opts.Update != nil {
			applyUpdate(w, r, id)
			return
		}
		current, err := store.Get(r.Context(), id)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		// Decoding into the stored value leaves fields absent from the body as
		// they are. A body id is ignored: Store.Update takes the key from id.
		merged, ok := decodeInto(w, r, acc, current, current, updateHook(id), opts.Validate)
		if !ok {
			return
		}
		updated, err := store.Update(r.Context(), id, merged)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		brcontext.JSON(w, http.StatusOK, view(afterUpdate(r, id, updated)))
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
// keep their values, resets readonly fields to base's, then runs the before
// hook and validation (see finish). The body must be a JSON object. On failure
// it writes the error response and returns false.
func decodeInto[T any](w http.ResponseWriter, r *http.Request, acc access[T], input, base T, before func(context.Context, *T) error, validate func(T) error) (T, bool) {
	acc.clearReadOnly(&input)
	raw, err := readObject(r)
	if err == nil {
		err = json.Unmarshal(raw, &input)
	}
	acc.keepReadOnly(&input, base)
	if err != nil {
		writeBodyError(w, r, err)
		return input, false
	}
	return finish(w, r, input, before, validate)
}

// buildInto reads the body and turns it into a record with build (a custom
// input), then runs the before hook and validation.
func buildInto[T any](w http.ResponseWriter, r *http.Request, build func(json.RawMessage) (T, error), before func(context.Context, *T) error, validate func(T) error) (T, bool) {
	raw, err := readObject(r)
	if err != nil {
		var zero T
		writeBodyError(w, r, err)
		return zero, false
	}
	input, err := build(raw)
	if err != nil {
		writeBuildError(w, r, err)
		return input, false
	}
	return finish(w, r, input, before, validate)
}

// finish runs the before hook, then validates. Hook errors are mapped like any
// other; validation errors respond 422.
func finish[T any](w http.ResponseWriter, r *http.Request, input T, before func(context.Context, *T) error, validate func(T) error) (T, bool) {
	if before != nil {
		if err := before(r.Context(), &input); err != nil {
			brerrors.WriteError(w, r, err)
			return input, false
		}
	}
	if validate != nil {
		if err := validate(input); err != nil {
			writeValidation(w, r, err)
			return input, false
		}
	}
	return input, true
}

// typeOf returns the custom type carried by a Create, Update or Response
// option, or nil when it is unset.
func typeOf[O interface{ customType() reflect.Type }](o O) reflect.Type {
	if reflect.ValueOf(o).IsNil() {
		return nil
	}
	return o.customType()
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
