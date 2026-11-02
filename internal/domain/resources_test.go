package domain

import "testing"

func TestResourceCacheSettingsBoundaries(t *testing.T) {
	if got := NormalizeResourceCacheSettings(ResourceCacheSettings{}); got != DefaultResourceCacheSettings() || !got.Enabled || got.MaxBytes != 1024*1024*1024 {
		t.Fatal(got)
	}
	for _, v := range []ResourceCacheSettings{{}, {Enabled: false, MaxBytes: 64 * 1024 * 1024}, {Enabled: true, MaxBytes: 1024 * 1024 * 1024 * 1024}} {
		if err := ValidateResourceCacheSettings(v); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []int64{-1, 1, 64*1024*1024 - 1, 1024*1024*1024*1024 + 1} {
		if ValidateResourceCacheSettings(ResourceCacheSettings{Enabled: true, MaxBytes: n}) == nil {
			t.Fatalf("accepted invalid limit %d", n)
		}
	}
}
