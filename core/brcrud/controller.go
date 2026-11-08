// Package brcrud registers REST CRUD routes for a model type backed by a
// brstore.Store.
package brcrud

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/zaid223r/Bridgr/api"
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
	Middlewares []func(http.HandlerFunc) http.HandlerFunc
	Validate    func(input T) error
	Auth        func(r *http.Request) error
}

// RegisterCRUD mounts list, create, get, update and delete routes for T under
// /path, backed by store, and documents them in the OpenAPI spec. A nil opts uses
// the defaults. See parseQuery for how list query parameters become filters.
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

	base := "/" + path

	var zero T
	api.RegisterPath("get", base, zero)
	api.RegisterPath("post", base, zero)
	api.RegisterPath("get", base+"/{id}", zero)
	api.RegisterPath("put", base+"/{id}", zero)
	api.RegisterPath("delete", base+"/{id}", zero)

	handle := func(method, pattern string, handler http.HandlerFunc) {
		h := withAuth(opts.Auth, handler)
		for i := len(opts.Middlewares) - 1; i >= 0; i-- {
			h = opts.Middlewares[i](h)
		}
		router.AddRoute(method, pattern, h)
	}

	handle("GET", base, func(w http.ResponseWriter, r *http.Request) {
		q, err := parseQuery(model, r.URL.Query())
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		items, err := store.List(r.Context(), q)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		brcontext.JSON(w, http.StatusOK, items)
	})

	handle("POST", base, func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeInput(w, r, opts.Validate)
		if !ok {
			return
		}
		created, err := store.Create(r.Context(), input)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
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
		input, ok := decodeInput(w, r, opts.Validate)
		if !ok {
			return
		}
		updated, err := store.Update(r.Context(), r.PathValue("id"), input)
		if err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
		brcontext.JSON(w, http.StatusOK, updated)
	})

	handle("DELETE", base+"/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := store.Delete(r.Context(), r.PathValue("id")); err != nil {
			brerrors.WriteError(w, r, err)
			return
		}
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

// decodeInput reads the JSON body into a T and validates it. On failure it writes
// a 400, 413 or 422 problem and returns false.
func decodeInput[T any](w http.ResponseWriter, r *http.Request, validate func(T) error) (T, bool) {
	var input T
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		p := brerrors.New(http.StatusBadRequest, "invalid JSON body: "+err.Error())
		if tooBig, ok := errors.AsType[*http.MaxBytesError](err); ok {
			p = brerrors.New(http.StatusRequestEntityTooLarge,
				fmt.Sprintf("request body is larger than %d bytes", tooBig.Limit))
		}
		p.Instance = r.URL.Path
		brerrors.Write(w, p)
		return input, false
	}
	if validate != nil {
		if err := validate(input); err != nil {
			p := brerrors.New(http.StatusUnprocessableEntity, err.Error())
			p.Instance = r.URL.Path
			brerrors.Write(w, p)
			return input, false
		}
	}
	return input, true
}

// mustModel parses T's schema and panics with the resource path if it is invalid.
func mustModel[T any](path string) *brschema.Model {
	m, err := brschema.Of[T]()
	if err != nil {
		panic(fmt.Sprintf("bridgr: registering /%s: %v", path, err))
	}
	return m
}
