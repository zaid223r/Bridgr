// Package brhttp contains the HTTP router and server bootstrap.
package brhttp

import "net/http"

// Router dispatches requests using the standard library's http.ServeMux.
//
// Paths use ServeMux pattern syntax: {name} matches one path segment and is read
// with r.PathValue("name"), {name...} matches the rest of the path. Matching is
// exact, so "/todos" does not match "/todos/". A path that exists under another
// method gets 405 Method Not Allowed with an Allow header; GET routes also answer
// HEAD.
type Router struct {
	mux *http.ServeMux
}

// NewRouter returns an empty Router.
func NewRouter() *Router {
	return &Router{mux: http.NewServeMux()}
}

// AddRoute registers handler for method and path. An empty method matches every
// method. It panics if the pattern is invalid or conflicts with an existing route,
// so mistakes surface at startup. It is not safe to call while serving.
func (r *Router) AddRoute(method, path string, handler http.HandlerFunc) {
	pattern := path
	if method != "" {
		pattern = method + " " + path
	}
	r.mux.HandleFunc(pattern, handler)
}

// ServeHTTP dispatches the request to the matching route, or replies 404 or 405.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}
