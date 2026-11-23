package secretrotation

import (
	"context"
	"errors"
)

// VerifyStoredKeys reads encrypted values without rewriting them. It runs before
// setup commits the new configuration or creates an administrator on an adopted DB.
func (s *Service) VerifyStoredKeys(ctx context.Context) error {
	wrongKey := errors.New("existing database secrets cannot be decrypted; restore the original data_encryption_key and any previous keys")
	for _, field := range secretFields {
		var cursor uint
		for {
			page, err := s.repo.ListSecrets(ctx, field, cursor, batchSize)
			if err != nil {
				return err
			}
			for _, item := range page {
				if bind := secretBindings[field]; bind != nil {
					_, err = s.keyring.DecryptStringBound(item.Value, bind(item))
				} else {
					_, err = s.keyring.Decrypt(item.Value)
				}
				if err != nil {
					return wrongKey
				}
				cursor = item.ID
			}
			if len(page) < batchSize {
				break
			}
		}
	}
	settings, err := s.repo.ListSettingSecrets(ctx, s.settingKeys)
	if err != nil {
		return err
	}
	for _, item := range settings {
		if _, err := s.keyring.Decrypt(item.Value); err != nil {
			return wrongKey
		}
	}
	var cursor uint
	for {
		page, err := s.repo.ListRedemptionCodes(ctx, cursor, batchSize)
		if err != nil {
			return err
		}
		for _, item := range page {
			if _, err := s.keyring.Decrypt(item.CodeEncrypted); err != nil {
				return wrongKey
			}
			cursor = item.ID
		}
		if len(page) < batchSize {
			return nil
		}
	}
}
