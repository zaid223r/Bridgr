# Bridgr

[![CI](https://github.com/zaid223r/Bridgr/actions/workflows/ci.yml/badge.svg)](https://github.com/zaid223r/Bridgr/actions/workflows/ci.yml)

Turn Go structs into documented REST APIs with one line per resource.

> **Status: early development.** The API will change. Bridgr is being rebuilt in public;
> see [ROADMAP.md](ROADMAP.md) for what is done and what is next.

## What works today

- **Generic CRUD**: `GET/POST /todos`, `GET/PUT/PATCH/DELETE /todos/{id}` from a single
  call. `PUT` replaces the record (missing fields become zero); `PATCH` changes only the
  fields in the body, so `{"done": false}` sets `done` and leaves the rest alone.
- **Filtering** that models opt into:
  - `FilterableFields()` lists the fields that can be filtered.
  - `GenericFiltering()` makes every field filterable.
  - String fields match partially (`LIKE`), other fields match exactly.
  - Unknown filter fields return `400`.
  - A `FilterableFields` entry that isn't a JSON field of the model panics at
    registration, so the mistake stops the program at startup.
- **OpenAPI**: `/openapi.json` and a Swagger UI at `/docs`, with filters documented as query
  parameters.
- **Errors** as [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details
  (`application/problem+json`): 400 bad input, 401/403 from your `Auth` func, 404 missing
  records, 422 failed validation. Internal errors return a generic 500 and are logged, so
  database messages never leak to clients.
- **Production server**: `brhttp.NewServer` sets read/write/idle timeouts, caps request
  bodies (1 MiB by default, 413 above), logs every request with `log/slog`, passes the
  request context down to the database, and shuts down gracefully on SIGINT/SIGTERM.
- **Storage** behind one `brstore.Store` interface with typed queries (filters, operators,
  sorting, paging) and a shared contract test suite every backend must pass. Available
  now: in-memory (`brstore.NewMemory`), and SQLite and PostgreSQL on plain `database/sql`
  with no ORM (`sqlstore.New[T](db, sqlstore.SQLite)` or `sqlstore.Postgres`).
- **Struct tags**: `json` names fields in the API; `db:"column"` or `db:"column,pk"` sets
  the database column and primary key (default: snake_case of the field name, and the
  field named `ID`). GORM's `column` and `primaryKey` tags are also read, so GORM models
  work unchanged.

## Quick start

No database needed: this uses the in-memory store.

```go
package main

import (
	"log"

	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brhttp"
	"github.com/zaid223r/Bridgr/core/brstore"
)

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

	server := brhttp.NewServer(router, brhttp.Config{Addr: ":8080"})
	if err := server.ListenAndServe(); err != nil { // stops gracefully on Ctrl+C / SIGTERM
		log.Fatal(err)
	}
}
```

To keep data in SQLite instead, swap the store:

```go
// import "database/sql"; _ "modernc.org/sqlite"; ".../core/brstore/sqlstore"
db, err := sql.Open("sqlite", "file:app.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
todos, err := sqlstore.New[Todo](db, sqlstore.SQLite) // table "todos"
err = todos.CreateTable(ctx)                          // for prototypes; use migrations in production
```

For PostgreSQL, open the database with any `database/sql` driver and use the Postgres
dialect:

```go
// import _ "github.com/jackc/pgx/v5/stdlib"
db, err := sql.Open("pgx", os.Getenv("DATABASE_URL"))
todos, err := sqlstore.New[Todo](db, sqlstore.Postgres)
```

[examples/todoexample](examples/todoexample) is a complete Postgres example.

Then try:

- `POST /todos` with `{"title":"buy milk"}`
- `GET /todos?title=milk&done=false` (strings match case-insensitively as substrings;
  repeat a parameter to match any of several values)
- `GET /docs` for Swagger UI, `GET /openapi.json` for the spec

## Known limitations (being fixed, see the roadmap)

- List endpoints have no pagination yet.

## Where it's going

SQL first (PostgreSQL, SQLite, MySQL) without an ORM, MongoDB as an optional module,
OpenAPI 3.1 complete enough to generate typed TypeScript clients, validation, auth hooks,
middleware, and published benchmarks. Full plan: [ROADMAP.md](ROADMAP.md).

## Development

```bash
make check         # format, vet, lint, race tests
make integration   # also runs Postgres tests (starts it with docker compose)
```

Contributor and AI-assistant conventions are in [CLAUDE.md](CLAUDE.md).

## License

MIT
