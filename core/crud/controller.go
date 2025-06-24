package crud

import (
	"bridgr/core/context"
	brhttp "bridgr/core/http"
	"encoding/json"
	"net/http"
	"strings"
)

type BridgrOptions[T any] struct {
	Middlewares []func(http.HandlerFunc) http.HandlerFunc
	Validate    func(input T) error
	Auth        func(r *http.Request) bool
}

func RegisterCRUDRoutes[T any](
	router *brhttp.Router,
	path string,
	model BridgrModel[T],
	opts *BridgrOptions[T],
) {
	base := "/" + path

	with := func(handler http.HandlerFunc) http.HandlerFunc {
		for i := len(opts.Middlewares) - 1; i >= 0; i-- {
			handler = opts.Middlewares[i](handler)
		}
		return handler
	}

	authCheck := func(r *http.Request, w http.ResponseWriter) bool {
		if opts != nil && opts.Auth != nil && !opts.Auth(r) {
			context.JSON(w, 403, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}

	router.AddRoute("GET", base, with(func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r, w) {
			return
		}
		items, err := model.List()
		if err != nil {
			context.JSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		context.JSON(w, 200, items)
	}))

	router.AddRoute("POST", base, with(func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r, w) {
			return
		}
		var input T
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			context.JSON(w, 400, map[string]string{"error": "Invalid input"})
			return
		}
		if opts != nil && opts.Validate != nil {
			if err := opts.Validate(input); err != nil {
				context.JSON(w, 422, map[string]string{"error": err.Error()})
				return
			}
		}
		created, err := model.Create(input)
		if err != nil {
			context.JSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		context.JSON(w, 201, created)
	}))

	router.AddRoute("GET", base+"/{id}", with(func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r, w) {
			return
		}
		id := extractID(base, r.URL.Path)
		item, err := model.Get(id)
		if err != nil {
			context.JSON(w, 404, map[string]string{"error": "Not found"})
			return
		}
		context.JSON(w, 200, item)
	}))

	router.AddRoute("PUT", base+"/{id}", with(func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r, w) {
			return
		}
		id := extractID(base, r.URL.Path)
		var input T
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			context.JSON(w, 400, map[string]string{"error": "Invalid input"})
			return
		}
		if opts != nil && opts.Validate != nil {
			if err := opts.Validate(input); err != nil {
				context.JSON(w, 422, map[string]string{"error": err.Error()})
				return
			}
		}
		updated, err := model.Update(id, input)
		if err != nil {
			context.JSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		context.JSON(w, 200, updated)
	}))

	router.AddRoute("DELETE", base+"/{id}", with(func(w http.ResponseWriter, r *http.Request) {
		if !authCheck(r, w) {
			return
		}
		id := extractID(base, r.URL.Path)
		err := model.Delete(id)
		if err != nil {
			context.JSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		context.JSON(w, 204, nil)
	}))
}

func extractID(base string, fullPath string) string {
	return strings.TrimPrefix(fullPath, base+"/")
}
