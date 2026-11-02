// Package appservice exposes the bounded P0 operations to Wails. Wails injects
// request contexts and propagates CancellablePromise cancellation to them.
package appservice

import (
	"context"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"strings"
	"sync"
	"time"
)

type Catalog interface {
	Search(context.Context, string, int) (domain.SearchResult, error)
	Lyrics(context.Context, string) (domain.LyricData, error)
	Resolve(context.Context, string, string) (domain.ResolvedStream, error)
}
type QueueStore interface {
	LoadQueue() (*domain.QueueSnapshot, error)
	SaveQueue(domain.QueueSnapshot) error
}
type AppService struct {
	client            Catalog
	store             QueueStore
	options           Options
	settingsMu        sync.Mutex
	settingsPending   *domain.LocalSettings
	settingsNotifying bool
}

type LocalLibrary interface {
	GetSettings() (domain.LocalSettings, error)
	SetSettings(domain.LocalSettings) error
	GetLocalFavorites() ([]domain.TrackSnapshot, error)
	SetLocalFavorite(domain.TrackSnapshot, bool) error
	AddLocalHistory(domain.HistoryEntry) error
	ListLocalHistory(int, int) ([]domain.HistoryEntry, error)
	ClearLocalHistory() error
}

type RegionService interface {
	Status() domain.RegionStatus
	Refresh(context.Context) (domain.RegionStatus, error)
}

type Options struct {
	StorageRecovery     func() (domain.StorageRecoveryStatus, error)
	DesktopViews        func() DesktopViewActions
	ResourceCache       ResourceCacheService
	AudioCache          AudioCacheService
	Desktop             func() DesktopActions
	Playlists           PlaylistLibrary
	Library             LocalLibrary
	Region              RegionService
	Capabilities        domain.Capabilities
	SettingsChanged     func(domain.LocalSettings)
	PlaybackEntitlement func() domain.PlaybackEntitlement
}

