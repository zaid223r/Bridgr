package brhttp

import (
	"log"
	"net/http"

	"github.com/zaid223r/Bridgr/api"
)

// StartServer adds the /openapi.json and /docs routes and serves router on port.
// It blocks and exits the process if the server stops.
func StartServer(router *Router, port string) {
	log.Printf("Bridgr API running on port %s...", port)
	router.AddRoute("GET", "/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		json, err := api.BuildSpec()
		if err != nil {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(json)
	})

	router.AddRoute("GET", "/docs", api.ServeSwaggerUI())
	log.Fatal(http.ListenAndServe(":"+port, router))
}
