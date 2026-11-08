// Command server is the API behind the TypeScript client example: a Todo
// resource on the in-memory store.
//
//	go run ./examples/typescript-client/server              # serve on :8080
//	go run ./examples/typescript-client/server -spec x.json # write the OpenAPI document and exit
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/zaid223r/Bridgr/api"
	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

// Todo is a task that can be marked done.
type Todo struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Done     bool   `json:"done"`
	Priority int    `json:"priority"`
}

// FilterableFields lists what the client can filter and sort on.
func (Todo) FilterableFields() []string { return []string{"title", "done", "priority"} }

var info = api.Info{Title: "Todo API", Version: "1.0.0"}

func main() {
	specOut := flag.String("spec", "", "write the OpenAPI document to this file and exit")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	router, err := newRouter()
	if err != nil {
		log.Fatal(err)
	}
	if *specOut != "" {
		doc, err := router.Docs().Build(info)
		if err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(*specOut, append(doc, '\n'), 0o644); err != nil {
			log.Fatal(err)
		}
		return
	}

	server := brhttp.NewServer(router, brhttp.Config{Addr: *addr, Title: info.Title, Version: info.Version})
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func newRouter() (*brhttp.Router, error) {
	todos, err := brstore.NewMemory[Todo]()
	if err != nil {
		return nil, err
	}
	for _, t := range []Todo{
		{Title: "Define a Go struct", Done: true, Priority: 1},
		{Title: "Generate the TypeScript client", Priority: 2},
		{Title: "Ship it", Priority: 3},
	} {
		if _, err := todos.Create(context.Background(), t); err != nil {
			return nil, err
		}
	}

	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[Todo](router, "todos", todos, &brcrud.Options[Todo]{
		Validate: func(t Todo) error {
			if strings.TrimSpace(t.Title) == "" {
				return errors.New("title is required")
			}
			return nil
		},
	})
	return router, nil
}
