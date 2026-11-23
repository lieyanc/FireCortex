//go:build !sqlite

package openwebui

import (
	"fmt"

	"github.com/lieyanc/FireCortex/backend/internal/repository"
	"gorm.io/gorm"
)

// openSQLite 在未启用 -tags sqlite 的默认服务端构建中不可用：只支持从 PostgreSQL 导入，
// SQLite 来源按无效输入拒绝。
func openSQLite(string) (*gorm.DB, error) {
	return nil, fmt.Errorf("%w: SQLite import sources are not compiled into this binary", repository.ErrInvalidInput)
}
