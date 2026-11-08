# Bridgr

[![CI](https://github.com/zaid223r/Bridgr/actions/workflows/ci.yml/badge.svg)](https://github.com/zaid223r/Bridgr/actions/workflows/ci.yml)

Turn Go structs into documented REST APIs with one line per resource.

> **Status: early development.** The API will change. Bridgr is being rebuilt in public;
> see [ROADMAP.md](ROADMAP.md) for what is done and what is next.

## What works today

- **Generic CRUD**: `GET/POST /todos`, `GET/PUT/DELETE /todos/{id}` from a single call.
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
  now: in-memory (`brstore.NewMemory`) and PostgreSQL through GORM (being replaced by a faster `database/sql` layer).

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
	brcrud.RegisterStore[Todo](router, "todos", todos, nil)

	server := brhttp.NewServer(router, brhttp.Config{Addr: ":8080"})
	if err := server.ListenAndServe(); err != nil { // stops gracefully on Ctrl+C / SIGTERM
		log.Fatal(err)
	}
}
```

For PostgreSQL, swap the store for `brcrud.RegisterCRUD[Todo](router, "todos", db, nil)` with
a GORM connection from `brdb.ConnectPostgres` (a faster `database/sql` store is on the
roadmap). [examples/todoexample](examples/todoexample) shows that version.

Then try:

- `POST /todos` with `{"title":"buy milk"}`
- `GET /todos?title=milk&done=false` (strings match case-insensitively as substrings;
  repeat a parameter to match any of several values)
- `GET /docs` for Swagger UI, `GET /openapi.json` for the spec

## Known limitations (being fixed, see the roadmap)

- `PUT` ignores zero values, so `{"done": false}` doesn't clear a field.
- List endpoints have no pagination yet.

## Where it's going

SQL first (PostgreSQL, SQLite, MySQL) without an ORM, MongoDB as an optional module,
OpenAPI 3.1 complete enough to generate typed TypeScript clients, validation, auth hooks,
middleware, and published benchmarks. Full plan: [ROADMAP.md](ROADMAP.md).

## Development

```bash
make check   # format, vet, lint, race tests
```

Contributor and AI-assistant conventions are in [CLAUDE.md](CLAUDE.md).

## License

MIT
