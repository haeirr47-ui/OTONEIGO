package stream

import (
	"context"
	"errors"
	"fmt"
	"github.com/voici5986/otoneigo/internal/domain"
	"sync"
	"testing"
	"time"
)

type fakeCatalog struct {
	normal, compatible func(context.Context) (domain.ResolvedStream, error)
	calls              int
}

func (f *fakeCatalog) Resolve(ctx context.Context, _, _ string) (domain.ResolvedStream, error) {
	return f.normal(ctx)
}
func (f *fakeCatalog) ResolveCompatible(ctx context.Context, _, _ string) (domain.ResolvedStream, error) {
	f.calls++
	return f.compatible(ctx)
}

type fakeRegion struct {
	class   string
	changed chan struct{}
}

func (f *fakeRegion) Status() domain.RegionStatus {
	return domain.RegionStatus{Classification: f.class}
}
func (f *fakeRegion) Changes() <-chan struct{} { return f.changed }
func TestPolicyAndEntitlement(t *testing.T) {
	for _, tc := range []struct {
		name, mode, class, status, code string
		wantNormal, wantCompatible      int
	}{
		{"CN", "auto", "CN", "unavailable", "", 3, 0},
		{"overseas", "auto", "overseas", "unavailable", "", 0, 3},
		{"unknown", "auto", "unknown", "unavailable", "", 3, 3},
		{"unknownFull", "auto", "unknown", "full", "", 1, 0},
		{"alwaysCN", "always", "CN", "unavailable", "", 0, 3},
		{"alwaysUnknown", "always", "unknown", "unavailable", "", 0, 3},
		{"offOverseas", "off", "overseas", "unavailable", "", 3, 0},
		{"offUnknown", "off", "unknown", "unavailable", "", 3, 0},
		{"compatibleLowerQuality", "auto", "overseas", "full", "", 0, 1},
		{"trialUnknown", "auto", "unknown", "trial", "", 3, 0},
		{"trialOverseas", "auto", "overseas", "trial", "", 0, 3},
		{"unauthorized", "always", "overseas", "unauthorized", "", 0, 3},
		{"login", "always", "unknown", "unknown", "LOGIN_REQUIRED", 0, 3},
		{"rate", "always", "overseas", "unknown", "RATE_LIMITED", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			normalCalls := 0
			response := func(context.Context) (domain.ResolvedStream, error) {
				var e error
				if tc.code != "" {
					e = errors.New(tc.code)
				}
				return domain.ResolvedStream{Status: tc.status, URL: "https://m7.music.126.net/a", ActualQuality: "standard"}, e
			}
			c := &fakeCatalog{normal: func(ctx context.Context) (domain.ResolvedStream, error) { normalCalls++; return response(ctx) }, compatible: response}
			r := NewResolver(c, &fakeRegion{tc.class, make(chan struct{})}, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: tc.mode} })
			v, _ := r.Resolve(context.Background(), "123", "hires")
			if normalCalls != tc.wantNormal || c.calls != tc.wantCompatible {
				t.Fatalf("normal=%d compatible=%d", normalCalls, c.calls)
			}
			if tc.wantNormal == 0 && v.AccessStrategy != "region-header" {
				t.Fatalf("%+v", v)
			}
		})
	}
}
func TestCancelDiscardsIgnoringProvider(t *testing.T) {
	for _, strategy := range []string{"normal", "compatible"} {
		for _, reason := range []string{"parent", "policy", "network", "close"} {
			t.Run(strategy+"/"+reason, func(t *testing.T) {
				started := make(chan struct{})
				release := make(chan struct{})
				var once sync.Once
				c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
					once.Do(func() { close(started) })
					<-release
					return domain.ResolvedStream{URL: "https://m7.music.126.net/late", Status: "full"}, nil
				}}
				if strategy == "compatible" {
					c.compatible = c.normal
					c.normal = func(context.Context) (domain.ResolvedStream, error) {
						t.Error("selected compatible path dispatched ordinary request")
						return domain.ResolvedStream{}, nil
					}
				}
				classification := "unknown"
				if strategy == "compatible" {
					classification = "overseas"
				}
				region := &fakeRegion{classification, make(chan struct{})}
				r := NewResolver(c, region, nil)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan reply, 1)
				go func() { v, e := r.Resolve(ctx, "123", "standard"); done <- reply{v, e} }()
				<-started
				switch reason {
				case "parent":
					cancel()
				case "policy":
					r.Invalidate()
				case "network":
					close(region.changed)
				case "close":
					r.Close()
				}
				select {
				case v := <-done:
					if !errors.Is(v.err, context.Canceled) || v.stream.URL != "" {
						t.Fatalf("late URL accepted %+v", v)
					}
				case <-time.After(time.Second):
					t.Fatal("cancel waited for provider")
				}
				close(release)
			})
		}
	}
}
func TestGenerationCheckAfterImmediateProvider(t *testing.T) {
	rgn := &fakeRegion{"overseas", make(chan struct{})}
	c := &fakeCatalog{compatible: func(context.Context) (domain.ResolvedStream, error) {
		close(rgn.changed)
		return domain.ResolvedStream{Status: "full", URL: "late"}, nil
	}}
	v, e := NewResolver(c, rgn, nil).Resolve(context.Background(), "123", "standard")
	if e == nil || v.URL != "" {
		t.Fatal("late result escaped")
	}
}
func TestPhaseBudgetAndUnknownFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, e := phase(ctx, time.Second, func(c context.Context) (domain.ResolvedStream, error) {
		<-c.Done()
		return domain.ResolvedStream{}, nil
	})
	if !errors.Is(e, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatal(e)
	}
	c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
		return domain.ResolvedStream{Status: "unavailable", ErrorCode: "EMPTY_URL"}, nil
	}, compatible: func(context.Context) (domain.ResolvedStream, error) {
		return domain.ResolvedStream{}, errors.New("PROVIDER_UNAVAILABLE")
	}}
	v, e := NewResolver(c, &fakeRegion{"unknown", make(chan struct{})}, nil).Resolve(context.Background(), "123", "standard")
	if e != nil || v.ErrorCode != "EMPTY_URL" || v.RegionSuspected {
		t.Fatalf("invented regional cause %+v %v", v, e)
	}
}
func TestCompatibleTrialStaysTrial(t *testing.T) {
	c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
		return domain.ResolvedStream{Status: "unavailable"}, nil
	}, compatible: func(context.Context) (domain.ResolvedStream, error) {
		return domain.ResolvedStream{Status: "trial", URL: "https://m7.music.126.net/a"}, nil
	}}
	v, e := NewResolver(c, nil, nil).Resolve(context.Background(), "123", "standard")
	if e != nil || v.Status != "trial" {
		t.Fatal(v, e)
	}
}
func TestPolicySnapshotCannotOutliveInvalidate(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
		return domain.ResolvedStream{Status: "full", URL: "stale"}, nil
	}}
	r := NewResolver(c, nil, func() domain.LocalSettings { close(entered); <-release; return domain.DefaultSettings() })
	done := make(chan reply, 1)
	go func() { v, e := r.Resolve(context.Background(), "123", "standard"); done <- reply{v, e} }()
	<-entered
	r.Invalidate()
	close(release)
	v := <-done
	if !errors.Is(v.err, context.Canceled) || v.stream.URL != "" {
		t.Fatal("stale policy returned", v)
	}
}
func TestCompatibilityCooldownResetsOnNetwork(t *testing.T) {
	c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
		return domain.ResolvedStream{Status: "unavailable"}, nil
	}, compatible: func(context.Context) (domain.ResolvedStream, error) {
		return domain.ResolvedStream{}, errors.New("PROVIDER_UNAVAILABLE")
	}}
	region := &fakeRegion{"overseas", make(chan struct{})}
	r := NewResolver(c, region, nil)
	for i := 0; i < 4; i++ {
		_, _ = r.Resolve(context.Background(), "123", "standard")
	}
	if c.calls != 9 {
		t.Fatal("compatibility cooldown missing", c.calls)
	}
	close(region.changed)
	region.changed = make(chan struct{})
	_, _ = r.Resolve(context.Background(), "123", "standard")
	if c.calls != 12 {
		t.Fatal("new network retained stale cooldown")
	}
	r.Invalidate()
	_, _ = r.Resolve(context.Background(), "123", "standard")
	if c.calls != 15 {
		t.Fatal("policy change retained stale cooldown")
	}
}

