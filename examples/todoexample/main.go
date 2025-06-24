package main

import (
	brcrud "bridgr/core/brcrud"
	brdb "bridgr/core/brdb"
	brhttp "bridgr/core/brhttp"
	"bridgr/examples/todoexample/models"
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
