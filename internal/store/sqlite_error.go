package store

import (
	"errors"
	"fmt"

	sqlite3 "github.com/mattn/go-sqlite3"
)

func isUniqueConstraintError(err error) bool {
	var sqliteErr sqlite3.Error
	if !errors.As(err, &sqliteErr) {
		return false
	}
	return sqliteErr.ExtendedCode == sqlite3.ErrConstraintUnique ||
		sqliteErr.ExtendedCode == sqlite3.ErrConstraintPrimaryKey
}

func wrapSQLiteWriteError(operation string, err error) error {
	if isUniqueConstraintError(err) {
		return fmt.Errorf("%s: %w: %w", operation, ErrConflict, err)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