type cacheFallback struct {
	mu            sync.Mutex
	invalidations int
	calls         int
	invalidate    func()
	resolve       func(context.Context) (domain.ResolvedStream, error)
}

func (f *cacheFallback) Invalidate() {
	f.mu.Lock()
	f.invalidations++
	hook := f.invalidate
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
}
func (f *cacheFallback) Resolve(ctx context.Context, _, _ string) (domain.ResolvedStream, error) {
	f.mu.Lock()
	f.calls++
	hook := f.resolve
	f.mu.Unlock()
	if hook != nil {
		return hook(ctx)
	}
	return domain.ResolvedStream{SongID: "123", Provider: "gd-netease", Status: "full", URL: "https://m7.music.126.net/gd.mp3"}, nil
}
func TestOneResolutionDeadlineForEveryCandidate(t *testing.T) {
	for _, strategy := range []string{"normal", "compatible", "fallback"} {
		t.Run(strategy, func(t *testing.T) {
			c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
				v := domain.ResolvedStream{Status: "unavailable"}
				if strategy == "normal" {
					v.Status = "full"
					v.URL = "normal"
				}
				return v, nil
			}, compatible: func(context.Context) (domain.ResolvedStream, error) {
				v := domain.ResolvedStream{Status: "unavailable"}
				if strategy == "compatible" {
					v.Status = "full"
					v.URL = "compatible"
				}
				return v, nil
			}}
			r := NewResolver(c, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "auto", GDEnabled: true} })
			f := &cacheFallback{}
			r.SetFallback(f)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			deadline, _ := ctx.Deadline()
			v, e := r.Resolve(ctx, "123", "standard")
			if e != nil || v.URL == "" || v.ResolutionDeadlineAt != deadline.UnixMilli() {
				t.Fatal("deadline was restarted or lost", v, e)
			}
			if strategy == "normal" && (c.calls != 0 || f.calls != 0) {
				t.Fatal("direct full result waited for fallback")
			}
		})
	}
}
func TestFallbackInvalidationGatesCacheWithoutBlockingDirect(t *testing.T) {
	for _, direct := range []bool{true, false} {
		t.Run(map[bool]string{true: "direct", false: "fallback"}[direct], func(t *testing.T) {
			region := &fakeRegion{"overseas", make(chan struct{})}
			c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
				v := domain.ResolvedStream{Status: "unavailable"}
				if direct {
					v.Status = "full"
					v.URL = "direct"
				}
				return v, nil
			}, compatible: func(context.Context) (domain.ResolvedStream, error) {
				return domain.ResolvedStream{Status: "unavailable"}, nil
			}}
			r := NewResolver(c, region, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "off", GDEnabled: true} })
			f := &cacheFallback{}
			r.SetFallback(f)
			entered := make(chan struct{})
			release := make(chan struct{})
			f.invalidate = func() { close(entered); <-release }
			done := make(chan reply, 1)
			go func() { v, e := r.Resolve(context.Background(), "123", "standard"); done <- reply{v, e} }()
			<-entered
			if direct {
				select {
				case v := <-done:
					if v.err != nil || v.stream.URL != "direct" {
						t.Fatal(v)
					}
				case <-time.After(time.Second):
					t.Fatal("GD invalidation blocked direct result")
				}
			} else {
				select {
				case <-done:
					t.Fatal("used cached fallback before invalidation")
				default:
				}
			}
			close(release)
			if !direct {
				v := <-done
				if v.err != nil || v.stream.Provider != "gd-netease" {
					t.Fatal(v)
				}
			}
			f.invalidate = nil
			r.Invalidate()
			f.mu.Lock()
			count := f.invalidations
			f.mu.Unlock()
			if count < 3 {
				t.Fatal("policy invalidation not forwarded", count)
			}
		})
	}
}
func TestCatalogEntitlementFallbackPolicy(t *testing.T) {
	for _, stage := range []string{"normal", "compatible"} {
		for _, status := range []string{"trial", "unauthorized", "login"} {
			for _, enabled := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/gd=%t", stage, status, enabled), func(t *testing.T) {
					protected := domain.ResolvedStream{SongID: "1850977722", Status: status}
					var protectedErr error
					if status == "login" {
						protected.Status = "unknown"
						protectedErr = errors.New("LOGIN_REQUIRED")
					}
					c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
						if stage == "normal" {
							return protected, protectedErr
						}
						return domain.ResolvedStream{Status: "unavailable", ErrorCode: "EMPTY_URL"}, nil
					}, compatible: func(context.Context) (domain.ResolvedStream, error) { return protected, protectedErr }}
					f := &cacheFallback{resolve: func(context.Context) (domain.ResolvedStream, error) {
						return domain.ResolvedStream{SongID: "1850977722", Provider: "gd-netease", Status: "full", URL: "https://m7.music.126.net/gd.mp3"}, nil
					}}
					r := NewResolver(c, nil, func() domain.LocalSettings {
						mode := "always"
						if stage == "normal" {
							mode = "off"
						}
						return domain.LocalSettings{RegionMode: mode, GDEnabled: enabled}
					})
					r.SetFallback(f)
					v, e := r.Resolve(context.Background(), "1850977722", "standard")
					wantCompat := 0
					if stage == "compatible" {
						wantCompat = 3
					}
					if c.calls != wantCompat {
						t.Fatalf("compat calls=%d", c.calls)
					}
					if enabled {
						if e != nil || f.calls != 1 || v.Provider != "gd-netease" || v.Status != "full" {
							t.Fatalf("GD not used: %+v %v calls=%d", v, e, f.calls)
						}
					} else if f.calls != 0 || v.Status != protected.Status || v.ErrorCode != protected.ErrorCode || !errors.Is(e, protectedErr) || v.RegionSuspected {
						t.Fatalf("disabled GD changed catalog response: %+v %v", v, e)
					}
				})
			}
		}
	}
}

