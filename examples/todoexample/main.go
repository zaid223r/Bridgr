package main

import (
	brcrud "bridgr/core/brcrud"
	brdb "bridgr/core/brdb"
	brhttp "bridgr/core/brhttp"
)

type Todo struct {
	ID    uint   `json:"id" gorm:"primaryKey"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

func main() {
	db, err := brdb.ConnectPostgres("test", "123456", "localhost", "bridgr", 5432)
	if err != nil {
		panic(err)
	}
	_ = db.AutoMigrate(&Todo{})

	router := brhttp.NewRouter()

	brcrud.RegisterCRUD[Todo](router, "todos", db, nil)

	brhttp.StartServer(router, "8080")
}
