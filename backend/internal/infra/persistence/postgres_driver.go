//go:build !nopostgres

package persistence

import (
	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"github.com/lieyanc/FireCortex/backend/internal/infra/persistence/postgres"
	"gorm.io/gorm"
)

func openPostgres(cfg config.Config) (*gorm.DB, error) {
	return postgres.New(cfg)
}