func TestUnsafeCatalogFailureNeverUsesGD(t *testing.T) {
	for _, stage := range []string{"normal", "compatible"} {
		for _, code := range []string{"RATE_LIMITED", "INVALID_SONG_ID", "INVALID_QUALITY", "MISMATCHED_SONG_ID", "UNTRUSTED_MEDIA_URL", "INVALID_PROVIDER_RESPONSE", "canceled"} {
			t.Run(stage+"/"+code, func(t *testing.T) {
				failure := errors.New(code)
				if code == "canceled" {
					failure = context.Canceled
				}
				c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
					if stage == "normal" {
						return domain.ResolvedStream{}, failure
					}
					return domain.ResolvedStream{Status: "unavailable"}, nil
				}, compatible: func(context.Context) (domain.ResolvedStream, error) { return domain.ResolvedStream{}, failure }}
				f := &cacheFallback{}
				r := NewResolver(c, nil, func() domain.LocalSettings {
					mode := "always"
					if stage == "normal" {
						mode = "off"
					}
					return domain.LocalSettings{RegionMode: mode, GDEnabled: true}
				})
				r.SetFallback(f)
				_, e := r.Resolve(context.Background(), "123", "standard")
				if f.calls != 0 || !errors.Is(e, failure) {
					t.Fatalf("unsafe failure reached GD: calls=%d err=%v", f.calls, e)
				}
			})
		}
	}
}

