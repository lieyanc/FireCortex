//go:build !sqlite

package persistence

import (
	"errors"

	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"gorm.io/gorm"
)

// ErrSQLiteUnavailable 在未使用 -tags sqlite 构建时返回。
var ErrSQLiteUnavailable = errors.New("persistence: sqlite driver not compiled into this binary; FireCortex server builds support postgres only (rebuild with -tags sqlite and CGO_ENABLED=1)")

func openSQLite(config.Config) (*gorm.DB, error) {
	return nil, ErrSQLiteUnavailable
}
