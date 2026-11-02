package appservice

import (
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
)

type DesktopViewActions interface {
	Epoch() int64
	Snapshot() domain.DesktopSnapshot
	State() domain.DesktopNativeState
	ShowView(string) error
	HideView(string) error
	Dispatch(domain.DesktopCommand) error
	ResetPositions() error
}

func (s *AppService) desktopViews() (DesktopViewActions, error) {
	if s.options.DesktopViews != nil {
		if d := s.options.DesktopViews(); d != nil {
			return d, nil
		}
	}
	return nil, errors.New("DESKTOP_UNAVAILABLE")
}
func (s *AppService) GetDesktopEpoch() (int64, error) {
	d, e := s.desktopViews()
	if e != nil {
		return 0, e
	}
	return d.Epoch(), nil
}
func (s *AppService) GetDesktopSnapshot() (domain.DesktopSnapshot, error) {
	d, e := s.desktopViews()
	if e != nil {
		return domain.DesktopSnapshot{}, e
	}
	return d.Snapshot(), nil
}
func (s *AppService) GetDesktopNativeState() (domain.DesktopNativeState, error) {
	d, e := s.desktopViews()
	if e != nil {
		return domain.DesktopNativeState{}, e
	}
	return d.State(), nil
}
func (s *AppService) ShowDesktopView(kind string) error {
	d, e := s.desktopViews()
	if e != nil {
		return e
	}
	return d.ShowView(kind)
}
func (s *AppService) HideDesktopView(kind string) error {
	d, e := s.desktopViews()
	if e != nil {
		return e
	}
	return d.HideView(kind)
}
func (s *AppService) DispatchDesktopCommand(c domain.DesktopCommand) error {
	if e := domain.ValidateDesktopCommand(c); e != nil {
		return e
	}
	d, e := s.desktopViews()
	if e != nil {
		return e
	}
	return d.Dispatch(c)
}
func (s *AppService) ResetDesktopViewPositions() error {
	d, e := s.desktopViews()
	if e != nil {
		return e
	}
	return d.ResetPositions()
}
