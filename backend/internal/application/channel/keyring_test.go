package channel

import (
	"testing"

	"github.com/lieyanc/FireCortex/backend/internal/pkg/secretbox"
)

func testKeyring(t *testing.T, key string) *secretbox.Keyring {
	t.Helper()
	keyring, err := secretbox.NewKeyring(key)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return keyring
}
