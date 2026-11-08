// Command todoexample serves Todo and Note CRUD APIs backed by PostgreSQL.
//
//	DATABASE_URL="postgres://user:pass@localhost:5432/db?sslmode=disable" go run ./examples/todoexample
package main

import (
	"context"
	"database/sql"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore/sqlstore"
	"github.com/zaid223r/Bridgr/examples/todoexample/models"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
	if err != nil {
		return err
	}
	defer db.Close()

	todos, err := newStore[models.Todo](db)
	if err != nil {
		return err
	}
	notes, err := newStore[models.Note](db)
	if err != nil {
		return err
	}

	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[models.Todo](router, "todos", todos, nil)
	brcrud.RegisterCRUD[models.Note](router, "notes", notes, nil)

	return brhttp.NewServer(router, brhttp.Config{Addr: ":8080"}).ListenAndServe()
}

// newStore opens a Postgres store for T and creates its table if needed.
func newStore[T any](db *sql.DB) (*sqlstore.Store[T], error) {
	s, err := sqlstore.New[T](db, sqlstore.Postgres)
	if err != nil {
		return nil, err
	}
	return s, s.CreateTable(context.Background())
}
