package store

import (
	"database/sql"
	"errors"
	"strings"

	"modernc.org/sqlite"
)

// MapDBError converts driver-level errors to domain store errors.
// Specifically maps SQLite extended constraint error code 2067 (SQLITE_CONSTRAINT_UNIQUE),
// 19 (SQLITE_CONSTRAINT), 1555 (SQLITE_CONSTRAINT_PRIMARYKEY), and unique constraint messages
// to ErrDuplicateKey, and sql.ErrNoRows to ErrNotFound.
func MapDBError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if IsDuplicateKeyError(err) {
		return ErrDuplicateKey
	}
	return err
}

// IsDuplicateKeyError returns true if the error is or wraps ErrDuplicateKey,
// or matches SQLite unique constraint error code 2067 / constraint violations.
func IsDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDuplicateKey) {
		return true
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		// 2067 is SQLITE_CONSTRAINT_UNIQUE, 19 is SQLITE_CONSTRAINT, 1555 is SQLITE_CONSTRAINT_PRIMARYKEY
		code := sqliteErr.Code()
		if code == 2067 || code == 19 || code == 1555 {
			return true
		}
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: 2067") ||
		strings.Contains(msg, "PRIMARY KEY must be unique")
}
