# TypeScript client example

A React + Vite app that talks to a Bridgr API through a client generated from the
API's OpenAPI document. Every path, query parameter, request body, response and error
in [`web/src/App.tsx`](web/src/App.tsx) is typed; if the Go struct changes, the app
stops compiling until you regenerate.

```
server/   Go API: a Todo resource on the in-memory store (filters, paging, PATCH, validation)
web/      React app: openapi-typescript for the types, openapi-fetch for the calls
```

## Run it

```bash
go run ./examples/typescript-client/server      # API on :8080 (/docs for the reference)
```

```bash
cd examples/typescript-client/web && bun install && bun run dev   # app on :5173
```

The Vite dev server proxies `/todos`, `/openapi.json` and `/docs` to the API, so there is no
CORS setup.

## Regenerate the client after changing the Go struct

```bash
cd examples/typescript-client/web && bun run gen
```

`gen` runs the Go server with `-spec openapi.json` to write the document (no server
needs to be running), then `openapi-typescript` turns it into `src/api/schema.d.ts`. CI
does the same and fails if the committed files are out of date, then type-checks and
builds the app.

## What the app shows

- `api.GET("/todos", { params: { query: { limit, offset, sort, title, done } } })`, with
  the total from the `X-Total-Count` header
- `api.POST("/todos", { body: { title, priority } })`: the body type is `NewTodo`, a
  custom create type on the Go side (`CRUDOptions.Create: bridgr.Input(...)`), so there's
  no `id` or `done` to fill in
- `api.PATCH("/todos/{id}", ...)` sends only `done` when you tick a box
- server errors arrive as typed `Problem` objects (try adding an empty title: 422)
