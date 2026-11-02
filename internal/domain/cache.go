package domain

import "errors"

type AudioCacheSettings struct {
	Enabled  bool  `json:"enabled"`
	MaxBytes int64 `json:"maxBytes"`
}

func DefaultAudioCacheSettings() AudioCacheSettings {
	return AudioCacheSettings{Enabled: true, MaxBytes: 20 * 1024 * 1024 * 1024}
}

func NormalizeAudioCacheSettings(v AudioCacheSettings) AudioCacheSettings {
	if v == (AudioCacheSettings{}) {
		return DefaultAudioCacheSettings()
	}
	return v
}

func ValidateAudioCacheSettings(v AudioCacheSettings) error {
	// A zero value is an older settings record; the service supplies defaults.
	if v == (AudioCacheSettings{}) {
		return nil
	}
	if v.MaxBytes < 64*1024*1024 || v.MaxBytes > 1024*1024*1024*1024 {
		return errors.New("INVALID_AUDIO_CACHE_SETTINGS")
	}
	return nil
}

type AudioCacheStats struct {
	Available       bool   `json:"available"`
	Enabled         bool   `json:"enabled"`
	MaxBytes        int64  `json:"maxBytes"`
	UsedBytes       int64  `json:"usedBytes"`
	CachedTracks    int    `json:"cachedTracks"`
	ActiveDownloads int    `json:"activeDownloads"`
	ErrorCode       string `json:"errorCode,omitempty"`
}

type AudioCacheClearResult struct {
	RemovedTracks        int `json:"removedTracks"`
	RetainedActiveTracks int `json:"retainedActiveTracks"`
}
