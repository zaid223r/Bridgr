package main

import (
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

	brhttp.StartServer(router, "8080")
}
