# Bridgr: working agreement for Claude

Bridgr turns Go structs into fast, fully documented REST APIs. The goal is to be the
quickest way to ship an API in Go: fast to write (one line per resource) and fast at runtime
(no reflection on the hot path, no heavy ORM).

## How we work

- **The plan lives in [ROADMAP.md](ROADMAP.md).** When the user says "next", "continue" or
  "daily goal": open ROADMAP.md, take the **first unchecked feature**, and do only that one.
- **One feature = one branch = one PR.** Branch names describe the feature, never the day:
  `feat/stdlib-router`, `fix/patch-zero-values`, `perf/hot-path`, `chore/...`, `docs/...`,
  `test/...`. Branch from an up-to-date `main`.
- **Small steps.** A feature should be reviewable in one sitting (aim for under ~400 changed
  lines excluding tests and golden files). If it grows bigger, split it in ROADMAP.md and stop
  at the first half.
- **Finish the loop.** A feature is done when: code + tests are in, `make check` passes, the
  feature's boxes in ROADMAP.md are ticked, and README/godoc are updated if the public API
  changed.
- **Ask before outward-facing actions:** pushing, opening PRs, merging, deleting remote
  branches, tagging releases, editing the GitHub repo description. Committing on the feature
  branch is fine once the user okays it.

## Branch lifecycle

Every feature goes through the same steps. `main` only changes through merged PRs.

1. **Start.** `git switch main && git pull --ff-only`, then `git switch -c <type>/<feature>`.
   Only one feature branch is in progress at a time; finish or park it before starting another.
2. **Work.** Small commits with Conventional Commit messages (`feat(router): ...`,
   `fix(store): ...`, `test: ...`). Commits are SSH-signed automatically; never bypass that.
3. **Ready for review** when all of these hold:
   - `make check` passes locally
   - the feature's boxes in ROADMAP.md are ticked in the same branch
   - README/godoc are updated if the public API changed
   - the branch is up to date with `main` (rebase onto `origin/main` if `main` moved; resolve
     conflicts locally, never on GitHub)
4. **Open a PR** (after the user okays the push): title = the main commit's subject, body =
   what changed, why, how it was tested, and anything left for later.
5. **Merge** only when **all** of these hold, and only after the user says so:
   - CI is green on the PR
   - nothing in the PR is marked TODO/WIP for this feature
   - the user approved the merge in chat

   Use **squash merge** (`gh pr merge --squash --delete-branch`) so `main` has one commit per
   feature. Then `git switch main && git pull --ff-only` and delete the local branch with
   `git branch -d` (lowercase `-d`: it refuses if the work isn't in `main`).
6. **Delete without merging** when the work is abandoned or superseded: say why, get the
   user's okay, then close the PR with a comment and delete the branch. Use `git branch -D`
   only in this case. If any part is worth keeping, move it to a new branch first.
7. **Park** a branch that's blocked (waiting on a decision, or too big and being split): push
   it, note it in ROADMAP.md under the feature as `(parked: <reason>)`, and continue with the
   next feature. Revisit parked branches before starting a new phase.

Housekeeping: at the start of each session, run `git fetch --prune` and check
`git branch -vv`. Report any local branches whose upstream is gone (merged and deleted on
GitHub) and delete them with `git branch -d`. Never force-push `main`; force-push a feature
branch only with `--force-with-lease`, and only after a rebase.

## Code style

- **Doc comments go above declarations, not inside bodies.** Every exported identifier gets a
  godoc comment that starts with its name. Unexported functions get one when the *why* isn't
  obvious.
- **Inline comments only for surprising code:** a workaround, a subtle invariant, a
  performance trick, or something that looks wrong but is intentional. Never narrate what the
  next line does.
- `gofmt`/`goimports` formatting, enforced by `golangci-lint` (config in `.golangci.yml`).
- Errors: wrap with `fmt.Errorf("doing x: %w", err)`, compare with `errors.Is/As`, never
  compare error strings. Return errors from library code; panics only for programmer mistakes
  detected at registration time (bad struct tags, unknown filter fields).
- Accept `context.Context` as the first parameter of anything that does I/O, and pass the
  request context all the way to the database.
- No global mutable state. Configuration lives on an `App`/options value, not package vars.
- Reflection happens **once at registration**, results are cached in plain structs. Request
  handling must not call `reflect` per field per request.
- Keep the root module's dependency list small. Optional integrations (MongoDB, alternative
  JSON codecs, benchmarks against other frameworks) live in **separate Go modules** inside the
  repo (their own `go.mod`) so users don't download what they don't use.

## Tests are mandatory

- Every feature ships with tests. No PR without them.
- Table-driven tests, `t.Run` subtests, `t.Parallel()` where safe. Standard library `testing`
  only; no assertion frameworks.
- HTTP behaviour is tested end to end with `httptest`.
- Storage adapters must pass the shared **store contract test suite**, so SQL and NoSQL
  backends behave identically.
- Unit tests use SQLite (pure Go, `modernc.org/sqlite`) or the in-memory store; Postgres,
  MySQL and MongoDB run as integration tests behind the `integration` build tag. They read
  their DSN from `BRIDGR_<DB>_DSN`; `make integration` starts the databases from
  `compose.yaml`, and CI uses service containers. Without a DSN they skip locally and fail
  in CI. Each test uses its own table and drops it afterwards.
- OpenAPI output is covered by golden files (`testdata/*.golden.json`, update with
  `go test ./... -update`).
- Performance-sensitive code gets a benchmark (`BenchmarkXxx` with `b.ReportAllocs()`).
- `make check` (fmt, vet, lint, race tests) must pass before calling a feature done.

## Libraries

| Concern | Use | Notes |
|---|---|---|
| Routing | `net/http.ServeMux` (method + `{wildcard}` patterns) | no third-party router |
| SQL | `database/sql` + per-dialect SQL builder | Postgres via `jackc/pgx/v5/stdlib`, SQLite via `modernc.org/sqlite`, MySQL via `go-sql-driver/mysql` |
| NoSQL | `go.mongodb.org/mongo-driver/v2` | separate module |
| Logging | `log/slog` | |
| JSON | `encoding/json` behind a codec interface | faster codecs as opt-in modules |
| Spec validation (tests only) | `getkin/kin-openapi` | |
| Integration tests | `compose.yaml` + service containers in CI | no testcontainers: keeps Docker client libraries out of `go.mod` |

No ORM. Storage goes through `brstore.Store`; SQL lives in `sqlstore` dialects. `gorm` struct tags are read only so GORM models migrate unchanged.

## Commands

```bash
make check        # fmt + vet + lint + race tests: run before saying a feature is done
make test         # unit tests
make integration  # integration tests; starts databases from compose.yaml (needs Docker/OrbStack)
make bench        # benchmarks
make cover        # coverage report
```

Go: support the two latest Go releases (currently 1.26 and 1.27), matching Go's own support
policy. Local Postgres for manual runs: the `postgres` container described in the user's
global CLAUDE.md; tests never depend on it.
