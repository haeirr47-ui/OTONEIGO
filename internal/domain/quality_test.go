package domain

import (
	"reflect"
	"testing"
	"time"
)

func TestQualityPolicyRequiresVerifiedLoginAndMembership(t *testing.T) {
	for _, e := range []PlaybackEntitlement{{}, {LoggedIn: true}, {Member: true}} {
		p := QualityPolicy(e)
		if p.PremiumAllowed || p.MaxBitrate != 320000 || !reflect.DeepEqual(p.SelectableQualities, []string{"exhigh"}) {
			t.Fatalf("unverified entitlement %+v granted %+v", e, p)
		}
		if ResolveQualityTimeout(e) != 26*time.Second {
			t.Fatal("unexpected anonymous budget")
		}
	}
	p := QualityPolicy(PlaybackEntitlement{LoggedIn: true, Member: true})
	if !p.PremiumAllowed || p.MaxBitrate != 0 || !reflect.DeepEqual(p.SelectableQualities, []string{"exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster"}) {
		t.Fatalf("verified member policy %+v", p)
	}
}

func TestPreferredQualityNormalizationAndStructuralCompatibility(t *testing.T) {
	premium := PlaybackEntitlement{LoggedIn: true, Member: true}
	for _, q := range []string{"standard", "higher", "exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster"} {
		s := DefaultSettings()
		s.PreferredQuality = q
		if err := ValidateSettings(s); err != nil {
			t.Fatalf("legacy structure %s: %v", q, err)
		}
		for _, e := range []PlaybackEntitlement{{}, {Member: true}, premium} {
			want := "exhigh"
			if e == premium && q != "standard" && q != "higher" {
				want = q
			}
			if got := NormalizePreferredQuality(q, e); got != want {
				t.Fatalf("normalize %s/%+v: %s", q, e, got)
			}
			if got, err := EffectivePlaybackQuality(q, e); err != nil || got != want {
				t.Fatalf("effective %s/%+v: %s %v", q, e, got, err)
			}
		}
	}
	if got := NormalizePreferredQuality("unknown", premium); got != "exhigh" {
		t.Fatal(got)
	}
	if _, err := EffectivePlaybackQuality("unknown", premium); err == nil || err.Error() != "INVALID_QUALITY" {
		t.Fatalf("unknown: %v", err)
	}
}

func TestQualityFallbacksNeverAddHigherQualities(t *testing.T) {
	wants := map[string][]string{
		"standard": {"standard"}, "higher": {"higher", "standard"}, "exhigh": {"exhigh", "higher", "standard"},
		"lossless": {"lossless", "exhigh", "higher", "standard"}, "hires": {"hires", "lossless", "exhigh", "higher", "standard"},
		"jyeffect": {"jyeffect", "lossless", "exhigh", "higher", "standard"}, "sky": {"sky", "lossless", "exhigh", "higher", "standard"},
		"jymaster": {"jymaster", "hires", "lossless", "exhigh", "higher", "standard"},
	}
	for q, want := range wants {
		if got := QualityFallbacks(q); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: %v", q, got)
		}
	}
	if QualityFallbacks("unknown") != nil {
		t.Fatal("unknown ladder")
	}
}
