// Command quickstart is the README example: a Todo API on the in-memory store,
// with OpenAPI at /openapi.json and the API reference at /docs.
//
//	go run ./examples/quickstart
package main

import (
	"log"

	bridgr "github.com/zaid223r/Bridgr"
)

// Todo is a task that can be marked done.
type Todo struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

func main() {
	app := bridgr.New(bridgr.Config{Addr: ":8080", Title: "Todo API", DB: bridgr.Memory()})
	bridgr.CRUD[Todo](app, "todos", &bridgr.CRUDOptions[Todo]{
		Filters: bridgr.Filters{
			"title": bridgr.Contains, // ?title=milk matches substrings
			"done":  bridgr.Exact,    // ?done=false
		},
		Sort: []string{"title"},
	})

	if err := app.ListenAndServe(); err != nil { // stops gracefully on Ctrl+C / SIGTERM
		log.Fatal(err)
	}
}
