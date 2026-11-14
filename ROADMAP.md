# Bridgr roadmap

One feature per branch, one branch per PR. Tell Claude **"next"** and it picks up the first
unchecked feature below. Sub-task boxes are ticked on the feature branch as the work lands;
the feature's own box is ticked in the branch's last commit, so it reaches `main` together with
the work when the PR is squash-merged. When to merge,
park or delete a branch is defined in [CLAUDE.md § Branch lifecycle](CLAUDE.md#branch-lifecycle).

---

## Phase 1: Solid core

- [x] **`chore/project-foundations`**: groundwork for everything else
  - [x] CLAUDE.md (working agreement and style)
  - [x] ROADMAP.md (this file)
  - [x] GitHub Actions CI: build, vet, race tests, golangci-lint on Go 1.26 + 1.27
  - [x] `Makefile` with `check`, `test`, `integration`, `bench`, `cover`
  - [x] README: correct import paths, honest feature list, roadmap link
  - [x] `go.mod` targets Go 1.26
- [x] **`feat/stdlib-router`**: replace the hand-written router with `net/http.ServeMux`
  - [x] `GET /todos/{id}` patterns, `r.PathValue("id")`
  - [x] correct `405 Method Not Allowed` and trailing-slash behaviour
  - [x] `httptest` tests for every CRUD route
- [x] **`feat/problem-errors`**: consistent errors
  - [x] rename `errors` package (it shadows the stdlib)
  - [x] RFC 9457 `application/problem+json` responses
  - [x] correct codes: 401 vs 403, 404 only for "not found", 500 otherwise, 204 with empty body
  - [x] tests for each error path
- [x] **`feat/model-schema`**: reflect once, cache forever
  - [x] `schema` package: JSON name, DB column, Go kind, primary key, `omitempty`, `json:"-"`
  - [x] embedded structs flattened
  - [x] cached per type, no reflection on requests
  - [x] tests + benchmark
- [x] **`feat/fail-fast-registration`**: invalid models fail at startup, not at request time
  - [x] validate `FilterableFields` during registration
  - [x] remove global `GenericFilteringEnabled` (unsafe: lets any column name reach SQL)
  - [x] tests
- [x] **`feat/app-lifecycle`**: an `App` type that's ready for production
  - [x] server timeouts, graceful shutdown on SIGINT/SIGTERM
  - [x] `log/slog` request logging, request-body size limit
  - [x] request context passed through to storage
  - [x] tests

## Phase 2: Storage (SQL first)

- [x] **`feat/store-interface`**: one interface for every backend
  - [x] `Store[T]` with `Query` (filters, sort, page)
  - [x] in-memory store
  - [x] reusable **store contract test suite** that every adapter must pass
- [x] **`feat/sqlstore-sqlite`**: `database/sql` adapter, SQLite dialect
  - [x] precomputed SQL per model, placeholders per dialect
  - [x] passes the contract suite
Reordered: Postgres and the GORM removal come first, so the PUT/PATCH fix, filter
operators and pagination are built once, on the Store path only, instead of also for GORM.

- [x] **`feat/sqlstore-postgres`**: Postgres dialect (any `database/sql` driver, tested with `pgx/v5/stdlib`)
  - [x] `$1` placeholders, `RETURNING`, `ILIKE`, identity keys kept in sync after explicit ids
  - [x] integration tests (`compose.yaml` locally, service container in CI)
- [x] **`chore/remove-gorm`**: drop GORM completely
  - [x] CRUD routes take a `brstore.Store`; remove `BridgrModel`, `brorm`, `brdb`
  - [x] `db:"column,pk"` struct tags (gorm tags still read for migration)
  - [x] migrate example and README
  - [x] before/after benchmark numbers
- [x] **`fix/put-patch-semantics`**: `PUT` replaces, `PATCH` merges
  - [x] fixes the bug where `{"done": false}` is silently ignored
  - [x] tests for zero values
- [x] **`feat/filter-operators`**: richer, safe filtering
  - [x] `eq, ne, gt, gte, lt, lte, contains, in` (`?age[gte]=18`)
  - [x] escape `LIKE` wildcards, columns only from the whitelist
  - [x] tests incl. injection attempts
- [x] **`feat/pagination-sorting`**
  - [x] `limit`/`offset` with default + max, `sort=-created_at,title`
  - [x] total count, `Link` header
  - [x] tests
- [x] **`feat/sqlstore-mysql`**: MySQL dialect + integration tests

## Phase 3: OpenAPI that generates real clients

- [x] **`feat/openapi-schemas`**: OpenAPI 3.1 with `components`, request + response schemas, error schemas
  - [x] deterministic output, golden-file tests
- [x] **`test/openapi-validation`**: validate the generated spec in tests (`pb33f/libopenapi-validator`, which supports 3.1)
  - [x] the document passes the OpenAPI 3.1 schema
  - [x] real requests and responses for every documented status match the spec
- [x] **`feat/docs-ui`**: replace Swagger UI with [Scalar](https://github.com/scalar/scalar) at `/docs`
  - [x] `@scalar/api-reference` standalone bundle, version-pinned and embedded with `go:embed` (no CDN at runtime)
  - [x] options: page title, theme, dark mode, and an opt-in to load the pinned bundle from a CDN instead (smaller binary)
  - [x] remove the Swagger UI handler; `/openapi.json` unchanged
  - [x] tests: `/docs` serves the page with the right spec URL, assets are cached and gzip-served, `DisableDocs` still works
- [x] **`example/typescript-client`**: generate a typed TS client (`openapi-typescript`, Bun) and use it from a React + Vite app
  - [x] `bun run gen` writes the spec from the Go code and regenerates the types; CI fails if they are stale
  - [x] the app uses typed list filters and paging, POST with `TodoInput`, PATCH, DELETE and typed `Problem` errors

## Phase 4: Framework core

The core stays small: struct → endpoints, storage, OpenAPI, and the hooks modules need to
plug in. Modules (Phase 9) are built only on these public hooks.

- [x] **`feat/app`**: one entry point, `bridgr.New(cfg)` → `bridgr.CRUD[T](app, "todos", opts)` → `app.Run()`
  - [x] root package `bridgr` wraps router, server and docs; existing packages keep working
- [x] **`feat/modules`**: the module contract every add-on uses
  - [x] `Module` interface: register routes, middleware and OpenAPI additions; `Start`/`Stop` for background work
  - [x] `app.Use(...)`, ordered start and reverse-order graceful stop
  - [x] `moduletest`: a contract suite every module repo runs (registers cleanly, starts/stops within the timeout, its OpenAPI additions are valid, works next to other modules)
- [x] **`feat/capabilities`**: shared interfaces so modules cooperate without importing each other
  - [x] capability registry: `bridgr.Provide[C]` / `bridgr.Lookup[C]` by interface type
  - [x] `core/brcap`: caller identity in the request context, key-value store with TTL and counters, scheduler with named handlers and retries
  - [x] field-level validation errors (`brerrors.ValidationError` → 422 with `errors`)
  - [x] in-memory implementations (`MemoryKV`, `MemoryScheduler`) for tests and fallbacks
  - resource events moved to `feat/hooks`, where the CRUD writes that publish them live
- [x] **`feat/middleware`**: global + per-resource middleware; built-ins: recover, request ID, CORS
  - [x] request IDs (kept from `X-Request-ID` or generated) in the response, every log line and problem bodies
  - [x] panics become logged 500s, for handlers and middleware alike
  - [x] `Config.Middleware` / `app.AddMiddleware`, and per-resource `Options.Middleware`, all `func(http.Handler) http.Handler`
  - [x] `brmw.CORS` with preflight handling; refuses credentials with `*`
- [x] **`feat/hooks`**: per-resource `BeforeCreate`, `AfterUpdate`, … for business rules, and resource events (created/updated/deleted) published to an event bus capability
  - [x] before hooks can change the record or reject the write (mapped like any error); after hooks run only after a successful write
  - [x] `brcap.EventBus` + `MemoryEventBus`; `bridgr.CRUD` publishes to the app's bus, looked up per event
- [x] **`feat/typed-handlers`**: `bridgr.Handle[In, Out]` for custom endpoints, documented in OpenAPI automatically
  - [x] `path`, `query`, `header` tags parsed with typed 400s; the rest is the JSON body; `Validate()` on the input gives 422s
  - [x] `NoContent` → 204; options for operationId, summary, tags, status, documented errors, middleware
  - [x] wildcard/field mismatches, unsupported types and GET bodies panic at startup

## Phase 5: Developer experience

The target: register the database once, keep one file per table (the model plus optional
request and response types), and wire each table in `main` with `bridgr.CRUD` and its options.

```go
// posts.go
type Post struct {
	ID        int64     `json:"id" bridgr:"readonly"`
	UserID    int64     `json:"user_id" bridgr:"ref=users"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at" bridgr:"readonly"`
	Author    *User     `json:"author,omitempty" bridgr:"include=user_id"`
}

// main.go
app := bridgr.New(bridgr.Config{DB: bridgr.SQL(db, sqlstore.Postgres)})
bridgr.CRUD[User](app, "users", nil)
bridgr.CRUD[Post](app, "posts", &bridgr.CRUDOptions[Post]{
	Filters: bridgr.Filters{"user_id": bridgr.Exact, "title": bridgr.Exact | bridgr.Contains},
	Sort:    []string{"created_at", "title"},
	NestedUnder: "users", // also GET/POST /users/{id}/posts
})
```

Decisions: one app-level database (a per-resource store can still override it); request and
response shapes come from struct tags for the common cases and from typed structs with mapping
functions for anything custom; nothing is filterable or sortable until listed; relations are
declared on the model, with no ORM and no lazy loading; anything that writes several records
runs in a transaction. Relations and includes use only `brstore.Store` (an `in` filter), so
they work on every backend, NoSQL included.

- [x] **`feat/app-db`**: register storage once
  - [x] `Config.DB` with `bridgr.SQL(db, dialect)` (SQLite, Postgres, MySQL) and `bridgr.Memory()`; the app opens no connections itself, so drivers stay the caller's choice
  - [x] `bridgr.CRUD[T](app, path, opts)` builds the store from the app's database; `Options.Store` overrides it
  - [x] opt-in table creation at startup for development (`Config.CreateTables`, `app.CreateTables`)
- [x] **`feat/field-tags`**: shape requests and responses with tags
  - [x] `bridgr:"readonly"` (ignored on input, e.g. ids and timestamps), `bridgr:"writeonly"` (accepted, never returned, e.g. passwords)
  - [x] reflected in the OpenAPI `<Model>` and `<Model>Input` schemas (`readOnly` / `writeOnly`)
  - [x] writeonly fields also hidden from typed-handler responses and events; nested ones are rejected at startup
- [x] **`feat/filter-config`**: per-field filter and sort control, deny by default
  - [x] `Options.Filters` maps a field to its allowed operators (`Exact`, `Contains`, `Range`, `In`, ...); `Options.Sort` lists sortable fields
  - [x] other fields and operators get a 400; replaces `FilterableFields` and `GenericFiltering`
  - [x] OpenAPI documents only the allowed parameters and operators
- [ ] **`feat/typed-io`**: custom request and response types per resource
  - [ ] `Options.Create`, `Options.Update` (`bridgr.Input(func(In) (T, error))`) and `Options.Response` (`bridgr.Output(func(T) Out)`)
  - [ ] OpenAPI uses the custom types; the TypeScript example shows one
- [ ] **`feat/transactions`**: several writes, all or nothing
  - [ ] `bridgr.Tx(ctx, app, fn)`; every store call inside `fn` joins the transaction through the context
  - [ ] CRUD writes and their hooks run in one transaction, so a failing after-hook rolls back
  - [ ] store contract suite covers commit and rollback
- [ ] **`feat/relations`**: references between models
  - [ ] `bridgr:"ref=<resource>"` checks the referenced record exists on write (422, not a database error)
  - [ ] deleting a referenced record is refused (409) unless the child sets `OnDelete: bridgr.Cascade`, which deletes in the same transaction
  - [ ] relations shown in OpenAPI
- [ ] **`feat/nested-routes`**: `Options.NestedUnder` mounts `GET`/`POST /<parent>/{id}/<children>` with the reference filled in
- [ ] **`feat/include`**: `?include=author` expands relations with one extra query per relation (no N+1); only declared relations, documented in OpenAPI
- [ ] **`feat/batch-create`**: opt-in `POST /<resource>/batch`, all or nothing, with per-item 422 errors
- [ ] **`example/project-layout`**: users, posts and orders, one file per table, using everything above, including a multi-create order endpoint built with `bridgr.Handle` and `bridgr.Tx`

## Phase 6: Speed

- [ ] **`perf/benchmarks`**: reproducible suite vs plain `net/http`, Gin, Echo, Fiber, Huma (separate module)
- [ ] **`perf/hot-path`**: cut allocations: pooled buffers, prepared statements, precomputed responses
- [ ] **`feat/json-codecs`**: codec interface, optional faster JSON codec as a separate module

## Phase 7: NoSQL

- [ ] **`feat/mongostore`**: MongoDB adapter as a separate module, passes the contract suite (transactions included, on a replica set); opens `Config.DB` to backends outside the core
- [ ] **`feat/mongo-filters`**: filter operators, pagination and ObjectID support on MongoDB

## Phase 8: Core release

- [ ] **`docs/guides`**: README rewrite, runnable godoc `Example` tests, pkg.go.dev polish
- [ ] **`release/v0.1.0`**: CHANGELOG, tag, GitHub release

## Phase 9: Modules (after the core release)

Each module is its **own repository** (`github.com/zaid223r/bridgr-<name>`) with its own
`go.mod`, CI and releases. They stay fully compatible with each other because:

- they import **only the core**, never each other; they cooperate through the core's
  capability interfaces (identity, scheduler, key-value store, events, validation errors)
- every module runs the core's **`moduletest`** contract suite in its CI, against the
  latest core release and core `main`
- **versions line up**: the module API only changes in a core major version, and every
  module's major version matches the core's (bridgr v1 ↔ bridgr-auth v1.x, …)
- a **`bridgr-compat`** repository builds one app with every module enabled and runs it on
  each core or module release, so incompatibilities show up before users see them

Setup comes first: **`bridgr-module-template`** (repo template: go.mod, CI with
`moduletest`, lint, CLAUDE.md conventions) and **`bridgr-compat`**. Creating the repos
needs the user's okay. Module order is decided when the core ships.

- [ ] **`bridgr-validate`**: struct-tag validation (`validate:"required,max=100"`), 422 with field errors, rules reflected in the OpenAPI schema
- [ ] **`bridgr-auth`**: API keys and JWT bearer tokens, per-operation authorization, OpenAPI security schemes
- [ ] **`bridgr-jobs`**: scheduled jobs (cron and intervals), retries, graceful stop, one-runner locking across instances
- [ ] **`bridgr-ratelimit`**: per-client and per-route limits, 429 with `Retry-After`; in-memory and Redis
- [ ] **`bridgr-cache`**: response caching with `ETag` / `If-None-Match`; in-memory and Redis
- [ ] **`bridgr-events`**: events on create/update/delete; signed webhooks with retries (delivered through the core scheduler capability, so `bridgr-jobs` is used when present)
- [ ] **`bridgr-uploads`**: multipart uploads to disk or S3-compatible storage, linked to records

## Later (not scheduled)

- CLI: `bridgr new`, project scaffolding
- Migrations, soft delete, multi-tenancy, many-to-many relations
