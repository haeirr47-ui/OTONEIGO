package stream

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

type qualityCatalog struct {
	call func(context.Context, string, string) (domain.ResolvedStream, error)
}

func (c qualityCatalog) Resolve(ctx context.Context, id, q string) (domain.ResolvedStream, error) {
	return c.call(ctx, "normal", q)
}
func (c qualityCatalog) ResolveCompatible(ctx context.Context, id, q string) (domain.ResolvedStream, error) {
	return c.call(ctx, "compatible", q)
}

type qualityFallback struct {
	call func(context.Context, string, string) (domain.ResolvedStream, error)
}

func (f qualityFallback) Resolve(ctx context.Context, id, q string) (domain.ResolvedStream, error) {
	return f.call(ctx, id, q)
}

func TestSourcePriorityQualityLadders(t *testing.T) {
	for _, tc := range []struct{ mode, class, source string }{
		{"auto", "CN", "normal"}, {"auto", "overseas", "compatible"}, {"auto", "unknown", "normal"}, {"always", "CN", "compatible"}, {"off", "overseas", "normal"},
	} {
		for _, quality := range []string{"exhigh", "higher", "standard", "gd"} {
			t.Run(tc.mode+"/"+tc.class+"/"+quality, func(t *testing.T) {
				winner := tc.source + "/" + quality
				if quality == "gd" {
					winner = "gd/standard"
				}
				var calls []string
				call := func(ctx context.Context, source, q string) (domain.ResolvedStream, error) {
					calls = append(calls, source+"/"+q)
					deadline, _ := ctx.Deadline()
					budget := 4 * time.Second
					if source == "compatible" {
						budget = 5 * time.Second
					}
					if source == "gd" {
						budget = 17 * time.Second
					}
					if time.Until(deadline) > budget {
						t.Error("stage budget extended", source)
					}
					if source+"/"+q == winner {
						return domain.ResolvedStream{SongID: "123", Provider: "gd-netease", Status: "full", URL: "https://m7.music.126.net/a", ActualQuality: q}, nil
					}
					return domain.ResolvedStream{Status: "unavailable"}, nil
				}
				r := NewResolver(qualityCatalog{call}, &fakeRegion{tc.class, make(chan struct{})}, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: tc.mode, GDEnabled: true} })
				r.SetFallback(qualityFallback{func(ctx context.Context, id, q string) (domain.ResolvedStream, error) { return call(ctx, "gd", q) }})
				v, err := r.Resolve(context.Background(), "123", "standard")
				stages := []string{tc.source}
				if tc.mode == "auto" && tc.class == "unknown" {
					stages = append(stages, "compatible")
				}
				stages = append(stages, "gd")
				var want []string
			outer:
				for _, source := range stages {
					for _, q := range []string{"exhigh", "higher", "standard"} {
						step := source + "/" + q
						want = append(want, step)
						if step == winner {
							break outer
						}
					}
				}
				if err != nil || !reflect.DeepEqual(calls, want) || v.URL == "" || v.RequestedQuality != "exhigh" {
					t.Fatal(calls, want, v, err)
				}
			})
		}
	}
}
func TestAnonymousRejectsExplicitPremiumAcrossProviders(t *testing.T) {
	for _, premium := range []domain.ResolvedStream{{ActualBitrate: 320001}, {ActualQuality: "lossless"}, {ActualFormat: "FLAC"}, {ActualFormat: "alac"}} {
		var calls []string
		call := func(ctx context.Context, source, q string) (domain.ResolvedStream, error) {
			calls = append(calls, source+"/"+q)
			v := premium
			v.SongID = "123"
			v.Provider = "gd-netease"
			v.Status = "full"
			v.URL = "https://m7.music.126.net/premium"
			return v, nil
		}
		r := NewResolver(qualityCatalog{call}, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "always", GDEnabled: true} })
		r.SetFallback(qualityFallback{func(ctx context.Context, id, q string) (domain.ResolvedStream, error) { return call(ctx, "gd", q) }})
		v, err := r.Resolve(context.Background(), "123", "jymaster")
		if err != nil || v.URL != "" || len(calls) != 6 {
			t.Fatal(v, err, calls)
		}
		for _, step := range calls {
			if step != "normal/exhigh" && step != "normal/higher" && step != "normal/standard" && step != "compatible/exhigh" && step != "compatible/higher" && step != "compatible/standard" && step != "gd/exhigh" && step != "gd/higher" && step != "gd/standard" {
				t.Fatal(step)
			}
		}
	}
}
func TestHonestAutomaticLowerResponseStopsImmediately(t *testing.T) {
	calls := 0
	c := qualityCatalog{func(ctx context.Context, source, q string) (domain.ResolvedStream, error) {
		calls++
		if q != "exhigh" {
			t.Error(q)
		}
		return domain.ResolvedStream{Status: "full", URL: "https://m7.music.126.net/lower", ActualQuality: "standard", ActualBitrate: 128000, ActualFormat: "mp3"}, nil
	}}
	v, err := NewResolver(c, nil, nil).Resolve(context.Background(), "123", "hires")
	if err != nil || calls != 1 || v.ActualQuality != "standard" || v.ActualBitrate != 128000 {
		t.Fatal(v, err, calls)
	}
}
func TestQualityCancellationStopsNextDispatch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	c := qualityCatalog{func(context.Context, string, string) (domain.ResolvedStream, error) {
		calls++
		cancel()
		return domain.ResolvedStream{Status: "unavailable"}, nil
	}}
	v, err := NewResolver(c, nil, nil).Resolve(ctx, "123", "exhigh")
	if !errors.Is(err, context.Canceled) || calls != 1 || v.URL != "" {
		t.Fatal(v, err, calls)
	}
}
func TestMemberGDQualityDeduplicatesEquivalentBitrates(t *testing.T) {
	var got []string
	c := qualityCatalog{func(context.Context, string, string) (domain.ResolvedStream, error) {
		return domain.ResolvedStream{Status: "unavailable"}, nil
	}}
	r := NewResolver(c, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "off", GDEnabled: true} }, Options{PlaybackEntitlement: func() domain.PlaybackEntitlement { return domain.PlaybackEntitlement{LoggedIn: true, Member: true} }})
	r.SetFallback(qualityFallback{func(ctx context.Context, id, q string) (domain.ResolvedStream, error) {
		got = append(got, q)
		deadline, _ := ctx.Deadline()
		if time.Until(deadline) > 31*time.Second {
			t.Error("GD stage unbounded")
		}
		return domain.ResolvedStream{}, nil
	}})
	_, err := r.Resolve(context.Background(), "123", "jymaster")
	if err != nil || !reflect.DeepEqual(got, []string{"jymaster", "lossless", "exhigh", "higher", "standard"}) {
		t.Fatal(got, err)
	}
}

