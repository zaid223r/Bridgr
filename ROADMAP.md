# Bridgr roadmap

One feature per day, one branch per feature. Tell Claude **"next"** and it picks up the first
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
plug in. Modules (Phase 8) are built only on these public hooks.

- [x] **`feat/app`**: one entry point, `bridgr.New(cfg)` → `bridgr.CRUD[T](app, "todos", store, opts)` → `app.Run()`
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
- [ ] **`feat/typed-handlers`**: `bridgr.Handle[In, Out]` for custom endpoints, documented in OpenAPI automatically

## Phase 5: Speed

- [ ] **`perf/benchmarks`**: reproducible suite vs plain `net/http`, Gin, Echo, Fiber, Huma (separate module)
- [ ] **`perf/hot-path`**: cut allocations: pooled buffers, prepared statements, precomputed responses
- [ ] **`feat/json-codecs`**: codec interface, optional faster JSON codec as a separate module

## Phase 6: NoSQL

- [ ] **`feat/mongostore`**: MongoDB adapter as a separate module, passes the contract suite
- [ ] **`feat/mongo-filters`**: filter operators, pagination and ObjectID support on MongoDB

## Phase 7: Core release

- [ ] **`docs/guides`**: README rewrite, runnable godoc `Example` tests, pkg.go.dev polish
- [ ] **`release/v0.1.0`**: CHANGELOG, tag, GitHub release

## Phase 8: Modules (after the core release)

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
- Migrations, relations/preloading, soft delete, multi-tenancy
