//go:build cgo

package dberror

import (
	"testing"

	sqlite3 "github.com/mattn/go-sqlite3"
)

func TestIsUniqueConstraintSQLite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "sqlite unique code", err: sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintUnique}, want: true},
		{name: "sqlite primary key code", err: sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintPrimaryKey}, want: true},
		{name: "sqlite rowid code", err: sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintRowID}, want: true},
		{name: "sqlite check code", err: sqlite3.Error{Code: sqlite3.ErrConstraint, ExtendedCode: sqlite3.ErrConstraintCheck}, want: false},
		{name: "sqlite unspecified constraint", err: sqlite3.Error{Code: sqlite3.ErrConstraint}, want: false},
		{name: "sqlite non-constraint code", err: sqlite3.Error{Code: sqlite3.ErrBusy}, want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := IsUniqueConstraint(tt.err); got != tt.want {
				t.Fatalf("IsUniqueConstraint() = %v, want %v", got, tt.want)
			}
		})
	}
}