func New(client Catalog, store QueueStore, options ...Options) *AppService {
	s := &AppService{client: client, store: store}
	if len(options) > 0 {
		s.options = options[0]
	}
	return s
}
func (s *AppService) SearchNetease(ctx context.Context, query string, page int) (domain.SearchResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	r, e := s.client.Search(ctx, query, page)
	if ctx.Err() != nil {
		return domain.SearchResult{}, ctx.Err()
	}
	return r, e
}
func (s *AppService) GetLyrics(ctx context.Context, id string) (domain.LyricData, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	r, e := s.client.Lyrics(ctx, id)
	if ctx.Err() != nil {
		return domain.LyricData{}, ctx.Err()
	}
	return r, e
}
func (s *AppService) ResolveStream(ctx context.Context, id, quality string) (domain.ResolvedStream, error) {
	entitlement := s.playbackEntitlement()
	quality, err := domain.EffectivePlaybackQuality(quality, entitlement)
	if err != nil {
		return domain.ResolvedStream{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, domain.ResolveQualityTimeout(entitlement))
	defer cancel()
	r, e := s.client.Resolve(ctx, id, quality)
	if ctx.Err() != nil {
		return domain.ResolvedStream{}, ctx.Err()
	}
	return r, e
}
func storageError(err error) error {
	if err == nil {
		return nil
	}
	code := err.Error()
	if strings.HasPrefix(code, "INVALID_") || code == "STALE_QUEUE_REVISION" || code == "QUEUE_TOO_LARGE" || code == "LOCAL_METADATA_TOO_LARGE" || code == "LOCAL_METADATA_REQUIRES_NEWER_VERSION" || code == "PLAYLIST_NOT_FOUND" || code == "STALE_PLAYLIST_REVISION" || code == "PLAYLIST_LIMIT_REACHED" || code == "PLAYLIST_TOO_LARGE" {
		return err
	}
	return errors.New("LOCAL_STORAGE_UNAVAILABLE")
}

func (s *AppService) LoadQueue() (*domain.QueueSnapshot, error) {
	if s.store == nil {
		return nil, errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	q, err := s.store.LoadQueue()
	return q, storageError(err)
}
func (s *AppService) SaveQueue(q domain.QueueSnapshot) error {
	if s.store == nil {
		return errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	return storageError(s.store.SaveQueue(q))
}
func (s *AppService) GetCapabilities() domain.Capabilities { return s.options.Capabilities }
func (s *AppService) playbackEntitlement() domain.PlaybackEntitlement {
	if s.options.PlaybackEntitlement != nil {
		return s.options.PlaybackEntitlement()
	}
	return domain.PlaybackEntitlement{}
}
func (s *AppService) GetPlaybackQualityPolicy() domain.PlaybackQualityPolicy {
	return domain.QualityPolicy(s.playbackEntitlement())
}
func (s *AppService) GetRegionStatus() domain.RegionStatus {
	if s.options.Region == nil {
		return domain.RegionStatus{Classification: "unknown"}
	}
	return s.options.Region.Status()
}
func (s *AppService) RefreshRegion(ctx context.Context) (domain.RegionStatus, error) {
	if s.options.Region == nil {
		return domain.RegionStatus{Classification: "unknown"}, errors.New("REGION_DETECTION_UNAVAILABLE")
	}
	return s.options.Region.Refresh(ctx)
}
func (s *AppService) GetSettings() (domain.LocalSettings, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if s.options.Library == nil {
		return domain.LocalSettings{}, errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	v, err := s.options.Library.GetSettings()
	if err != nil {
		return domain.LocalSettings{}, storageError(err)
	}
	return s.settingsView(v), nil
}

// Runtime policy limits playback; reading that view must not rewrite a user's
// dormant preference when membership is unavailable or temporarily unverified.
func (s *AppService) settingsView(v domain.LocalSettings) domain.LocalSettings {
	v.PreferredQuality = domain.NormalizePreferredQuality(v.PreferredQuality, s.playbackEntitlement())
	v.AudioCache = domain.NormalizeAudioCacheSettings(v.AudioCache)
	v.ResourceCache = domain.NormalizeResourceCacheSettings(v.ResourceCache)
	v.Desktop = domain.NormalizeDesktopSettings(v.Desktop)
	return v
}
func (s *AppService) SetSettings(settings domain.LocalSettings) (domain.LocalSettings, error) {
	v, err := s.commitSettings(settings)
	if err == nil {
		s.notifySettingsChanged()
	}
	return v, err
}
func (s *AppService) commitSettings(settings domain.LocalSettings) (domain.LocalSettings, error) {
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	if err := domain.ValidateSettings(settings); err != nil {
		return domain.LocalSettings{}, err
	}
	if settings.PreferredQuality == "standard" || settings.PreferredQuality == "higher" {
		return domain.LocalSettings{}, errors.New("QUALITY_FALLBACK_ONLY")
	}
	if settings.PreferredQuality != "exhigh" && !s.GetPlaybackQualityPolicy().PremiumAllowed {
		return domain.LocalSettings{}, errors.New("QUALITY_MEMBERSHIP_REQUIRED")
	}
	if settings.GDEnabled && !s.options.Capabilities.GD {
		return domain.LocalSettings{}, errors.New("GD_UNAVAILABLE")
	}
	if s.options.Library == nil {
		return domain.LocalSettings{}, errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	current, err := s.options.Library.GetSettings()
	if err != nil {
		return domain.LocalSettings{}, storageError(err)
	}
	if settings.AudioCache == (domain.AudioCacheSettings{}) {
		settings.AudioCache = domain.NormalizeAudioCacheSettings(current.AudioCache)
	}
	if settings.ResourceCache == (domain.ResourceCacheSettings{}) {
		settings.ResourceCache = domain.NormalizeResourceCacheSettings(current.ResourceCache)
	}
	currentDesktop := domain.NormalizeDesktopSettings(current.Desktop)
	if settings.Desktop == (domain.DesktopSettings{}) {
		settings.Desktop = currentDesktop
	}
	// Geometry is owned by native window events, never an old renderer snapshot.
	settings.Desktop.MiniPlacement = currentDesktop.MiniPlacement
	settings.Desktop.LyricsPlacement = currentDesktop.LyricsPlacement
	persisted := settings
	if !s.GetPlaybackQualityPolicy().PremiumAllowed {
		for _, premium := range domain.QualityPolicy(domain.PlaybackEntitlement{LoggedIn: true, Member: true}).SelectableQualities {
			if premium != "exhigh" && current.PreferredQuality == premium {
				// Anonymous clients only see exhigh. An unrelated volume/window
				// save cannot express a change to this dormant premium choice.
				persisted.PreferredQuality = current.PreferredQuality
				break
			}
		}
	}
	if err := s.options.Library.SetSettings(persisted); err != nil {
		return domain.LocalSettings{}, storageError(err)
	}
	s.queueSettingsNotificationLocked(persisted)
	return s.settingsView(persisted), nil
}

func (s *AppService) queueSettingsNotificationLocked(settings domain.LocalSettings) {
	if s.options.SettingsChanged != nil {
		s.settingsPending = &settings
	}
}

// Commits enqueue under settingsMu; a single drain invokes callbacks outside
// it, coalescing concurrent pending commits to their latest complete snapshot.
// A callback may read or write settings without deadlocking or reordering the
// final runtime preferences behind an older notification.
func (s *AppService) notifySettingsChanged() {
	s.settingsMu.Lock()
	if s.settingsNotifying || s.options.SettingsChanged == nil {
		s.settingsMu.Unlock()
		return
	}
	s.settingsNotifying = true
	for s.settingsPending != nil {
		next := *s.settingsPending
		s.settingsPending = nil
		callback := s.options.SettingsChanged
		s.settingsMu.Unlock()
		callback(next)
		s.settingsMu.Lock()
	}
	s.settingsNotifying = false
	s.settingsMu.Unlock()
}
func (s *AppService) GetLocalFavorites() ([]domain.TrackSnapshot, error) {
	if s.options.Library == nil {
		return nil, errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	v, err := s.options.Library.GetLocalFavorites()
	return v, storageError(err)
}
func (s *AppService) SetLocalFavorite(track domain.TrackSnapshot, liked bool) error {
	if s.options.Library == nil {
		return errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	return storageError(s.options.Library.SetLocalFavorite(track, liked))
}
func (s *AppService) AddLocalHistory(entry domain.HistoryEntry) error {
	if s.options.Library == nil {
		return errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	return storageError(s.options.Library.AddLocalHistory(entry))
}
func (s *AppService) ListLocalHistory(limit, offset int) ([]domain.HistoryEntry, error) {
	if s.options.Library == nil {
		return nil, errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	v, err := s.options.Library.ListLocalHistory(limit, offset)
	return v, storageError(err)
}
func (s *AppService) ClearLocalHistory() error {
	if s.options.Library == nil {
		return errors.New("LOCAL_STORAGE_UNAVAILABLE")
	}
	return storageError(s.options.Library.ClearLocalHistory())
}
