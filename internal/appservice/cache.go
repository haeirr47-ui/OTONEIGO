package appservice

import (
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"regexp"
)

type AudioCacheService interface {
	Stats() domain.AudioCacheStats
	Clear() (domain.AudioCacheClearResult, error)
	Release(string)
	ReportFailure(string)
}

var cacheTokenPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (s *AppService) GetAudioCacheStats() domain.AudioCacheStats {
	if s.options.AudioCache != nil {
		return s.options.AudioCache.Stats()
	}
	return domain.AudioCacheStats{ErrorCode: "AUDIO_CACHE_UNAVAILABLE"}
}
func (s *AppService) ClearAudioCache() (domain.AudioCacheClearResult, error) {
	if s.options.AudioCache == nil {
		return domain.AudioCacheClearResult{}, errors.New("AUDIO_CACHE_UNAVAILABLE")
	}
	v, err := s.options.AudioCache.Clear()
	if err != nil {
		return v, errors.New("AUDIO_CACHE_FAILED")
	}
	return v, nil
}
func (s *AppService) ReleasePlaybackSource(token string) error {
	if !cacheTokenPattern.MatchString(token) {
		return errors.New("INVALID_CACHE_TOKEN")
	}
	if s.options.AudioCache != nil {
		s.options.AudioCache.Release(token)
	}
	return nil
}
func (s *AppService) ReportPlaybackSourceFailure(token string) error {
	if !cacheTokenPattern.MatchString(token) {
		return errors.New("INVALID_CACHE_TOKEN")
	}
	if s.options.AudioCache != nil {
		s.options.AudioCache.ReportFailure(token)
	}
	return nil
}
