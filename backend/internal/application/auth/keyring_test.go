package auth

import (
	"testing"

	"github.com/lieyanc/FireCortex/backend/internal/pkg/secretbox"
)

func sealForTest(t *testing.T, key string, value string) (string, error) {
	t.Helper()
	keyring, err := secretbox.NewKeyring(key)
	if err != nil {
		return "", err
	}
	return keyring.EncryptString(value)
}
