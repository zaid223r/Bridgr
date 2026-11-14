// Command todoexample serves Todo and Note CRUD APIs backed by PostgreSQL.
//
//	DATABASE_URL="postgres://user:pass@localhost:5432/db?sslmode=disable" go run ./examples/todoexample
package main

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib"

	bridgr "github.com/zaid223r/Bridgr"
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

	app := bridgr.New(bridgr.Config{
		Addr:         ":8080",
		Title:        "Todo and Note API",
		DB:           bridgr.SQL(db, sqlstore.Postgres),
		CreateTables: true, // tables "todos" and "notes"
	})
	bridgr.CRUD[models.Todo](app, "todos", models.TodoOptions)
	bridgr.CRUD[models.Note](app, "notes", models.NoteOptions)

	return app.ListenAndServe()
}
