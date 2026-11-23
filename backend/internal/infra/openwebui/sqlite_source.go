//go:build sqlite

package openwebui

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openSQLite(dsn string) (*gorm.DB, error) {
	return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
}
