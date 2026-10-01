# Conventions

This is the house doctrine: how applications built with `canguro-rails` are
written. It is deliberately short, because a convention you have to look up is not
a convention.

> **Golden rule: a small change touches few files, in predictable places.** If
> adding a field means touching ten files, the design is wrong.

---

## 1. Where things live

| What | Where | Rule |
|---|---|---|
| **Routes** | `internal/routes/routes.go` | **All** routes in one file. It is our `routes.rb`. |
| **Models** | `internal/models/` | The **only** layer that talks SQL. SQL anywhere else is a bug. |
| **Services** | `internal/services/` | Business rules. Takes models, returns results/errors. Knows nothing about HTTP. |
| **Controllers** | `internal/controllers/` | Thin: read the request, call a service, pick the view. **No business logic.** |
| **Views** | `internal/views/` | templ. **No logic**: they paint data, formatting lives in helpers. |
| **Middleware** | `internal/middleware/` | Session, CSRF, auth, logging. |
| **Background work** | `internal/jobs/` | Enqueue here. Bounded pool, never a bare `go func()`. |
| **Migrations** | `migrations/` | Numbered, immutable once applied. `make migrate`. |
| **Config** | `internal/config/` | Read once, validated at boot, injected. No stray `os.Getenv`. |
| **Static assets** | `web/static/` | CSS and JS. The design system lives in `css/design-system.css`. |

## 2. The seven rules

1. **Routes are declared in one place.** Never register a route from a controller or an `init()`.
2. **Only models touch the database.** Services ask models; controllers never see a `*sqlx.DB`.
3. **Controllers are thin.** A business `if` belongs in a service. Target: a controller reads in 15 lines.
4. **Views don't think.** No queries, no rules, no complex formatting.
5. **Configuration is validated at startup.** A missing variable stops the process, not a request.
6. **Errors are returned, not hidden.** Explicit `error` end to end; `panic` is for programmer bugs and the middleware turns it into a 500 with a stack.
7. **Anything done often lives in the Makefile.** Nobody memorises commands.

## 3. Adding a screen (the recipe)

A typical change touches **six files, always the same six**:

1. **Migration** — `migrations/000N_whatever.sql` (`make migrate`)
2. **Model** — `internal/models/whatever.go` (struct + queries)
3. **Service** — `internal/services/whatever.go` (rules + tests)
4. **View** — `internal/views/pages/whatever.templ` (`make templ`)
5. **Controller** — `internal/controllers/whatever.go`
6. **Route** — one line in `internal/routes/routes.go`

Plus the service and controller tests. If you skipped a step, the coverage gate or
the review will say so.

## 4. htmx without inventing a second app

- A handler is written **once**. `web.Render` decides: htmx (`HX-Request`) gets the
  fragment, everyone else gets the full page. Do not duplicate handlers.
- Validation failures return **422 with the form re-rendered** (`RenderInvalid`).
  No hand-written JavaScript validators.
- Redirects go through `Renderer.Redirect`: a 302 would be silently followed by
  `fetch()` and the whole page would land inside a fragment's place.
- htmx attributes live in `web/components`, not scattered across templates.

## 5. Tests

- Every service and every middleware has tests: that is where the logic is.
- Controllers are tested through the real server (`httptest`) and the test database.
- Models are tested against a real database, not a mock: same engine as production.
- **Minimum coverage 80%** (`make cover` fails below). It goes up, never down.
- **A fixed bug comes with its test.** No test, not fixed.
- **A skip must be visible.** `testsupport.DB` skips when no database is available —
  unless `REQUIRE_DB=1` (what `make test` and CI set), in which case a missing
  database **fails**. "Everything passed" must never mean "nobody ran the tests".

## 6. Things we don't do

- `os.Getenv` outside `internal/config`.
- SQL outside `internal/models`.
- Bare `go func()` for background work: use `jobs`.
- Migrations that run when the app starts (a broken migration would take the
  service down and the health check would lie).
- Routes registered outside `internal/routes`.
- Business logic in views.
- **Copying infrastructure into an app.** If it is not app-specific, it belongs in
  this toolkit. A piece moves in here only when a **second** project needs it, and
  only after it already worked in the first one — no speculative abstractions.
