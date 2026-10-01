package dbx

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

func TestParseRubyDBURL(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "url completa con puerto",
			in:   "mysql2://user1:secreto@10.0.0.1:3306/gpswox_web",
			want: "user1:secreto@tcp(10.0.0.1:3306)/gpswox_web?timeout=10s&readTimeout=30s&parseTime=true&loc=UTC&charset=utf8mb4,utf8",
		},
		{
			name: "sin puerto usa 3306",
			in:   "mysql://root:clave@db.local/app",
			want: "root:clave@tcp(db.local:3306)/app?timeout=10s&readTimeout=30s&parseTime=true&loc=UTC&charset=utf8mb4,utf8",
		},
		{
			name: "sin password",
			in:   "mysql2://deploy@127.0.0.1:3307/app_test",
			want: "deploy@tcp(127.0.0.1:3307)/app_test?timeout=10s&readTimeout=30s&parseTime=true&loc=UTC&charset=utf8mb4,utf8",
		},
		{
			name: "password con caracteres raros percent-encoded",
			in:   "mysql2://user:p%40ss%3Aword@host:3306/db",
			want: "user:p@ss:word@tcp(host:3306)/db?timeout=10s&readTimeout=30s&parseTime=true&loc=UTC&charset=utf8mb4,utf8",
		},
		{name: "esquema desconocido", in: "postgres://u:p@h:5432/db", wantErr: true},
		{name: "vacía", in: "   ", wantErr: true},
		{name: "sin base", in: "mysql2://u:p@host:3306/", wantErr: true},
		{name: "sin usuario", in: "mysql2://host:3306/db", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRubyDBURL(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q, got %q", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRubyDBURL(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("DSN mismatch\n got: %s\nwant: %s", got, tc.want)
			}
		})
	}
}

func TestMapErrorRecognisesDriverErrors(t *testing.T) {
	cases := []struct {
		name string
		in   error
		want error
	}{
		{"sin filas", sql.ErrNoRows, ErrNotFound},
		{"clave duplicada", &mysql.MySQLError{Number: 1062, Message: "Duplicate entry"}, ErrDuplicate},
		{"tabla inexistente", &mysql.MySQLError{Number: 1146, Message: "Table doesn't exist"}, ErrNoTable},
		{"deadlock", &mysql.MySQLError{Number: 1213, Message: "Deadlock found"}, ErrDeadlock},
		{"lock wait timeout", &mysql.MySQLError{Number: 1205, Message: "Lock wait timeout"}, ErrDeadlock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mapped := MapError(tc.in)
			if !errors.Is(mapped, tc.want) {
				t.Errorf("MapError(%v) = %v, want it to wrap %v", tc.in, mapped, tc.want)
			}
		})
	}
}

func TestMapErrorKeepsUnknownErrorsUntouched(t *testing.T) {
	original := errors.New("algo raro")
	if got := MapError(original); !errors.Is(got, original) {
		t.Errorf("MapError should pass unknown errors through, got %v", got)
	}
	if MapError(nil) != nil {
		t.Error("MapError(nil) should be nil")
	}
}

func TestSmallPoolClampsToOne(t *testing.T) {
	o := SmallPool(0)
	if o.MaxOpenConns != 1 {
		t.Errorf("SmallPool(0).MaxOpenConns = %d, want 1", o.MaxOpenConns)
	}
}

func TestConnectRejectsEmptyDSN(t *testing.T) {
	if _, err := Connect(""); err == nil {
		t.Fatal("expected an error for an empty DSN")
	}
}

// testDB opens the test database, or skips. Set TEST_DATABASE_URL to run the
// integration tests (see the Makefile: `make test` does it for you).
func testDB(t *testing.T) *sqlx.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set: run `make db-up` first")
	}
	db, err := Connect(dsn, WithOptions(SmallPool(4)))
	if err != nil {
		t.Skipf("cannot reach the test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestConnectAppliesPoolCaps(t *testing.T) {
	db := testDB(t)
	open, _, _ := Stats(db)
	if open > 4 {
		t.Errorf("open connections = %d, the cap was 4", open)
	}
}

func TestWithTxCommitsAndRollsBack(t *testing.T) {
	db := testDB(t)

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS dbx_tx_probe (
		id INT PRIMARY KEY, note VARCHAR(50)) ENGINE=InnoDB`); err != nil {
		t.Skipf("cannot create the probe table (run `make migrate`): %v", err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE IF EXISTS dbx_tx_probe`) })
	_, _ = db.Exec(`DELETE FROM dbx_tx_probe`)

	// commit path
	if err := WithTx(db, func(tx *sqlx.Tx) error {
		_, err := tx.Exec(`INSERT INTO dbx_tx_probe (id, note) VALUES (1, 'ok')`)
		return err
	}); err != nil {
		t.Fatalf("WithTx (commit): %v", err)
	}
	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM dbx_tx_probe`); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("after commit there should be 1 row, got %d", count)
	}

	// rollback path
	sentinel := errors.New("no me gustó")
	err := WithTx(db, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec(`INSERT INTO dbx_tx_probe (id, note) VALUES (2, 'nope')`); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx should return the original error, got %v", err)
	}
	if err := db.Get(&count, `SELECT COUNT(*) FROM dbx_tx_probe`); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("after rollback there should still be 1 row, got %d", count)
	}
}

func TestMapErrorOnMissingTableEndToEnd(t *testing.T) {
	db := testDB(t)
	var n int
	err := db.Get(&n, `SELECT COUNT(*) FROM tabla_que_no_existe_9f3a`)
	if err == nil {
		t.Fatal("expected an error querying a missing table")
	}
	if !IsNoTable(MapError(err)) {
		t.Errorf("a missing table should map to ErrNoTable, got %v", MapError(err))
	}
	if !strings.Contains(err.Error(), "9f3a") {
		t.Errorf("the original error should survive: %v", err)
	}
}
