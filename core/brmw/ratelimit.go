package brmw

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// RateLimit allows at most limit requests per client IP in each window.
func RateLimit(limit int, window time.Duration) func(http.Handler) http.Handler {
	var mu sync.Mutex
	counts := map[string]int{}
	reset := time.Now().Add(window)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, _ := net.SplitHostPort(r.RemoteAddr)
			mu.Lock()
			if time.Now().After(reset) {
				counts = map[string]int{}
				reset = time.Now().Add(window)
			}
			counts[ip]++
			over := counts[ip] > limit
			mu.Unlock()
			if over {
				http.Error(w, "too many requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