func TestEntitlementSnapshotCannotOutliveInvalidate(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	c := qualityCatalog{func(context.Context, string, string) (domain.ResolvedStream, error) {
		t.Error("stale entitlement dispatched")
		return domain.ResolvedStream{}, nil
	}}
	r := NewResolver(c, nil, nil, Options{PlaybackEntitlement: func() domain.PlaybackEntitlement {
		close(entered)
		<-release
		return domain.PlaybackEntitlement{LoggedIn: true, Member: true}
	}})
	done := make(chan error, 1)
	go func() { _, err := r.Resolve(context.Background(), "123", "hires"); done <- err }()
	<-entered
	r.Invalidate()
	close(release)
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestEntitlementResponseCanDowngradeWithinSameSource(t *testing.T) {
	for _, tc := range []struct{ source, status, winner string }{{"normal", "unauthorized", "higher"}, {"compatible", "trial", "standard"}} {
		t.Run(tc.source, func(t *testing.T) {
			var calls []string
			c := qualityCatalog{func(ctx context.Context, source, q string) (domain.ResolvedStream, error) {
				calls = append(calls, source+"/"+q)
				if source != tc.source {
					return domain.ResolvedStream{Status: "unavailable"}, nil
				}
				if q == tc.winner {
					return domain.ResolvedStream{Status: "full", URL: "https://m7.music.126.net/free", ActualQuality: q}, nil
				}
				return domain.ResolvedStream{Status: tc.status, ErrorCode: "TRIAL_ONLY"}, nil
			}}
			v, err := NewResolver(c, nil, nil).Resolve(context.Background(), "123", "exhigh")
			if err != nil || v.Status != "full" || v.ActualQuality != tc.winner || calls[len(calls)-1] != tc.source+"/"+tc.winner {
				t.Fatal(v, err, calls)
			}
			if tc.source == "normal" && len(calls) != 2 {
				t.Fatal("left source prematurely", calls)
			}
		})
	}
}
func TestPremiumTrialCauseSurvivesQualityDefense(t *testing.T) {
	calls := 0
	c := qualityCatalog{func(ctx context.Context, source, q string) (domain.ResolvedStream, error) {
		calls++
		if source != "normal" {
			t.Error("entitlement failure triggered header compatibility")
		}
		return domain.ResolvedStream{Status: "trial", ErrorCode: "TRIAL_ONLY", URL: "https://m7.music.126.net/trial", ActualQuality: "hires", ActualFormat: "flac", ActualBitrate: 900000}, nil
	}}
	v, err := NewResolver(c, nil, nil).Resolve(context.Background(), "123", "exhigh")
	if err != nil || v.Status != "trial" || v.ErrorCode != "TRIAL_ONLY" || v.URL != "" || calls != 3 {
		t.Fatal(v, err, calls)
	}
}

func TestStageTimeoutPreservesObservedEntitlementButCallerCancellationWins(t *testing.T) {
	for _, callerCancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "stage", true: "caller"}[callerCancel], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			v, err := qualityStage(ctx, 20*time.Millisecond, []string{"exhigh", "higher", "standard"}, domain.QualityPolicy(domain.PlaybackEntitlement{}), false, false, "123", func(stage context.Context, id, q string) (domain.ResolvedStream, error) {
				calls++
				if q == "exhigh" {
					return domain.ResolvedStream{Status: "trial", ErrorCode: "TRIAL_ONLY", URL: "https://m7.music.126.net/trial"}, nil
				}
				if callerCancel {
					cancel()
				}
				<-stage.Done()
				return domain.ResolvedStream{}, stage.Err()
			})
			if callerCancel {
				if !errors.Is(err, context.Canceled) || v.URL != "" {
					t.Fatal(v, err)
				}
			} else if err != nil || v.Status != "trial" || v.ErrorCode != "TRIAL_ONLY" || v.URL != "" || !noCompatibility(v, err) {
				t.Fatal(v, err)
			}
			if calls != 2 {
				t.Fatal("unexpected ladder dispatch", calls)
			}
		})
	}
}

