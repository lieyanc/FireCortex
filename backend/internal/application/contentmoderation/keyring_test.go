package contentmoderation

import "github.com/lieyanc/FireCortex/backend/internal/pkg/secretbox"

func testKeyring() *secretbox.Keyring {
	keyring, err := secretbox.NewKeyring("test-data-encryption-key")
	if err != nil {
		panic(err)
	}
	return keyring
}
