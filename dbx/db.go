// Package dbx opens and configures database connections, and maps driver errors
// to a small set of values the rest of the app can reason about.
//
// One pool per process, injected into the model layer. Only models talk SQL —
// that rule is what keeps a schema change from dragging the whole app along.
package dbx

import (
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"

	_ "github.com/go-sql-driver/mysql" // mysql driver
)

// Options are the pool limits. They are explicit because they are a property of
// the SERVER you are talking to, not of the code:
//
//   - A modern MySQL will happily take 25 connections from one app.
//   - An old MariaDB shared with a client's platform will fall over — it has
//     already happened, and "Too many connections" took that platform down.
//
// So the caller states the cap instead of inheriting a library default.
type Options struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// DefaultOptions is a sane pool for a small web app: few simultaneous
// connections, recycled often enough to survive a flaky network.
func DefaultOptions() Options {
	return Options{
		MaxOpenConns:    25,
		MaxIdleConns:    25,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
	}
}

// SmallPool is for talking to a shared or fragile server: at most a handful of
// connections, recycled aggressively. Use it when the database belongs to
// someone else.
func SmallPool(max int) Options {
	if max < 1 {
		max = 1
	}
	return Options{
		MaxOpenConns:    max,
		MaxIdleConns:    max,
		ConnMaxLifetime: 5 * time.Minute,
		ConnMaxIdleTime: 2 * time.Minute,
	}
}

// Connect opens the pool, applies opts (DefaultOptions when omitted) and pings
// once so a bad URL fails here instead of on the first request.
func Connect(dsn string, opts ...func(*Options)) (*sqlx.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("dbx: empty DSN")
	}
	o := DefaultOptions()
	for _, apply := range opts {
		apply(&o)
	}
	if o.MaxIdleConns > o.MaxOpenConns {
		o.MaxIdleConns = o.MaxOpenConns
	}

	db, err := sqlx.Connect("mysql", dsn) // Connect pings
	if err != nil {
		return nil, fmt.Errorf("dbx: connecting: %w", err)
	}
	db.SetMaxOpenConns(o.MaxOpenConns)
	db.SetMaxIdleConns(o.MaxIdleConns)
	db.SetConnMaxLifetime(o.ConnMaxLifetime)
	db.SetConnMaxIdleTime(o.ConnMaxIdleTime)
	return db, nil
}

// WithOptions returns an option setter, which reads better at call sites than
// mutating a struct:
//
//	db, err := dbx.Connect(dsn, dbx.WithOptions(dbx.SmallPool(2)))
func WithOptions(o Options) func(*Options) {
	return func(dst *Options) { *dst = o }
}

// WithTx runs fn inside a transaction and commits or rolls back for you.
// This is the only place BEGIN/COMMIT appears: models just ask for a tx.
func WithTx(db *sqlx.DB, fn func(tx *sqlx.Tx) error) error {
	tx, err := db.Beginx()
	if err != nil {
		return fmt.Errorf("dbx: beginning transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			// Both errors matter: the original cause AND the failed rollback.
			return fmt.Errorf("%w (and rollback failed: %v)", err, rbErr)
		}
		return err
	}
	return tx.Commit()
}

// Stats exposes the pool counters, mostly for tests and health endpoints.
func Stats(db *sqlx.DB) (open, inUse, idle int) {
	s := db.Stats()
	return s.OpenConnections, s.InUse, s.Idle
}
