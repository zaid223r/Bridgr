// Command quickstart is the README example: a Todo API on the in-memory store,
// with OpenAPI at /openapi.json and the API reference at /docs.
//
//	go run ./examples/quickstart
package main

import (
	"log"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// Todo is a task that can be marked done.
type Todo struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// GenericFiltering makes every Todo field filterable.
func (Todo) GenericFiltering() bool { return true }

func main() {
	todos, err := brstore.NewMemory[Todo]()
	if err != nil {
		log.Fatal(err)
	}

	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[Todo](router, "todos", todos, nil)

	server := brhttp.NewServer(router, brhttp.Config{Addr: ":8080", Title: "Todo API"})
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
