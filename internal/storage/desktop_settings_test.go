package storage

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/voici5986/otoneigo/internal/domain"
)

func TestSchemaFiveDesktopMigrationPreservesLocalData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v5.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	settings := domain.DefaultSettings()
	settings.RegionMode, settings.Volume, settings.CloseToTray, settings.GDEnabled = "off", .37, true, true
	settings.AudioCache.Enabled = false
	if err = s.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveQueue(snapshot(19)); err != nil {
		t.Fatal(err)
	}
	track := snapshot(19).Entries[0].Track
	if err = s.SetLocalFavorite(track, true); err != nil {
		t.Fatal(err)
	}
	history := domain.HistoryEntry{HistoryEntryID: "keep", Track: track, PlayedAt: 7}
	if err = s.AddLocalHistory(history); err != nil {
		t.Fatal(err)
	}
	playlist, err := s.CreateLocalPlaylist("keep playlist")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]json.RawMessage
	if err = json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	delete(legacy, "desktop")
	data, err = json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE local_settings SET metadata=? WHERE id=1", string(data)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA user_version=5"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var version int
	if err = s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 7 {
		t.Fatal(version, err)
	}
	if got, e := s.GetSettings(); e != nil || got != settings {
		t.Fatalf("settings changed: %+v %v", got, e)
	}
	if got, e := s.LoadQueue(); e != nil || got == nil || !reflect.DeepEqual(*got, snapshot(19)) {
		t.Fatal("queue changed", got, e)
	}
	if got, e := s.GetLocalFavorites(); e != nil || !reflect.DeepEqual(got, []domain.TrackSnapshot{track}) {
		t.Fatal("favorites changed", got, e)
	}
	if got, e := s.ListLocalHistory(10, 0); e != nil || !reflect.DeepEqual(got, []domain.HistoryEntry{history}) {
		t.Fatal("history changed", got, e)
	}
	if got, e := s.GetLocalPlaylist(playlist.PlaylistID); e != nil || !reflect.DeepEqual(got, playlist) {
		t.Fatal("playlist changed", got, e)
	}
	// The defaults are committed, rather than synthesized only by a service read.
	var persisted domain.LocalSettings
	var raw string
	if err = s.db.QueryRow("SELECT metadata FROM local_settings WHERE id=1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal([]byte(raw), &persisted); err != nil || persisted.Desktop != domain.DefaultDesktopSettings() {
		t.Fatal(persisted, err)
	}
}

func TestSchemaFiveDesktopMigrationPreservesDamagedOrUnknownSettings(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		damaged    bool
	}{
		{"invalid desktop", `{"preferredQuality":"exhigh","regionMode":"off","volume":0.3,"desktop":{"lyricsFontSize":65}}`, true},
		{"invalid settings", `{"preferredQuality":"exhigh","regionMode":"off","volume":2}`, true},
		{"unknown field", `{"preferredQuality":"exhigh","regionMode":"off","volume":0.3,"futurePreference":"keep"}`, false},
		{"unknown desktop field", `{"preferredQuality":"exhigh","regionMode":"off","volume":0.3,"desktop":{"futurePreference":"keep"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "v5.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.SaveQueue(snapshot(8)); err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.Exec("INSERT INTO local_settings VALUES(1,?)", tc.data); err != nil {
				t.Fatal(err)
			}
			if _, err = s.db.Exec("PRAGMA user_version=5"); err != nil {
				t.Fatal(err)
			}
			s.Close()
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			got, err := s.GetSettings()
			if err != nil {
				t.Fatal(err)
			}
			report, err := s.RecoverySummary()
			if err != nil || report.SettingsRecovered != tc.damaged {
				t.Fatal(report, err)
			}
			if tc.damaged {
				if got != domain.DefaultSettings() {
					t.Fatal("safe defaults not restored")
				}
				var raw string
				if err = s.db.QueryRow("SELECT original_metadata FROM local_record_quarantine WHERE source_table='local_settings'").Scan(&raw); err != nil || raw != tc.data {
					t.Fatal("original settings not preserved", err)
				}
			} else {
				if got.RegionMode != "off" || got.Volume != .3 {
					t.Fatal("known settings changed")
				}
				var kept string
				if err = s.db.QueryRow("SELECT COALESCE(json_extract(metadata,'$.futurePreference'),json_extract(metadata,'$.desktop.futurePreference')) FROM local_settings WHERE id=1").Scan(&kept); err != nil || kept != "keep" {
					t.Fatal("unknown field lost", err)
				}
			}
			queue, err := s.LoadQueue()
			if err != nil || queue == nil || !reflect.DeepEqual(*queue, snapshot(8)) {
				t.Fatal("queue changed", err)
			}
		})
	}
}
