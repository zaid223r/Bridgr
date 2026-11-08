// Package brmw holds HTTP middleware for Bridgr apps. Request IDs, logging and
// panic recovery are built into the server; these are opt-in.
package brmw

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

// CORSOptions configures CORS. AllowedOrigins is required; the other fields
// have defaults suited to a Bridgr API.
type CORSOptions struct {
	// AllowedOrigins lists origins such as "https://app.example.com", or "*"
	// for any origin.
	AllowedOrigins []string
	// AllowedMethods defaults to GET, POST, PUT, PATCH and DELETE.
	AllowedMethods []string
	// AllowedHeaders defaults to Content-Type, Authorization and X-Request-ID.
	AllowedHeaders []string
	// ExposedHeaders are response headers scripts may read. Defaults to
	// X-Total-Count, Link and X-Request-ID, which list endpoints and errors use.
	ExposedHeaders []string
	// AllowCredentials lets browsers send cookies and HTTP auth.
	AllowCredentials bool
	// MaxAge is how long browsers may cache a preflight response. Default 10m.
	MaxAge time.Duration
}

// CORS returns middleware that adds CORS headers for allowed origins and
// answers preflight requests itself. Register it app-wide (Config.Middleware),
// because preflight requests use OPTIONS, which resources don't route.
//
// It panics if no origins are given, or if AllowCredentials is combined with
// "*": that would let any website make authenticated requests on a user's
// behalf.
func CORS(opts CORSOptions) func(http.Handler) http.Handler {
	if len(opts.AllowedOrigins) == 0 {
		panic("brmw.CORS: AllowedOrigins is empty")
	}
	anyOrigin := slices.Contains(opts.AllowedOrigins, "*")
	if anyOrigin && opts.AllowCredentials {
		panic(`brmw.CORS: AllowCredentials can't be used with origin "*"; list the trusted origins`)
	}
	origins := map[string]bool{}
	for _, o := range opts.AllowedOrigins {
		origins[strings.ToLower(strings.TrimSuffix(o, "/"))] = true
	}
	methods := strings.Join(orDefault(opts.AllowedMethods, []string{"GET", "POST", "PUT", "PATCH", "DELETE"}), ", ")
	headers := strings.Join(orDefault(opts.AllowedHeaders, []string{"Content-Type", "Authorization", "X-Request-ID"}), ", ")
	exposed := strings.Join(orDefault(opts.ExposedHeaders, []string{"X-Total-Count", "Link", "X-Request-ID"}), ", ")
	maxAge := strconv.Itoa(int(orDefaultDuration(opts.MaxAge, 10*time.Minute).Seconds()))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			h := w.Header()
			h.Add("Vary", "Origin")
			if origin == "" || (!anyOrigin && !origins[strings.ToLower(origin)]) {
				next.ServeHTTP(w, r)
				return
			}
			if anyOrigin {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
			}
			if opts.AllowCredentials {
				h.Set("Access-Control-Allow-Credentials", "true")
			}

			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Add("Vary", "Access-Control-Request-Method")
				h.Add("Vary", "Access-Control-Request-Headers")
				h.Set("Access-Control-Allow-Methods", methods)
				h.Set("Access-Control-Allow-Headers", headers)
				h.Set("Access-Control-Max-Age", maxAge)
				w.WriteHeader(http.StatusNoContent)
				return
			}
			h.Set("Access-Control-Expose-Headers", exposed)
			next.ServeHTTP(w, r)
		})
	}
}

func orDefault(v, def []string) []string {
	if len(v) == 0 {
		return def
	}
	return v
}

func orDefaultDuration(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}
