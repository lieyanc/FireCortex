//go:build sqlite

package persistence

import (
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	sqlitedb "github.com/lieyanc/FireCortex/backend/internal/infra/persistence/sqlite"
	"gorm.io/gorm"
)

func openSQLite(cfg config.Config) (*gorm.DB, error) {
	return sqlitedb.New(cfg)
}
