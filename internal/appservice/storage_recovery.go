package appservice

import "github.com/voici5986/otoneigo/internal/domain"

func (s *AppService) GetStorageRecoveryStatus() (domain.StorageRecoveryStatus, error) {
	if s.options.StorageRecovery == nil {
		return domain.StorageRecoveryStatus{}, nil
	}
	v, err := s.options.StorageRecovery()
	return v, storageError(err)
}
