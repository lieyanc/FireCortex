//go:build cgo

package dberror

import (
	"errors"

	sqlite3 "github.com/mattn/go-sqlite3"
)

func isSQLiteUniqueConstraint(err error) bool {
	var sqliteErr sqlite3.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code != sqlite3.ErrConstraint {
		return false
	}
	switch sqliteErr.ExtendedCode {
	case sqlite3.ErrConstraintUnique, sqlite3.ErrConstraintPrimaryKey, sqlite3.ErrConstraintRowID:
		return true
	default:
		return false
	}
}
