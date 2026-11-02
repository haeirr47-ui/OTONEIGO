//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/voici5986/otoneigo/internal/appservice"
	"github.com/voici5986/otoneigo/internal/catalog"
	"github.com/voici5986/otoneigo/internal/diagnostics"
	"github.com/voici5986/otoneigo/internal/domain"
	"github.com/voici5986/otoneigo/internal/gd"
	"github.com/voici5986/otoneigo/internal/lifecycle"
	"github.com/voici5986/otoneigo/internal/mediacache"
	"github.com/voici5986/otoneigo/internal/region"
	"github.com/voici5986/otoneigo/internal/resourcecache"
	"github.com/voici5986/otoneigo/internal/storage"
	"github.com/voici5986/otoneigo/internal/stream"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/sys/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

const gdReleaseValidated = true

func main() {
	// Framework/native diagnostics may contain payloads; our recorder accepts
	// only a fixed, sanitized metadata schema.
	log.SetOutput(io.Discard)
	dataDir := os.Getenv("OTONEIGO_DATA_DIR")
	if dataDir == "" {
		local, err := os.UserCacheDir()
		if err != nil {
			startupError()
			os.Exit(1)
		}
		dataDir = filepath.Join(local, "OTONEIGO")
	}
	store, err := storage.Open(filepath.Join(dataDir, "library.db"))
	if err != nil {
		startupError()
		os.Exit(1)
	}
	settings, err := store.GetSettings()
	if err != nil {
		_ = store.Close()
		startupError()
		os.Exit(1)
	}
	capabilities := domain.Capabilities{RegionCompatibility: true, GD: gdEnabledForBuild()}
	// Account capability remains disabled until a backend account provider has
	// independently verified login and membership. UI input cannot grant access.
	playbackEntitlement := func() domain.PlaybackEntitlement { return domain.PlaybackEntitlement{} }
	previousSettings := settings
	// Preserve the configured quality on disk; ResolveStream and the settings
	// view independently enforce the currently verified playback entitlement.
	settings.AudioCache = domain.NormalizeAudioCacheSettings(settings.AudioCache)
	settings.ResourceCache = domain.NormalizeResourceCacheSettings(settings.ResourceCache)
	settings.Desktop = domain.NormalizeDesktopSettings(settings.Desktop)
	if settings.GDEnabled && !capabilities.GD {
		settings.GDEnabled = false
	}
	if settings != previousSettings {
		if err := store.SetSettings(settings); err != nil {
			_ = store.Close()
			startupError()
			os.Exit(1)
		}
	}
	var preferences atomic.Value
	preferences.Store(settings)
	controller := region.NewController()
	ctx, cancel := context.WithCancel(context.Background())
	recorder, _ := diagnostics.Open(filepath.Join(dataDir, "log"))
	var lastRecoveryCount atomic.Int64
	lastRecoveryCount.Store(-1)
	recoveryStatus := func() (domain.StorageRecoveryStatus, error) {
		r, err := store.RecoverySummary()
		if err != nil {
			return domain.StorageRecoveryStatus{}, err
		}
		count := r.QuarantinedFavorites + r.QuarantinedHistory
		if r.SettingsRecovered {
			count++
		}
		if previous := lastRecoveryCount.Swap(count); count > 0 && previous != count {
			recorder.Record(diagnostics.Event{Stage: "storage", ErrorCode: "LOCAL_RECORDS_QUARANTINED"})
		}
		return domain.StorageRecoveryStatus{QuarantinedFavorites: r.QuarantinedFavorites, QuarantinedHistory: r.QuarantinedHistory, SettingsRecovered: r.SettingsRecovered}, nil
	}
	_, _ = recoveryStatus()
	audioCache, _ := mediacache.New(mediacache.Options{Root: filepath.Join(dataDir, "cache", "audio"), Context: ctx, Settings: func() domain.AudioCacheSettings { return preferences.Load().(domain.LocalSettings).AudioCache }, Entitlement: playbackEntitlement, HTTPClient: cacheMediaHTTPClient()})
	var cacheService appservice.AudioCacheService
	var playbackPreparer playbackCache
	if audioCache != nil {
		cacheService = audioCache
		playbackPreparer = audioCache
	}
	resources, _ := resourcecache.New(resourcecache.Options{
		Root: filepath.Join(dataDir, "cache", "resources"), Context: ctx,
		Settings: func() domain.ResourceCacheSettings { return preferences.Load().(domain.LocalSettings).ResourceCache },
	})
	var resourceService appservice.ResourceCacheService
	assetHandler := application.AssetFileServerFS(assets)
	if resources != nil {
		resourceService = resources
		assetHandler = resources.Handler(assetHandler)
	}
	cacheLookup := func(c context.Context, id, q string, e domain.PlaybackEntitlement) (domain.ResolvedStream, bool) {
		if audioCache == nil {
			return domain.ResolvedStream{}, false
		}
		v, ok := audioCache.Lookup(c, id, q, e)
		if ok {
			recorder.Record(diagnostics.Event{Stage: "cache-hit", SongID: id, Strategy: "cache", Provider: v.Provider, Status: v.Status, RequestedQuality: q, ActualQuality: v.ActualQuality, ActualFormat: v.ActualFormat, ActualBitrate: v.ActualBitrate})
		}
		return v, ok
	}
	client := catalog.NewClient()
	resolver := stream.NewResolver(observedCatalog{Client: client, region: controller, log: recorder}, controller, func() domain.LocalSettings { return preferences.Load().(domain.LocalSettings) }, stream.Options{PlaybackEntitlement: playbackEntitlement, CachedStream: cacheLookup})
	var gdClient *gd.Client
	if capabilities.GD {
		gdClient = gd.NewClient(client)
		resolver.SetFallback(observedFallback{Client: gdClient, region: controller, log: recorder})
	}
	var desktop *desktopController
	service := appservice.New(applicationCatalog{Client: client, resolver: resolver, audioCache: playbackPreparer, resources: resources}, store, appservice.Options{
		StorageRecovery: recoveryStatus,
		DesktopViews: func() appservice.DesktopViewActions {
			if desktop == nil || desktop.Views() == nil {
				return nil
			}
			return desktop.Views()
		},
		AudioCache: cacheService, ResourceCache: resourceService, Desktop: func() appservice.DesktopActions {
			if desktop == nil {
				return nil
			}
			return desktop
		},
		Library: store, Playlists: store, Region: controller, Capabilities: capabilities,
		PlaybackEntitlement: playbackEntitlement,
		SettingsChanged: func(next domain.LocalSettings) {
			previous := preferences.Load().(domain.LocalSettings)
			preferences.Store(next)
			if desktop != nil && previous.Desktop != next.Desktop {
				desktop.ApplyDesktopSettings()
			}
			if audioCache != nil && previous.AudioCache != next.AudioCache {
				audioCache.ApplySettings()
			}
			if resources != nil && previous.ResourceCache != next.ResourceCache {
				resources.ApplySettings()
			}
			if previous.RegionMode != next.RegionMode || previous.PreferredQuality != next.PreferredQuality || previous.GDEnabled != next.GDEnabled || previous.AudioCache.Enabled != next.AudioCache.Enabled {
				resolver.Invalidate()
			}
		},
	})
	controller.Start(ctx)
	var closeOnce sync.Once
	var closeGate *lifecycle.CloseGate
	shutdown := func() {
		closeOnce.Do(func() {
			if closeGate != nil {
				closeGate.Stop()
			}
			cancel()
			resolver.Close()
			controller.Close()
			if gdClient != nil {
				gdClient.Close()
			}
			if audioCache != nil {
				_ = audioCache.Close()
			}
			if resources != nil {
				_ = resources.Close()
			}
			_ = store.Close()
			_ = recorder.Close()
		})
	}
	defer shutdown()
	absoluteDir, _ := filepath.Abs(dataDir)
	instanceHash := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(absoluteDir))))

	var windowRef atomic.Pointer[application.WebviewWindow]
	app := application.New(application.Options{
		Name:        "OTONEIGO",
		Description: "网易云音乐 · Windows 本地客户端",
		// Framework transport diagnostics may contain bound-method payloads.
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Services: []application.Service{application.NewService(service)},
		Assets:   application.AssetOptions{Handler: assetHandler, DisableLogging: true},
		Windows: application.WindowsOptions{
			WebviewUserDataPath:   filepath.Join(dataDir, "webview"),
			AdditionalBrowserArgs: debugBrowserArgs(),
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: fmt.Sprintf("io.github.voici5986.otoneigo.%x", instanceHash[:8]),
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window := windowRef.Load(); window != nil {
					window.Show()
					window.Focus()
				}
			},
		},
		OnShutdown: shutdown,
	})
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "main", Title: "OTONEIGO", Width: 1200, Height: 800,
		MinWidth: 900, MinHeight: 640,
		DevToolsEnabled: debugToolsEnabled(),
	})
	windowRef.Store(window)
	desktop, _ = newDesktopController(app, window, func() domain.LocalSettings { return preferences.Load().(domain.LocalSettings) })
	if desktop != nil {
		desktop.onTrayUnavailable = func() { recorder.Record(diagnostics.Event{Stage: "desktop", ErrorCode: "TRAY_UNAVAILABLE"}) }
		desktop.ConfigureViews(app, service.SaveDesktopPlacement)
		app.OnShutdown(desktop.Close)
	}
	closeGate = lifecycle.NewCloseGate(3*time.Second, window.Close)
	closeDecisions := &windowCloseCoordinator{gate: closeGate, desktop: desktop}
	app.Event.On("otoneigo:close-ready", func(event *application.CustomEvent) {
		if event.Sender != "main" {
			return
		}
		if token, ok := event.Data.(string); ok {
			closeGate.Acknowledge(token)
		}
	})
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		allow, token := closeDecisions.Request(func() { event.Cancel(); desktop.Hide() })
		if allow {
			if desktop != nil {
				desktop.CloseAuxiliaryWindows()
			}
			return
		}
		event.Cancel()
		if token != "" {
			if desktop != nil {
				desktop.MarkMainClosing()
			}
			window.EmitEvent("otoneigo:prepare-close", token)
		}
	})
	if err := app.Run(); err != nil {
		startupError()
		os.Exit(1)
	}
}

func shouldHideWindowOnClose(gate *lifecycle.CloseGate, desktop *desktopController) bool {
	return gate != nil && !gate.Started() && desktop.WantsHideOnClose()
}

type windowCloseCoordinator struct {
	mu      sync.Mutex
	gate    *lifecycle.CloseGate
	desktop *desktopController
}

func (c *windowCloseCoordinator) Request(hide func()) (bool, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if shouldHideWindowOnClose(c.gate, c.desktop) {
		hide()
		return false, ""
	}
	return c.gate.Request()
}

func startupError() {
	message, _ := windows.UTF16PtrFromString("无法读取本地数据或启动窗口。请确认磁盘空间和应用数据目录权限后重试。")
	title, _ := windows.UTF16PtrFromString("OTONEIGO")
	_, _ = windows.MessageBox(0, message, title, windows.MB_OK|windows.MB_ICONERROR)
}
