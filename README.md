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
- **OpenAPI**: `/openapi.json` and a Swagger UI at `/docs`, with filters documented as query
  parameters.
- **Errors** as [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem details
  (`application/problem+json`): 400 bad input, 401/403 from your `Auth` func, 404 missing
  records, 422 failed validation. Internal errors return a generic 500 and are logged, so
  database messages never leak to clients.
- **Storage**: PostgreSQL through GORM (being replaced by a faster `database/sql` layer).

## Quick start

```go
package main

import (
	"github.com/zaid223r/Bridgr/core/brcrud"
	"github.com/zaid223r/Bridgr/core/brdb"
	"github.com/zaid223r/Bridgr/core/brhttp"
)

type Todo struct {
	ID    uint   `json:"id" gorm:"primaryKey"`
	Title string `json:"title"`
	Done  bool   `json:"done"`
}

// GenericFiltering makes every Todo field filterable.
func (Todo) GenericFiltering() bool { return true }

func main() {
	db, err := brdb.ConnectPostgres("user", "password", "localhost", "bridgr", 5432)
	if err != nil {
		panic(err)
	}
	if err := db.AutoMigrate(&Todo{}); err != nil {
		panic(err)
	}

	router := brhttp.NewRouter()
	brcrud.RegisterCRUD[Todo](router, "todos", db, nil)
	brhttp.StartServer(router, "8080")
}
```

Then try:

- `GET /todos?title=milk&done=false`
- `GET /docs` for Swagger UI, `GET /openapi.json` for the spec

A runnable version lives in [examples/todoexample](examples/todoexample).

## Known limitations (being fixed, see the roadmap)

- `PUT` ignores zero values, so `{"done": false}` doesn't clear a field.
- Invalid `FilterableFields` are only detected when `/openapi.json` is first requested.
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
