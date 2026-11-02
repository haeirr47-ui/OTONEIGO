package appservice

import "errors"

type DesktopActions interface {
	Available() bool
	Hide()
	Show()
}

func (s *AppService) MinimizeToTray() error {
	if s.options.Desktop == nil {
		return errors.New("DESKTOP_UNAVAILABLE")
	}
	d := s.options.Desktop()
	if d == nil || !d.Available() {
		return errors.New("DESKTOP_UNAVAILABLE")
	}
	d.Hide()
	return nil
}
func (s *AppService) ShowMainWindow() error {
	if s.options.Desktop == nil {
		return errors.New("DESKTOP_UNAVAILABLE")
	}
	d := s.options.Desktop()
	if d == nil {
		return errors.New("DESKTOP_UNAVAILABLE")
	}
	d.Show()
	return nil
}
