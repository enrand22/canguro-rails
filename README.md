# canguro-rails

**Convention over configuration for Go web apps** — the pieces I kept rewriting on every project,
extracted once so the next one starts in minutes instead of days.

Server-rendered by default (**templ + htmx**), boring on purpose (**sqlx + SQL, no ORM**),
explicit about the database (**goose migrations that never run behind your back**), and honest
about background work (**a bounded pool, not loose goroutines**).

> Written for real client work: it runs the portal of a payment client in production.

---

## What's in the box — `v0.3.0`

### The core (no HTTP dependency at all)

| Package | What it gives you |
|---|---|
| `config` | Declarative env loading. You declare your variables; it validates and **fails at boot**, naming what's missing. No `os.Getenv` scattered around. |
| `dbx` | Connection abstractions: pool with **explicit caps** (`SmallPool` for a database that belongs to someone else), `WithTx`, mapped errors (`ErrNotFound`, `ErrDuplicate`, `ErrNoTable`, `ErrDeadlock`), and a parser for legacy `mysql2://` URLs. |
| `logging` | `log/slog` + **size-based rotating writer** (10 × 1 MB by default) that also writes to stdout so journald keeps working. |
| `jobs` | **Bounded** background pool: backpressure instead of unbounded queues, panic isolation, drain-on-shutdown, and `Periodic` tasks that **never overlap themselves**. |
| `cli` | The daemon surface: `--once`, `--dry-run`, SIGTERM/SIGINT handling and an exit code that tells the truth to systemd. |

> A daemon can use the core without dragging Echo, templ and htmx into its binary.
> There is a test enforcing that the promise stays true.

### The web layer

| Package | What it gives you |
|---|---|
| `web` | First-class templ + htmx. `Render` detects `HX-Request` and returns **the fragment or the full page from the same handler**; helpers for the htmx answers that matter (`HXRedirect`, `HXTrigger`, `HXPushURL`, retarget/reswap, out-of-band) and `RenderInvalid`, which returns **422 with the form re-rendered** — server-side validation with zero hand-written JavaScript. |
| `web/components` | `Flash`, `Field`, `Table`, `EmptyState`, `Pagination`, `ConfirmButton`: the htmx attributes live **in one place** instead of being copied into every template. |
| `middleware` | Signed-cookie session (no session table), CSRF double-submit that also reads the header htmx sends, request id, panic recovery with stack, and request logging. |
| `testsupport` | Test database bootstrap that **cannot silently skip**: with `REQUIRE_DB=1` a missing database is a failure, not a skip. |
| assets | A neutral starter design system and htmx 2.0.4 **vendored** (embedded in the binary): no CDN, works offline, no third party deciding what your app runs. |

### Start a new project

```bash
go run github.com/enrand22/canguro-rails/cmd/canguro@latest new rndc_go \
  --module github.com/enrand22/rndc_go --title "RNDC"
```

You get a project that already follows the house conventions: `routes` as the single place where
routes are declared, controllers that stay thin, services that hold the rules, models as the only
layer that talks SQL, an `/healthz` that queries the database (a health check that answers 200
with the database down is worse than none), CSRF on anything that changes state, a `Makefile`, a
Dockerfile, a Kamal deploy file and a CI workflow.

The skeleton is a directory of templates the CLI embeds, with two rules:

- **`[[.Placeholder]]`, not `{{.Placeholder}}`.** Generated files are Go code, and Go is full of
  `{{ }}` composite literals (`[]Item{{ID: 1}}`). With the default delimiters every template would
  have to escape them; with these, nobody thinks about it again.
- **Only `*.tmpl` files are rendered**; anything else is copied byte for byte, and the `.tmpl`
  suffix is dropped. Paths are templated too, which is how `cmd/[[.App]]/main.go` becomes
  `cmd/rndc_go/main.go`.

A test generates a project, compiles it and runs its tests (`CANGURO_KIT_PATH` points it at this
checkout; CI sets it, so the skeleton is verified on every push instead of on the day it is first
used).

> The split is deliberate: the core has no HTTP dependency at all, so a daemon can
> use it without dragging Echo, templ and htmx into its binary. There is a test
> that enforces that.

## Quickstart

```bash
go get github.com/enrand22/canguro-rails@latest
```

```go
import (
    "github.com/enrand22/canguro-rails/config"
    "github.com/enrand22/canguro-rails/dbx"
)

func main() {
    cfg, err := config.Load([]config.Var{
        {Name: "DATABASE_URL", Required: true},
        {Name: "PORT", Default: "8080"},
    })
    if err != nil {
        log.Fatal(err) // fails here, naming the missing variable
    }

    db, err := dbx.Connect(cfg.Get("DATABASE_URL"))
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()
}
```

## The four rules

1. **Only models talk SQL.** One layer for the database, so a schema change doesn't drag the app.
2. **Migrations are explicit.** They never run at boot — if a migration breaks, the health check
   must not lie.
3. **No loose goroutines.** Background work goes through `jobs`, with a bounded queue and a drain.
4. **Boring beats clever.** `sqlx` and hand-written SQL over an ORM; server-rendered HTML over a
   SPA; a text file of conventions over a framework that hides them.

## Testing

```bash
make db-up      # test MySQL container (port 3307)
make test       # the whole suite against a real database
make cover      # coverage report, fails below the threshold
```

Tests that need a database are honest about it. With no database available they skip, so
`go test ./...` works on any machine. But `make test` and CI set `REQUIRE_DB=1`, which turns a
missing database into a **failure** instead of a skip — because "everything passed" must never mean
"nobody ran the database tests". That distinction exists because it happened: a suite reported green
while 36 database tests were silently skipped.

## Status

Early and honest: versioned with semver, app-facing packages only change **additively** inside a
major. Extraction is driven by a simple rule — *a piece moves in here only when a second project
needs it, and only after it already worked in the first one*. Anything that needs a second real
case (code generators, an ORM, DI, roles, i18n, caching) is deliberately **not** here yet.

## License

MIT — see [LICENSE](LICENSE).
