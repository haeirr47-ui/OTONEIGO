package appservice

import (
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"testing"
)

func TestStorageRecoveryStatusExposesCountsAndSanitizesReadFailure(t *testing.T) {
	s := New(nil, nil)
	if got, err := s.GetStorageRecoveryStatus(); err != nil || got != (domain.StorageRecoveryStatus{}) {
		t.Fatal(got, err)
	}
	want := domain.StorageRecoveryStatus{QuarantinedFavorites: 2, QuarantinedHistory: 3, SettingsRecovered: true}
	s.options.StorageRecovery = func() (domain.StorageRecoveryStatus, error) { return want, nil }
	if got, err := s.GetStorageRecoveryStatus(); err != nil || got != want {
		t.Fatal(got, err)
	}
	s.options.StorageRecovery = func() (domain.StorageRecoveryStatus, error) {
		return domain.StorageRecoveryStatus{}, errors.New("private original metadata")
	}
	if _, err := s.GetStorageRecoveryStatus(); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
		t.Fatal(err)
	}
}
