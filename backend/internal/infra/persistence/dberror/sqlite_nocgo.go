//go:build !cgo

package dberror

func isSQLiteUniqueConstraint(error) bool {
	return false
}