func TestCallerDeadlineDoesNotReturnProtectedStageResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	v, err := qualityStage(ctx, time.Second, []string{"exhigh", "higher"}, domain.QualityPolicy(domain.PlaybackEntitlement{}), false, false, "123", func(stage context.Context, id, q string) (domain.ResolvedStream, error) {
		if q == "exhigh" {
			return domain.ResolvedStream{Status: "trial", ErrorCode: "TRIAL_ONLY", URL: "https://m7.music.126.net/trial"}, nil
		}
		<-stage.Done()
		return domain.ResolvedStream{}, stage.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) || v.URL != "" || v.Status != "" {
		t.Fatal(v, err)
	}
}

func TestGDBudgetExhaustionIsExplicitExceptAuthoritativeEntitlement(t *testing.T) {
	for _, status := range []string{"unavailable", "trial"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			r := NewResolver(qualityCatalog{func(context.Context, string, string) (domain.ResolvedStream, error) {
				return domain.ResolvedStream{Status: status}, nil
			}}, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "off", GDEnabled: true} })
			r.SetFallback(qualityFallback{func(context.Context, string, string) (domain.ResolvedStream, error) {
				calls++
				return domain.ResolvedStream{}, errors.New("GD_BUDGET_EXHAUSTED")
			}})
			v, err := r.Resolve(context.Background(), "123", "exhigh")
			if calls != 1 {
				t.Fatal("retried unusable budget", calls)
			}
			if status == "trial" {
				if err != nil || v.Status != "trial" {
					t.Fatal(v, err)
				}
			} else if err == nil || err.Error() != "GD_BUDGET_EXHAUSTED" || v.ErrorCode != "GD_BUDGET_EXHAUSTED" {
				t.Fatal(v, err)
			}
		})
	}
}

func TestGDBudgetDoesNotReplaceConcreteNeteaseFailure(t *testing.T) {
	for _, code := range []string{"EMPTY_URL", "NO_AUDIO_URL", "REGION_RESTRICTED", "RESOLVE_TIMEOUT"} {
		t.Run(code, func(t *testing.T) {
			r := NewResolver(qualityCatalog{func(context.Context, string, string) (domain.ResolvedStream, error) {
				return domain.ResolvedStream{Status: "unavailable", ErrorCode: code}, nil
			}}, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "off", GDEnabled: true} })
			r.SetFallback(qualityFallback{func(context.Context, string, string) (domain.ResolvedStream, error) {
				return domain.ResolvedStream{}, errors.New("GD_BUDGET_EXHAUSTED")
			}})
			v, err := r.Resolve(context.Background(), "123", "exhigh")
			if err != nil || v.ErrorCode != code {
				t.Fatalf("primary failure %s replaced by %+v / %v", code, v, err)
			}
		})
	}
}
