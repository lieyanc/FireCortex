//go:build !sqlite

package persistence

import (
	"errors"
	"testing"

	"github.com/lieyanc/FireCortex/backend/internal/infra/config"
)

func TestOpenRejectsSQLiteWithoutDriver(t *testing.T) {
	t.Parallel()

	_, err := Open(config.Config{DatabaseDriver: "sqlite"})
	if !errors.Is(err, ErrSQLiteUnavailable) {
		t.Fatalf("expected ErrSQLiteUnavailable, got %v", err)
	}
}
