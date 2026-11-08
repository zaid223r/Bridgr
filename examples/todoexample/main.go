// Command todoexample serves Todo and Note CRUD APIs backed by Postgres.
package main

import (
	"log"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brdb"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/examples/todoexample/models"
)

func main() {
	db, err := brdb.ConnectPostgres("test", "123456", "localhost", "bridgr", 5432)
	if err != nil {
		panic(err)
	}
	_ = db.AutoMigrate(&models.Todo{}, &models.Note{})

	router := brhttp.NewRouter()

	brcrud.RegisterCRUD[models.Todo](router, "todos", db, nil)
	brcrud.RegisterCRUD[models.Note](router, "notes", db, nil)

	server := brhttp.NewServer(router, brhttp.Config{Addr: ":8080"})
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
