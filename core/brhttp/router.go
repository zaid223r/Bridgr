// Package brhttp contains the HTTP router and server bootstrap.
package brhttp

import (
	"context"
	"net/http"
	"strings"
)

// Route binds an HTTP method and path pattern to a handler.
type Route struct {
	Method  string
	Path    string
	Handler http.HandlerFunc
}

// Router matches requests against routes in registration order. Path segments
// written as {name} capture a parameter, read it with Params.
type Router struct {
	routes []Route
}

// NewRouter returns an empty Router.
func NewRouter() *Router {
	return &Router{}
}

// AddRoute registers handler for method and path. It is not safe to call while serving.
func (r *Router) AddRoute(method, path string, handler http.HandlerFunc) {
	r.routes = append(r.routes, Route{
		Method:  method,
		Path:    path,
		Handler: handler,
	})
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	for _, route := range r.routes {
		if match, params := matchRoute(route.Path, req.URL.Path); match && req.Method == route.Method {
			ctx := context.WithValue(req.Context(), ParamsKey, params)
			req = req.WithContext(ctx)
			route.Handler(w, req)
			return
		}
	}
	http.NotFound(w, req)
}

type paramsKeyType string

// ParamsKey is the request-context key holding the matched path parameters.
const ParamsKey paramsKeyType = "params"

func matchRoute(pattern, path string) (bool, map[string]string) {
	patternParts := strings.Split(pattern, "/")
	pathParts := strings.Split(path, "/")
	if len(patternParts) != len(pathParts) {
		return false, nil
	}
	params := make(map[string]string)
	for i := range patternParts {
		if strings.HasPrefix(patternParts[i], "{") && strings.HasSuffix(patternParts[i], "}") {
			key := patternParts[i][1 : len(patternParts[i])-1]
			params[key] = pathParts[i]
		} else if patternParts[i] != pathParts[i] {
			return false, nil
		}
	}
	return true, params
}

// Params returns the path parameters from the request context.
func Params(r *http.Request) map[string]string {
	params, _ := r.Context().Value(ParamsKey).(map[string]string)
	return params
}
