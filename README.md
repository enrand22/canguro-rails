# canguro-rails

**Convention over configuration for Go web apps** — the pieces I kept rewriting on every project,
extracted once so the next one starts in minutes instead of days.

Server-rendered by default (**templ + htmx**), boring on purpose (**sqlx + SQL, no ORM**),
explicit about the database (**goose migrations that never run behind your back**), and honest
about background work (**a bounded pool, not loose goroutines**).

> Written for real client work: it runs the portal of a payment client in production.

---

## What's in the box

### Available now — `v0.1.0` (the core)

| Package | What it gives you |
|---|---|
| `config` | Declarative env loading. You declare your variables; it validates and **fails at boot**, naming what's missing. No `os.Getenv` scattered around. |
| `dbx` | Connection abstractions: pool with **explicit caps** (`SmallPool` for a database that belongs to someone else), `WithTx`, mapped errors (`ErrNotFound`, `ErrDuplicate`, `ErrNoTable`, `ErrDeadlock`), and a parser for legacy `mysql2://` URLs. |
| `logging` | `log/slog` + **size-based rotating writer** (10 × 1 MB by default) that also writes to stdout so journald keeps working. |
| `jobs` | **Bounded** background pool: backpressure instead of unbounded queues, panic isolation, drain-on-shutdown, and `Periodic` tasks that **never overlap themselves**. |
| `cli` | The daemon surface: `--once`, `--dry-run`, SIGTERM/SIGINT handling and an exit code that tells the truth to systemd. |

### Coming next — `v0.2.0` (the web layer)

| Package | What it gives you |
|---|---|
| `web` | First-class templ + htmx: `Render` detects `HX-Request` and returns **the fragment or the full page from the same handler**, htmx response helpers (redirect, trigger, out-of-band, 422 re-render) and a component set that writes htmx attributes **in one place**. |
| `middleware` | Session, CSRF, flash, request-id, recovery, request logging. |
| `testsupport` | Test database bootstrap that **cannot silently skip**: a green suite means the database was actually exercised. |

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
make db-up      # dev (3306) + test (3307) MySQL containers
make test       # the whole suite against a real database
make cover      # coverage report, fails below the threshold
```

Tests that need a database **skip** when none is available — but `make test` loads the test
environment for you, so a green run in this repo means the database was exercised.

## Status

Early and honest: versioned with semver, app-facing packages only change **additively** inside a
major. Extraction is driven by a simple rule — *a piece moves in here only when a second project
needs it, and only after it already worked in the first one*. Anything that needs a second real
case (code generators, an ORM, DI, roles, i18n, caching) is deliberately **not** here yet.

## License

MIT — see [LICENSE](LICENSE).
