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

func (Todo) GenericFiltering() bool {
	return true
}

type Note struct {
	ID    uint   `json:"id" gorm:"primaryKey"`
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (Note) FilterableFields() []string {
	return []string{"title"}
}

func main() {
	db, err := brdb.ConnectPostgres("test", "123456", "localhost", "bridgr", 5432)
	if err != nil {
		panic(err)
	}
	_ = db.AutoMigrate(&Todo{}, &Note{})

	router := brhttp.NewRouter()

	brcrud.RegisterCRUD[Todo](router, "todos", db, nil)
	brcrud.RegisterCRUD[Note](router, "notes", db, nil)

	brhttp.StartServer(router, "8080")
}
