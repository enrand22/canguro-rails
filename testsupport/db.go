package testsupport

import (
	"log/slog"
	"os"
	"testing"

	"github.com/jmoiron/sqlx"

	"github.com/enrand22/canguro-rails/dbx"
)

// DB returns a pool connected to TEST_DATABASE_URL, and whether the database is
// actually being exercised.
//
// The behaviour that matters is the difference between two situations that used
// to look the same in a green test run:
//
//   - The database is not available on this machine → skip (nobody should fight
//     the environment to run `go test ./...`).
//   - Someone ASKED for the database (REQUIRE_DB=1, which is what `make test`
//     and CI set) and it is not there → FAIL.
//
// Without that distinction a suite reports success while every database test was
// silently skipped. It happened here: 36 tests skipped, exit code 0.
func DB(t *testing.T) *sqlx.DB {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if Required() {
			t.Fatal("TEST_DATABASE_URL is not set but the test database was required " +
				"(REQUIRE_DB=1): run `make db-up` first, or unset REQUIRE_DB to skip database tests")
		}
		t.Skip("TEST_DATABASE_URL is not set: run `make db-up` to exercise the database")
	}

	db, err := dbx.Connect(dsn, dbx.WithOptions(dbx.SmallPool(4)))
	if err != nil {
		if Required() {
			t.Fatalf("cannot connect to the test database at TEST_DATABASE_URL: %v", err)
		}
		t.Skipf("cannot reach the test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// Required reports whether the environment demands a database.
func Required() bool {
	switch os.Getenv("REQUIRE_DB") {
	case "", "0", "false", "no":
		return false
	default:
		return true
	}
}

// HasTable fails (or skips, when not required) if the table does not exist.
// "42 tests passed" should never mean "nobody ran the migrations".
func HasTable(t *testing.T, db *sqlx.DB, table string) {
	t.Helper()
	if TableExists(t, db, table) {
		return
	}
	msg := "table " + table + " does not exist: run `make migrate` against the test database"
	if Required() {
		t.Fatal(msg)
	}
	t.Skip(msg)
}

// TableExists reports whether the table is there. It is exported (and free of
// test assertions) so the check itself can be tested.
func TableExists(t *testing.T, db *sqlx.DB, table string) bool {
	t.Helper()
	var name string
	err := db.Get(&name, `SELECT table_name FROM information_schema.tables
		WHERE table_schema = DATABASE() AND table_name = ?`, table)
	return err == nil
}

// Exec runs a statement and fails the test if it errors.
func Exec(t *testing.T, db *sqlx.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec failed: %v\nquery: %s", err, query)
	}
}

// Clean deletes the rows of the given tables, and fails loudly instead of
// silently ignoring a table that does not exist yet.
func Clean(t *testing.T, db *sqlx.DB, tables ...string) {
	t.Helper()
	for _, table := range tables {
		HasTable(t, db, table)
		Exec(t, db, "DELETE FROM "+table)
	}
}

// Logger returns a logger that produces no output during tests.
func Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(discard{}, nil))
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
