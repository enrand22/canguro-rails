package dbx

import (
	"database/sql"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestSentinelPredicates(t *testing.T) {
	// The predicates are the public face of the mapping: consumers write
	// `if dbx.IsDuplicate(err)` and never import a driver.
	if !IsDuplicate(MapError(&mysql.MySQLError{Number: 1062})) {
		t.Error("IsDuplicate should be true for a 1062 error")
	}
	if !IsNotFound(MapError(sql.ErrNoRows)) {
		t.Error("IsNotFound should be true for sql.ErrNoRows")
	}
	if !IsNoTable(MapError(&mysql.MySQLError{Number: 1146})) {
		t.Error("IsNoTable should be true for a 1146 error")
	}
	if !IsDeadlock(MapError(&mysql.MySQLError{Number: 1213})) {
		t.Error("IsDeadlock should be true for a 1213 error")
	}
	if IsDuplicate(sql.ErrNoRows) || IsNotFound(nil) {
		t.Error("predicates must not fire on unrelated errors")
	}
}

func TestWithOptionsReplacesTheWholeSet(t *testing.T) {
	o := DefaultOptions()
	WithOptions(SmallPool(3))(&o)
	if o.MaxOpenConns != 3 {
		t.Errorf("MaxOpenConns = %d, want 3", o.MaxOpenConns)
	}
}

func TestConnectWithAnUnreachableServerFailsFast(t *testing.T) {
	// A wrong port must error here (Connect pings) instead of at the first query,
	// where the failure would be attributed to the query.
	_, err := Connect("user:pw@tcp(127.0.0.1:1)/nope?timeout=1s", WithOptions(SmallPool(1)))
	if err == nil {
		t.Fatal("expected a connection error")
	}
}