func TestFailedOrInvalidGDLeavesCompatibleEntitlementAuthoritative(t *testing.T) {
	for _, kind := range []string{"error", "wrong-id", "wrong-provider", "trial", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
				return domain.ResolvedStream{Status: "unavailable", ErrorCode: "EMPTY_URL"}, nil
			}, compatible: func(context.Context) (domain.ResolvedStream, error) {
				return domain.ResolvedStream{Status: "unauthorized", ErrorCode: "LOGIN_REQUIRED"}, nil
			}}
			f := &cacheFallback{resolve: func(context.Context) (domain.ResolvedStream, error) {
				v := domain.ResolvedStream{SongID: "123", Provider: "gd-netease", Status: "full", URL: "https://m7.music.126.net/a.mp3"}
				switch kind {
				case "error":
					return v, errors.New("https://secret.example/token")
				case "wrong-id":
					v.SongID = "456"
				case "wrong-provider":
					v.Provider = "other"
				case "trial":
					v.Status = "trial"
				case "unknown":
					v.Status = "unknown"
				}
				return v, nil
			}}
			r := NewResolver(c, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "always", GDEnabled: true} })
			r.SetFallback(f)
			v, e := r.Resolve(context.Background(), "123", "standard")
			wantCalls := 3
			if kind == "wrong-id" || kind == "wrong-provider" {
				wantCalls = 1
			}
			if f.calls != wantCalls || e != nil || v.Status != "unauthorized" || v.ErrorCode != "LOGIN_REQUIRED" || v.URL != "" || v.RegionSuspected {
				t.Fatalf("lost catalog failure or accepted invalid alternate: %+v %v calls=%d", v, e, f.calls)
			}
		})
	}
}

