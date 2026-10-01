package dbx

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
)

// The four failures the app layer should be able to recognise without importing
// a driver. Anything else is wrapped and returned as-is.
var (
	// ErrNotFound is returned instead of sql.ErrNoRows.
	ErrNotFound = errors.New("record not found")
	// ErrDuplicate is a unique/primary key violation (MySQL 1062).
	ErrDuplicate = errors.New("duplicate record")
	// ErrNoTable is a missing table (MySQL 1146). It usually means "migrations
	// were not run" or "this optional table does not exist here" — the sync
	// tools need to tell those two apart from a real failure.
	ErrNoTable = errors.New("table does not exist")
	// ErrDeadlock is a transaction rolled back by the server (MySQL 1213).
	ErrDeadlock = errors.New("transaction deadlock")
)

// MySQL error numbers we care about.
const (
	mysqlDuplicateEntry  = 1062
	mysqlNoSuchTable     = 1146
	mysqlDeadlock        = 1213
	mysqlLockWaitTimeout = 1205
)

// MapError translates driver errors into the sentinels above, keeping the
// original wrapped so nothing is lost:
//
//	if errors.Is(err, dbx.ErrDuplicate) { ... }
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	}

	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case mysqlDuplicateEntry:
			return fmt.Errorf("%w: %v", ErrDuplicate, err)
		case mysqlNoSuchTable:
			return fmt.Errorf("%w: %v", ErrNoTable, err)
		case mysqlDeadlock, mysqlLockWaitTimeout:
			return fmt.Errorf("%w: %v", ErrDeadlock, err)
		}
	}
	return err
}

// IsDuplicate, IsNotFound and IsNoTable read better at call sites than
// errors.Is chains, and they document intent.
func IsDuplicate(err error) bool { return errors.Is(err, ErrDuplicate) }
func IsNotFound(err error) bool  { return errors.Is(err, ErrNotFound) }
func IsNoTable(err error) bool   { return errors.Is(err, ErrNoTable) }
func IsDeadlock(err error) bool  { return errors.Is(err, ErrDeadlock) }
