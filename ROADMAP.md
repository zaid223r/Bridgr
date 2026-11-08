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
- [ ] **`feat/filter-operators`**: richer, safe filtering
  - [ ] `eq, ne, gt, gte, lt, lte, contains, in` (`?age[gte]=18`)
  - [ ] escape `LIKE` wildcards, columns only from the whitelist
  - [ ] tests incl. injection attempts
- [ ] **`feat/pagination-sorting`**
  - [ ] `limit`/`offset` with default + max, `sort=-created_at,title`
  - [ ] total count, `Link` header
  - [ ] tests
- [ ] **`feat/sqlstore-mysql`**: MySQL dialect + integration tests

## Phase 3: OpenAPI that generates real clients

- [ ] **`feat/openapi-schemas`**: OpenAPI 3.1 with `components`, request + response schemas, error schemas
  - [ ] deterministic output, golden-file tests
- [ ] **`test/openapi-validation`**: validate the generated spec with `kin-openapi` in tests
- [ ] **`feat/docs-ui`**: embedded, version-pinned docs UI (no CDN at runtime)
- [ ] **`example/typescript-client`**: generate a typed TS client (`openapi-typescript`, Bun) and use it from a React + Vite app

## Phase 4: Production features

- [ ] **`feat/validation`**: struct-tag validation (`validate:"required,max=100"`), 422 with field errors
- [ ] **`feat/middleware`**: global + per-resource middleware; built-ins: recover, request ID, logging, CORS
- [ ] **`feat/authorization`**: per-operation authorizer (list/get/create/update/delete), 401/403
- [ ] **`feat/hooks`**: `BeforeCreate`, `AfterUpdate`, …
- [ ] **`feat/typed-handlers`**: `bridgr.Handle[In, Out]` for custom endpoints, documented in OpenAPI automatically

## Phase 5: Speed

- [ ] **`perf/benchmarks`**: reproducible suite vs plain `net/http`, Gin, Echo, Fiber, Huma (separate module)
- [ ] **`perf/hot-path`**: cut allocations: pooled buffers, prepared statements, precomputed responses
- [ ] **`feat/json-codecs`**: codec interface, optional faster JSON codec as a separate module

## Phase 6: NoSQL

- [ ] **`feat/mongostore`**: MongoDB adapter as a separate module, passes the contract suite
- [ ] **`feat/mongo-filters`**: filter operators, pagination and ObjectID support on MongoDB

## Phase 7: Release

- [ ] **`docs/guides`**: README rewrite, runnable godoc `Example` tests, pkg.go.dev polish
- [ ] **`release/v0.1.0`**: CHANGELOG, tag, GitHub release

## Later (not scheduled)

- CLI: `bridgr new`, scaffolding, codegen for React components (from the original idea)
- Migrations, relations/preloading, soft delete, multi-tenancy

