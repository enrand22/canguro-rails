package testsupport

import (
	"testing"
)

func TestRequiredReadsTheEnvironment(t *testing.T) {
	cases := map[string]bool{
		"":      false,
		"0":     false,
		"false": false,
		"no":    false,
		"1":     true,
		"true":  true,
		"yes":   true,
	}
	for value, want := range cases {
		t.Setenv("REQUIRE_DB", value)
		if got := Required(); got != want {
			t.Errorf("REQUIRE_DB=%q → Required() = %v, want %v", value, got, want)
		}
	}
}

func TestDBReturnsAPoolWhenTheDatabaseIsRequired(t *testing.T) {
	db := DB(t)
	if db == nil {
		t.Fatal("DB returned no pool")
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("the returned pool does not work: %v", err)
	}
}

func TestTableExistsIsHonest(t *testing.T) {
	db := DB(t)

	// The test database always has at least one table once migrations ran; but we
	// do not assume WHICH one. So: create our own probe table.
	Exec(t, db, `CREATE TABLE IF NOT EXISTS testsupport_probe (id INT PRIMARY KEY)`)
	t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE IF EXISTS testsupport_probe`) })

	if !TableExists(t, db, "testsupport_probe") {
		t.Error("TableExists = false for a table that exists")
	}
	if TableExists(t, db, "tabla_que_no_existe_7b1c") {
		t.Error("TableExists = true for a table that does not exist")
	}
}

func TestCleanEmptiesTheTables(t *testing.T) {
	db := DB(t)
	Exec(t, db, `CREATE TABLE IF NOT EXISTS testsupport_clean (id INT PRIMARY KEY)`)
	t.Cleanup(func() { _, _ = db.Exec(`DROP TABLE IF EXISTS testsupport_clean`) })

	Exec(t, db, `INSERT INTO testsupport_clean (id) VALUES (1), (2), (3)`)
	Clean(t, db, "testsupport_clean")

	var count int
	if err := db.Get(&count, `SELECT COUNT(*) FROM testsupport_clean`); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("after Clean there are still %d rows", count)
	}
}

func TestLoggerWritesNothing(t *testing.T) {
	// Just prove it can be used without producing output.
	Logger().Info("esto no debería verse en la salida de los tests")
}
