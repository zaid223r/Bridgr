package brhttp

import (
	"log"
	"net/http"

	"github.com/zaid223r/Bridgr/api"
)

func StartServer(router *Router, port string) {
	log.Printf("Bridgr API running on port %s...", port)
	router.AddRoute("GET", "/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		json, err := api.BuildSpec()
		if err != nil {
			w.WriteHeader(500)
			w.Write([]byte(err.Error()))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(json)
	})

	router.AddRoute("GET", "/docs", api.ServeSwaggerUI())
	log.Fatal(http.ListenAndServe(":"+port, router))
}
