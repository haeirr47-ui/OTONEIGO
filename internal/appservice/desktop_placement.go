package appservice

import (
	"errors"

	"github.com/voici5986/otoneigo/internal/domain"
)

// SaveDesktopPlacement merges native geometry with the latest committed settings.
// It shares the settings lock with ordinary UI changes to prevent lost updates.
func (s *AppService) SaveDesktopPlacement(kind string, placement domain.WindowPlacement) error {
	err := s.commitDesktopPlacement(kind, placement)
	if err == nil {
		s.notifySettingsChanged()
	}
	return err
}
func (s *AppService) commitDesktopPlacement(kind string, placement domain.WindowPlacement) error {
	if kind != "mini" && kind != "lyrics" {
		return errors.New("INVALID_DESKTOP_VIEW")
	}
	if err := domain.ValidateWindowPlacement(placement); err != nil {
		return err
	}
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.options.Library == nil {
		return errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	settings, err := s.options.Library.GetSettings()
	if err != nil {
		return storageError(err)
	}
	settings.Desktop = domain.NormalizeDesktopSettings(settings.Desktop)
	if kind == "mini" {
		settings.Desktop.MiniPlacement = placement
	} else {
		settings.Desktop.LyricsPlacement = placement
	}
	if err = domain.ValidateSettings(settings); err != nil {
		return err
	}
	if err = s.options.Library.SetSettings(settings); err != nil {
		return storageError(err)
	}
	s.queueSettingsNotificationLocked(settings)
	return nil
}
