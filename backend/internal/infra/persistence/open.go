// Package persistence 负责选择 SQL 驱动。默认构建仅包含 Postgres
// （可通过 -tags nopostgres 排除）；SQLite 需显式使用 -tags sqlite 与
// CGO_ENABLED=1 构建（仅用于桌面端本地模式，服务端部署默认不支持）。
package persistence

import (
	"fmt"
	"strings"

	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
	"gorm.io/gorm"
)

// Open 返回 cfg.DatabaseDriver 对应的连接。
func Open(cfg config.Config) (*gorm.DB, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.DatabaseDriver)) {
	case "", "postgres":
		return openPostgres(cfg)
	case "sqlite":
		return openSQLite(cfg)
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.DatabaseDriver)
	}
}
