//go:build !sqlite

package openwebui

import (
	"errors"
	"testing"

	"github.com/lieyanc/FireCortex/backend/internal/repository"
)

// 不含 SQLite 驱动的构建只支持从 PostgreSQL 导入，SQLite 来源按无效输入拒绝。
func TestOpenDBRejectsSQLiteWithoutDriver(t *testing.T) {
	t.Parallel()

	for _, dsn := range []string{"sqlite:///tmp/webui.db", "/tmp/webui.db"} {
		if _, err := openDB(dsn); !errors.Is(err, repository.ErrInvalidInput) {
			t.Fatalf("openDB(%q) expected ErrInvalidInput, got %v", dsn, err)
		}
	}
}