func TestCompatibleUnknownStatusWithURLReturnsWithoutGD(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("gd=%t", enabled), func(t *testing.T) {
			c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
				return domain.ResolvedStream{Status: "unavailable", ErrorCode: "EMPTY_URL"}, nil
			}, compatible: func(context.Context) (domain.ResolvedStream, error) {
				return domain.ResolvedStream{Status: "unknown", URL: "https://m7.music.126.net/compatible.mp3"}, nil
			}}
			f := &cacheFallback{}
			r := NewResolver(c, nil, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "always", GDEnabled: enabled} })
			r.SetFallback(f)
			for i := 0; i < 4; i++ {
				v, e := r.Resolve(context.Background(), "123", "standard")
				if e != nil || v.Status != "unknown" || v.URL != "https://m7.music.126.net/compatible.mp3" || v.AccessStrategy != "region-header" || f.calls != 0 {
					t.Fatalf("compatible URL discarded or GD invoked: %+v %v calls=%d", v, e, f.calls)
				}
			}
			if c.calls != 4 || r.failures != 0 || !r.coolUntil.IsZero() {
				t.Fatalf("compatible success counted toward cooldown: calls=%d failures=%d", c.calls, r.failures)
			}
		})
	}
}

func TestSelectedCompatibilityCooldownNeverUsesOrdinary(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("gd=%t", enabled), func(t *testing.T) {
			c := &fakeCatalog{normal: func(context.Context) (domain.ResolvedStream, error) {
				t.Error("cooldown dispatched ordinary playback")
				return domain.ResolvedStream{Status: "full", URL: "ordinary"}, nil
			}, compatible: func(context.Context) (domain.ResolvedStream, error) {
				return domain.ResolvedStream{}, errors.New("PROVIDER_UNAVAILABLE")
			}}
			r := NewResolver(c, &fakeRegion{"overseas", make(chan struct{})}, func() domain.LocalSettings { return domain.LocalSettings{RegionMode: "auto", GDEnabled: enabled} })
			f := &cacheFallback{}
			r.SetFallback(f)
			for i := 0; i < 3; i++ {
				_, _ = r.Resolve(context.Background(), "123", "standard")
			}
			before := f.calls
			v, err := r.Resolve(context.Background(), "123", "standard")
			if err != nil || c.calls != 9 {
				t.Fatal(v, err, c.calls)
			}
			if enabled {
				if f.calls != before+1 || v.Provider != "gd-netease" {
					t.Fatal("GD did not follow cooldown", v, f.calls)
				}
			} else if v.Status != "unavailable" || v.ErrorCode != "COMPATIBILITY_COOLDOWN" || v.AccessStrategy != "region-header" {
				t.Fatal("missing cooldown reason", v)
			}
		})
	}
}
