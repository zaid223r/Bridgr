package bridgr

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/zaid223r/Bridgr/core/brstore"
	"github.com/zaid223r/Bridgr/core/brstore/sqlstore"
)

// Database is where bridgr.CRUD keeps resources that don't bring their own
// store (CRUDOptions.Store). Set it once in Config.DB; each resource then gets
// its own table, named after the model (Todo → todos, OrderItem → order_items).
//
// Create one with SQL or Memory. Other backends plug in per resource through
// CRUDOptions.Store.
type Database struct {
	db      *sql.DB
	dialect sqlstore.Dialect
}

// SQL keeps resources in db, using dialect for the SQL that differs between
// databases: sqlstore.SQLite, sqlstore.Postgres, or mysql.Dialect from
// sqlstore/mysql. The caller opens db with its driver and owns it, so pick the
// driver, pool size and DSN as usual:
//
//	db, err := sql.Open("pgx", os.Getenv("DATABASE_URL")) // import _ "github.com/jackc/pgx/v5/stdlib"
//	app := bridgr.New(bridgr.Config{DB: bridgr.SQL(db, sqlstore.Postgres)})
func SQL(db *sql.DB, dialect sqlstore.Dialect) *Database {
	if db == nil || dialect == nil {
		panic("bridgr.SQL: db and dialect are required")
	}
	return &Database{db: db, dialect: dialect}
}

// Memory keeps resources in process memory: nothing to set up, and everything
// is lost on exit. Suited to tests, prototypes and examples.
func Memory() *Database {
	return &Database{}
}

// tableCreator is implemented by stores that can create their own table, such
// as sqlstore.Store.
type tableCreator interface {
	CreateTable(ctx context.Context) error
}

// storeFor builds T's store in the app's database. It panics when the app has
// no database or T can't be stored in it: both are mistakes visible at startup.
func storeFor[T any](a *App, path string) brstore.Store[T] {
	db := a.cfg.DB
	if db == nil {
		panic(fmt.Sprintf("bridgr.CRUD /%s: no store; set Config.DB or CRUDOptions.Store", path))
	}
	var (
		store brstore.Store[T]
		err   error
	)
	if db.db == nil {
		store, err = brstore.NewMemory[T]()
	} else {
		store, err = sqlstore.New[T](db.db, db.dialect)
	}
	if err != nil {
		panic(fmt.Sprintf("bridgr.CRUD /%s: %v", path, err))
	}
	return store
}

// CreateTables creates the table of every resource whose store supports it
// (the SQL stores do), skipping tables that already exist. Run and Serve call
// it first when Config.CreateTables is set; call it yourself before Handler.
// It suits development and tests; use a migration tool for production schemas.
func (a *App) CreateTables(ctx context.Context) error {
	a.mu.Lock()
	tables := append([]tableCreator(nil), a.tables...)
	a.mu.Unlock()
	for _, t := range tables {
		if err := t.CreateTable(ctx); err != nil {
			return err
		}
	}
	return nil
}
