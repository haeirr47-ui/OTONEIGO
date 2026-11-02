package domain

import (
	"errors"
	"time"
)

// PlaybackEntitlement is supplied only by a trusted backend account provider.
// A membership flag without a verified login never enables premium playback.
type PlaybackEntitlement struct {
	LoggedIn  bool
	Member    bool
	AccountID string // Trusted backend account identity; never accepted from UI settings.
}

type PlaybackQualityPolicy struct {
	PremiumAllowed      bool     `json:"premiumAllowed"`
	SelectableQualities []string `json:"selectableQualities"`
	MaxBitrate          int64    `json:"maxBitrate"` // Zero means no policy upper limit.
}

func QualityPolicy(e PlaybackEntitlement) PlaybackQualityPolicy {
	if e.LoggedIn && e.Member {
		return PlaybackQualityPolicy{PremiumAllowed: true, SelectableQualities: []string{"exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster"}}
	}
	return PlaybackQualityPolicy{SelectableQualities: []string{"exhigh"}, MaxBitrate: 320000}
}

func knownQuality(q string) bool {
	switch q {
	case "standard", "higher", "exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster":
		return true
	}
	return false
}

func NormalizePreferredQuality(q string, e PlaybackEntitlement) string {
	if !knownQuality(q) || q == "standard" || q == "higher" || (q != "exhigh" && !QualityPolicy(e).PremiumAllowed) {
		return "exhigh"
	}
	return q
}

func EffectivePlaybackQuality(q string, e PlaybackEntitlement) (string, error) {
	if !knownQuality(q) {
		return "", errors.New("INVALID_QUALITY")
	}
	return NormalizePreferredQuality(q, e), nil
}

// QualityFallbacks includes the requested quality, followed by automatic-only
// lower qualities. Effects and spatial variants descend to plain lossless.
func QualityFallbacks(q string) []string {
	switch q {
	case "standard":
		return []string{"standard"}
	case "higher":
		return []string{"higher", "standard"}
	case "exhigh":
		return []string{"exhigh", "higher", "standard"}
	case "lossless":
		return []string{"lossless", "exhigh", "higher", "standard"}
	case "jymaster":
		return []string{"jymaster", "hires", "lossless", "exhigh", "higher", "standard"}
	case "hires", "jyeffect", "sky":
		return []string{q, "lossless", "exhigh", "higher", "standard"}
	}
	return nil
}

// ResolveQualityTimeout bounds direct, compatibility and rate-limited GD work
// together; callers' shorter deadlines always retain precedence.
func ResolveQualityTimeout(e PlaybackEntitlement) time.Duration {
	if QualityPolicy(e).PremiumAllowed {
		return 40 * time.Second
	}
	return 26 * time.Second
}
