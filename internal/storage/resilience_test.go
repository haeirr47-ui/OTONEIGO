package storage

import (
	"path/filepath"
	"testing"

	"github.com/voici5986/otoneigo/internal/domain"
)

// Startup must isolate logical record damage without making healthy data unavailable.
func TestStartupSurvivesLogicalRecordDamage(t *testing.T) {
	for _, kind := range []string{"history_v4", "settings_v5", "settings_v6", "unknown_settings_v5"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "records.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			track := queryTrack("123", "healthy favorite")
			if err = s.SetLocalFavorite(track, true); err != nil {
				t.Fatal(err)
			}
			if err = s.SetSettings(domain.DefaultSettings()); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "history_v4":
				if err = s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: "bad", PlayedAt: 1, Track: track}); err != nil {
					t.Fatal(err)
				}
				for _, q := range []string{"UPDATE local_history SET metadata='{}'", "DROP TABLE search_history", "ALTER TABLE local_favorites DROP COLUMN search_text", "ALTER TABLE local_history DROP COLUMN search_text", "PRAGMA user_version=4"} {
					if _, err = s.db.Exec(q); err != nil {
						t.Fatal(err)
					}
				}
			case "settings_v5", "settings_v6":
				if _, err = s.db.Exec("UPDATE local_settings SET metadata='{}'"); err != nil {
					t.Fatal(err)
				}
				if kind == "settings_v5" {
					if _, err = s.db.Exec("PRAGMA user_version=5"); err != nil {
						t.Fatal(err)
					}
				}
			case "unknown_settings_v5":
				if _, err = s.db.Exec(`UPDATE local_settings SET metadata=json_set(metadata,'$.futurePreference','keep')`); err != nil {
					t.Fatal(err)
				}
				if _, err = s.db.Exec("PRAGMA user_version=5"); err != nil {
					t.Fatal(err)
				}
			}
			s.Close()
			s, err = Open(path)
			if err != nil {
				t.Fatalf("logical record prevented startup: %v", err)
			}
			defer s.Close()
			favorites, err := s.GetLocalFavorites()
			if err != nil || len(favorites) != 1 {
				t.Fatal("healthy favorite unavailable", err)
			}
			settings, err := s.GetSettings()
			if err != nil || settings != domain.DefaultSettings() {
				t.Fatal("safe settings unavailable", err)
			}
		})
	}
}
