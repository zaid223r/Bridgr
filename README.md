# Bridgr

[![CI](https://github.com/zaid223r/Bridgr/actions/workflows/ci.yml/badge.svg)](https://github.com/zaid223r/Bridgr/actions/workflows/ci.yml)

**The bridge from a Go struct to a production API.**

Bridgr is a modular Go framework. Its core turns a struct into documented REST endpoints,
with storage, filtering, paging and OpenAPI built in. Everything else a real API needs
plugs in as a module on top of that core: auth, validation, scheduled jobs, rate limiting,
caching, events and webhooks, and file uploads.

```go
type Todo struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

brcrud.RegisterCRUD[Todo](router, "todos", store, nil) // 6 endpoints + OpenAPI
```

> **Status: early development.** The API will change. Bridgr is being rebuilt in public;
> see [ROADMAP.md](ROADMAP.md) for what is done and what is next.

## The core: what works today

- **Generic CRUD**: `GET/POST /todos`, `GET/PUT/PATCH/DELETE /todos/{id}` from a single
  call. `PUT` replaces the record (missing fields become zero); `PATCH` changes only the
  fields in the body, so `{"done": false}` sets `done` and leaves the rest alone.
- **Filtering** that models opt into:
  - `FilterableFields()` lists the fields that can be filtered.
  - `GenericFiltering()` makes every field filterable.
  - `?title=milk`: strings match case-insensitively as substrings; other types match
    exactly; repeat a parameter to match any of several values.
  - Operators: `?qty[gte]=3&qty[lt]=10`, `?title[eq]=Buy milk`, `?qty[in]=1,2,3`.
    Strings support `eq ne contains in`, booleans `eq ne`, numbers and times
    `eq ne gt gte lt lte in`.
  - Unknown fields, unsupported operators and unparsable values return `400` with the
    offending field in `invalid_fields`. Values are always bound as SQL parameters.
- **Paging and sorting**: `?limit=20&offset=40&sort=-qty,title`. Lists return 50 records
  by default and at most 200 (`Options.DefaultLimit` / `MaxLimit`). Responses carry
  `X-Total-Count` and an RFC 8288 `Link` header with `first`, `prev`, `next` and `last`
  pages that keep your filters. Sorting works on the primary key and filterable fields.
  - A `FilterableFields` entry that isn't a JSON field of the model panics at
    registration, so the mistake stops the program at startup.
- **OpenAPI 3.1** at `/openapi.json` and a [Scalar](https://github.com/scalar/scalar) API
  reference at `/docs`. Scalar is pinned and embedded, so docs work offline and need no
  CDN; `brhttp.Config{Docs: api.DocsOptions{Theme: "moon", DarkMode: true, CDN: true}}`
  customizes it. The document itself:
  - a schema per model (`Todo`) plus a request schema (`TodoInput`: nothing required,
    generated ids left out), nested structs as their own components
  - every response documented: success bodies, `X-Total-Count`/`Link` headers on lists,
    and each possible error as an RFC 9457 `Problem`
  - stable operation ids (`listTodos`, `createTodo`, `getTodo`, `replaceTodo`,
    `updateTodo`, `deleteTodo`) for client generators
  - filters, operators, `limit`/`offset`/`sort` documented as query parameters
  - title and version from `brhttp.Config{Title, Version}`
  - checked in tests: the document passes the OpenAPI 3.1 schema, and real requests
    and responses for every documented status are validated against it
- **Errors** as [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details
  (`application/problem+json`): 400 bad input, 401/403 from your `Auth` func, 404 missing
  records, 422 failed validation. Internal errors return a generic 500 and are logged, so
  database messages never leak to clients.
- **Production server**: `brhttp.NewServer` sets read/write/idle timeouts, caps request
  bodies (1 MiB by default, 413 above), logs every request with `log/slog`, passes the
  request context down to the database, and shuts down gracefully on SIGINT/SIGTERM.
- **Storage** behind one `brstore.Store` interface with typed queries (filters, operators,
  sorting, paging) and a shared contract test suite every backend must pass. Available
  now: in-memory (`brstore.NewMemory`), and SQLite, PostgreSQL and MySQL on plain
  `database/sql` with no ORM (`sqlstore.SQLite`, `sqlstore.Postgres`, `mysql.Dialect`
  from `sqlstore/mysql`). All of them pass the same contract tests, in CI.
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

For MySQL 8, use `github.com/go-sql-driver/mysql` with `parseTime=true` in the DSN:

```go
// import _ "github.com/go-sql-driver/mysql"; ".../core/brstore/sqlstore/mysql"
db, err := sql.Open("mysql", "user:pass@tcp(localhost:3306)/app?parseTime=true")
todos, err := sqlstore.New[Todo](db, mysql.Dialect)
```

Then try:

- `POST /todos` with `{"title":"buy milk"}`
- `GET /todos?title=milk&done=false` (strings match case-insensitively as substrings;
  repeat a parameter to match any of several values)
- `GET /docs` for the API reference, `GET /openapi.json` for the spec

The same example is runnable: `go run ./examples/quickstart`.

## Where it's going

The core comes first: a single `bridgr.App` entry point, a module contract (routes,
middleware, OpenAPI additions, background workers) with shared capability interfaces,
middleware and hooks, typed custom endpoints, a typed TypeScript client example, published benchmarks
and MongoDB, then a `v0.1.0` release of the core.

After that, modules, each in **its own repository** (`bridgr-<name>`) so you only add
what you use. They import only the core, never each other, and cooperate through
interfaces the core defines (caller identity, scheduler, key-value store, events,
validation errors). Every module runs the core's compatibility test suite in CI, and a
shared `bridgr-compat` project runs all of them together:

| Module | What it adds |
|---|---|
| `bridgr-validate` | struct-tag validation, 422 with field errors, rules in the OpenAPI schema |
| `bridgr-auth` | API keys and JWT, per-operation authorization, OpenAPI security schemes |
| `bridgr-jobs` | scheduled jobs (cron and intervals) with retries and single-runner locking |
| `bridgr-ratelimit` | per-client and per-route limits, 429 with `Retry-After` |
| `bridgr-cache` | response caching with `ETag` / `If-None-Match` |
| `bridgr-events` | events on create/update/delete, signed webhooks with retries |
| `bridgr-uploads` | multipart uploads to disk or S3-compatible storage |

Full plan: [ROADMAP.md](ROADMAP.md).

## Development

```bash
make check         # format, vet, lint, race tests
make integration   # also runs Postgres and MySQL tests (starts them with docker compose)
```

Contributor and AI-assistant conventions are in [CLAUDE.md](CLAUDE.md).

## License

MIT
