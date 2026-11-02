package appservice

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voici5986/otoneigo/internal/domain"
	"github.com/voici5986/otoneigo/internal/storage"
)

func openLibraryService(t *testing.T, caps domain.Capabilities) (*AppService, *storage.Store) {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return New(nil, nil, Options{Library: store, Capabilities: caps}), store
}

func fixtureTrack(id, name string) domain.TrackSnapshot {
	return domain.TrackSnapshot{NeteaseSongID: id, Name: name, Artists: []domain.Artist{{ID: "2", Name: "artist"}}, Album: &domain.Album{ID: "3", Name: "album"}, DurationMs: 1234}
}

func TestLibraryServicePersistsSettingsAndNotifiesOnlyAfterCommit(t *testing.T) {
	s, db := openLibraryService(t, domain.Capabilities{})
	settings := domain.LocalSettings{PreferredQuality: "exhigh", RegionMode: "always", Volume: .4, AudioCache: domain.DefaultAudioCacheSettings(), ResourceCache: domain.DefaultResourceCacheSettings(), Desktop: domain.DefaultDesktopSettings()}
	var calls int
	s.options.SettingsChanged = func(got domain.LocalSettings) {
		calls++
		if got != settings {
			t.Errorf("callback received %#v", got)
		}
		persisted, err := db.GetSettings()
		if err != nil || persisted != settings {
			t.Errorf("callback before durable commit: %#v %v", persisted, err)
		}
	}
	if got, err := s.SetSettings(settings); err != nil || got != settings {
		t.Fatalf("set: %#v %v", got, err)
	}
	if calls != 1 {
		t.Fatalf("callback count %d", calls)
	}
	if got, err := s.GetSettings(); err != nil || got != settings {
		t.Fatalf("get: %#v %v", got, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSettings(); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" {
		t.Fatalf("storage error leaked: %v", err)
	}
}

func TestLibraryServiceRefusesDisabledGDWithoutWritingOrNotifying(t *testing.T) {
	s, db := openLibraryService(t, domain.Capabilities{GD: false})
	before, err := db.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.options.SettingsChanged = func(domain.LocalSettings) { calls++ }
	attempt := before
	attempt.GDEnabled = true
	if _, err = s.SetSettings(attempt); err == nil || err.Error() != "GD_UNAVAILABLE" {
		t.Fatalf("error %v", err)
	}
	after, err := db.GetSettings()
	if err != nil || after != before {
		t.Fatalf("disabled GD changed persisted settings: %#v %v", after, err)
	}
	if calls != 0 {
		t.Fatalf("callback called %d times", calls)
	}
}

func TestLibraryServiceFavoritesHistoryAreOfflineAndPaginated(t *testing.T) {
	s, _ := openLibraryService(t, domain.Capabilities{})
	for _, tr := range []domain.TrackSnapshot{fixtureTrack("10", "ten"), fixtureTrack("2", "two")} {
		if err := s.SetLocalFavorite(tr, true); err != nil {
			t.Fatal(err)
		}
	}
	favorites, err := s.GetLocalFavorites()
	if err != nil || len(favorites) != 2 || favorites[0].NeteaseSongID != "2" || favorites[1].NeteaseSongID != "10" {
		t.Fatalf("favorites: %#v %v", favorites, err)
	}
	for _, h := range []domain.HistoryEntry{{HistoryEntryID: "a", Track: fixtureTrack("10", "ten"), PlayedAt: 100}, {HistoryEntryID: "b", Track: fixtureTrack("2", "two"), PlayedAt: 200}, {HistoryEntryID: "c", Track: fixtureTrack("10", "ten"), PlayedAt: 200}} {
		if err := s.AddLocalHistory(h); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListLocalHistory(2, 0)
	if err != nil || len(page) != 2 || page[0].HistoryEntryID != "c" || page[1].HistoryEntryID != "b" {
		t.Fatalf("page 1: %#v %v", page, err)
	}
	page, err = s.ListLocalHistory(2, 2)
	if err != nil || len(page) != 1 || page[0].HistoryEntryID != "a" {
		t.Fatalf("page 2: %#v %v", page, err)
	}
	if _, err = s.ListLocalHistory(0, 0); err == nil || err.Error() != "INVALID_HISTORY_PAGINATION" {
		t.Fatalf("pagination validation lost: %v", err)
	}
	if err = s.ClearLocalHistory(); err != nil {
		t.Fatal(err)
	}
	page, err = s.ListLocalHistory(2, 0)
	if err != nil || len(page) != 0 {
		t.Fatalf("clear: %#v %v", page, err)
	}
}

type failingLibrary struct{ LocalLibrary }

func (f failingLibrary) GetSettings() (domain.LocalSettings, error) {
	return domain.LocalSettings{}, errors.New("C:\\private\\path\\token=secret")
}

type staleQueueStore struct{}

func (staleQueueStore) LoadQueue() (*domain.QueueSnapshot, error) { return nil, nil }
func (staleQueueStore) SaveQueue(domain.QueueSnapshot) error      { return storage.ErrStaleRevision }

func TestStorageErrorsAreSanitizedAndValidationPreserved(t *testing.T) {
	s := New(nil, nil, Options{Library: failingLibrary{}})
	if _, err := s.GetSettings(); err == nil || err.Error() != "LOCAL_STORAGE_UNAVAILABLE" || strings.Contains(err.Error(), "private") {
		t.Fatalf("storage error leaked: %v", err)
	}
	valid := domain.DefaultSettings()
	invalid := valid
	invalid.Volume = 2
	if _, err := s.SetSettings(invalid); err == nil || err.Error() != "INVALID_SETTINGS" {
		t.Fatalf("validation error lost: %v", err)
	}
	queueService := New(nil, staleQueueStore{})
	queue := domain.QueueSnapshot{Entries: []domain.QueueEntry{{QueueEntryID: "entry", Track: fixtureTrack("123", "track")}}, CurrentEntryID: ptr("entry"), CurrentIndex: 0, PlayMode: "repeat-all", Revision: 1}
	if err := queueService.SaveQueue(queue); !errors.Is(err, storage.ErrStaleRevision) {
		t.Fatalf("stale revision was not preserved through service: %v", err)
	}
}

func ptr(s string) *string { return &s }
