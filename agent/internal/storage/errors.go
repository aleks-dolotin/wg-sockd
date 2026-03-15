package storage

import (
	"errors"

	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// IsConflict reports whether err is a SQLite unique or primary-key constraint
// violation. Both map to an HTTP 409 Conflict in the API layer.
// Using this helper decouples callers from the specific driver package so
// that switching SQLite drivers requires a change in one place only.
func IsConflict(err error) bool {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		code := sqliteErr.Code()
		return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE ||
			code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}

