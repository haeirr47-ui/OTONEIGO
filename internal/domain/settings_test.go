package domain

import "testing"

func TestDefaultSettings(t *testing.T) {
	settings := DefaultSettings()
	want := LocalSettings{PreferredQuality: "exhigh", RegionMode: "auto", GDEnabled: false, Volume: 1, AudioCache: DefaultAudioCacheSettings(), ResourceCache: DefaultResourceCacheSettings(), Desktop: DefaultDesktopSettings()}
	if settings != want {
		t.Fatalf("default settings = %+v, want %+v", settings, want)
	}
	if err := ValidateSettings(settings); err != nil {
		t.Fatalf("default settings are invalid: %v", err)
	}
}
