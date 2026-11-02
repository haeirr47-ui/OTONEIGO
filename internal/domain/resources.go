package domain

import "errors"

type ResourceCacheSettings struct {
	Enabled  bool  `json:"enabled"`
	MaxBytes int64 `json:"maxBytes"`
}

func DefaultResourceCacheSettings() ResourceCacheSettings {
	return ResourceCacheSettings{Enabled: true, MaxBytes: 1024 * 1024 * 1024}
}

func NormalizeResourceCacheSettings(v ResourceCacheSettings) ResourceCacheSettings {
	if v == (ResourceCacheSettings{}) {
		return DefaultResourceCacheSettings()
	}
	return v
}

func ValidateResourceCacheSettings(v ResourceCacheSettings) error {
	if v == (ResourceCacheSettings{}) {
		return nil
	}
	if v.MaxBytes < 64*1024*1024 || v.MaxBytes > 1024*1024*1024*1024 {
		return errors.New("INVALID_RESOURCE_CACHE_SETTINGS")
	}
	return nil
}

type ResourceCacheStats struct {
	Available    bool   `json:"available"`
	Enabled      bool   `json:"enabled"`
	MaxBytes     int64  `json:"maxBytes"`
	UsedBytes    int64  `json:"usedBytes"`
	CachedCovers int    `json:"cachedCovers"`
	CachedLyrics int    `json:"cachedLyrics"`
	ErrorCode    string `json:"errorCode,omitempty"`
}

type ResourceCacheClearResult struct {
	RemovedCovers int `json:"removedCovers"`
	RemovedLyrics int `json:"removedLyrics"`
}
