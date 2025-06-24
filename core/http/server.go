package http

import (
	"log"
	"net/http"
)

func StartServer(router *Router, port string){
	log.Printf("Bridgr API running on port %s...", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
}