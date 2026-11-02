package domain

import (
	"errors"
	"math"
)

type LocalSettings struct {
	Desktop          DesktopSettings       `json:"desktop"`
	ResourceCache    ResourceCacheSettings `json:"resourceCache"`
	AudioCache       AudioCacheSettings    `json:"audioCache"`
	CloseToTray      bool                  `json:"closeToTray"`
	PreferredQuality string                `json:"preferredQuality"`
	RegionMode       string                `json:"regionMode"`
	GDEnabled        bool                  `json:"gdEnabled"`
	Volume           float64               `json:"volume"`
}

func DefaultSettings() LocalSettings {
	return LocalSettings{PreferredQuality: "exhigh", RegionMode: "auto", Volume: 1, AudioCache: DefaultAudioCacheSettings(), ResourceCache: DefaultResourceCacheSettings(), Desktop: DefaultDesktopSettings()}
}

func ValidateSettings(s LocalSettings) error {
	if err := ValidateDesktopSettings(s.Desktop); err != nil {
		return err
	}
	if err := ValidateResourceCacheSettings(s.ResourceCache); err != nil {
		return err
	}
	if err := ValidateAudioCacheSettings(s.AudioCache); err != nil {
		return err
	}
	switch s.PreferredQuality {
	case "standard", "higher", "exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster":
	default:
		return errors.New("INVALID_SETTINGS")
	}
	if s.RegionMode != "auto" && s.RegionMode != "always" && s.RegionMode != "off" {
		return errors.New("INVALID_SETTINGS")
	}
	if math.IsNaN(s.Volume) || math.IsInf(s.Volume, 0) || s.Volume < 0 || s.Volume > 1 {
		return errors.New("INVALID_SETTINGS")
	}
	return nil
}

type RegionStatus struct {
	Classification    string `json:"classification"` // CN, overseas or unknown
	Country           string `json:"country,omitempty"`
	ObservedAt        int64  `json:"observedAt"`
	NetworkGeneration int64  `json:"networkGeneration"`
	Detecting         bool   `json:"detecting"`
	ErrorCode         string `json:"errorCode,omitempty"`
}

type Capabilities struct {
	RegionCompatibility bool `json:"regionCompatibility"`
	GD                  bool `json:"gd"`
	Account             bool `json:"account"`
}
