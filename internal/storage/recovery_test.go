package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/voici5986/otoneigo/internal/domain"
)

func mustSQL(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestLazyRecoveryPreservesOriginalSQLValues(t *testing.T) {
	s := queryStore(t)
	blob := []byte{0, 255, 128, 3}
	oversized := strings.Repeat("x", maxLibraryMetadataBytes+1)
	mustSQL(t, s, "INSERT INTO local_favorites(song_id,metadata,added_order,search_text) VALUES(?,?,?,?)", blob, blob, blob, blob)
	mustSQL(t, s, "INSERT INTO local_favorites(song_id,metadata,added_order,search_text) VALUES(NULL,?,2,'original search')", oversized)
	tracks, err := s.GetLocalFavorites()
	if err != nil || len(tracks) != 0 {
		t.Fatal(tracks, err)
	}
	var id, metadata, value, search []byte
	if err = s.db.QueryRow("SELECT original_id,original_metadata,original_value,original_search_text FROM local_record_quarantine WHERE typeof(original_id)='blob'").Scan(&id, &metadata, &value, &search); err != nil {
		t.Fatal(err)
	}
	for _, got := range [][]byte{id, metadata, value, search} {
		if !bytes.Equal(got, blob) {
			t.Fatal("original BLOB changed")
		}
	}
	var got string
	var nullID bool
	if err = s.db.QueryRow("SELECT original_id IS NULL,original_metadata FROM local_record_quarantine WHERE original_id IS NULL").Scan(&nullID, &got); err != nil || !nullID || got != oversized {
		t.Fatal("NULL or oversized value lost", err)
	}
	summary, err := s.RecoverySummary()
	if err != nil || summary.QuarantinedFavorites != 2 {
		t.Fatal(summary, err)
	}
}

func TestCurrentSchemaRecoveryIsLazyAndFillsPages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lazy.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tr := queryTrack("123", "healthy")
	for i := 1; i <= 210; i++ {
		if err = s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: fmt.Sprintf("e_%03d", i), PlayedAt: int64(i), Track: tr}); err != nil {
			t.Fatal(err)
		}
	}
	mustSQL(t, s, "UPDATE local_history SET metadata='{}' WHERE played_at>5")
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	summary, err := s.RecoverySummary()
	if err != nil || summary.QuarantinedHistory != 0 {
		t.Fatal("startup scanned history", summary, err)
	}
	page, err := s.ListLocalHistoryBefore(2, 0, "")
	if err != nil || len(page.Entries) != 2 || !page.HasMore || page.Entries[0].PlayedAt != 5 || page.Entries[1].PlayedAt != 4 {
		t.Fatal(page, err)
	}
	next, err := s.ListLocalHistoryBefore(2, 4, "e_004")
	if err != nil || len(next.Entries) != 2 || !next.HasMore || next.Entries[0].PlayedAt != 3 {
		t.Fatal(next, err)
	}
	last, err := s.ListLocalHistoryBefore(2, 2, "e_002")
	if err != nil || len(last.Entries) != 1 || last.HasMore {
		t.Fatal(last, err)
	}
	summary, err = s.RecoverySummary()
	if err != nil || summary.QuarantinedHistory != 205 {
		t.Fatal(summary, err)
	}
}

func TestRecoverySQLFailureRollsBackCopyAndDelete(t *testing.T) {
	s := queryStore(t)
	mustSQL(t, s, "INSERT INTO local_favorites(song_id,metadata,added_order) VALUES('123','{}',1)")
	mustSQL(t, s, "CREATE TRIGGER block_recovery BEFORE DELETE ON local_favorites BEGIN SELECT RAISE(ABORT,'blocked'); END")
	if _, err := s.GetLocalFavorites(); err == nil {
		t.Fatal("SQL error hidden")
	}
	var remaining, quarantined int
	if err := s.db.QueryRow("SELECT (SELECT count(*) FROM local_favorites),(SELECT count(*) FROM local_record_quarantine)").Scan(&remaining, &quarantined); err != nil || remaining != 1 || quarantined != 0 {
		t.Fatal(remaining, quarantined, err)
	}
}

func TestMigrationSQLFailureRollsBackRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollback.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	mustSQL(t, s, "INSERT INTO local_favorites(song_id,metadata,added_order) VALUES('123','{}',1)")
	mustSQL(t, s, "PRAGMA user_version=4") // Existing search columns force the later DDL to fail.
	s.Close()
	if reopened, e := Open(path); e == nil {
		reopened.Close()
		t.Fatal("SQL failure hidden")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, remaining, quarantined int
	if err = db.QueryRow("SELECT (SELECT user_version FROM pragma_user_version),(SELECT count(*) FROM local_favorites),(SELECT count(*) FROM local_record_quarantine)").Scan(&version, &remaining, &quarantined); err != nil || version != 4 || remaining != 1 || quarantined != 0 {
		t.Fatal(version, remaining, quarantined, err)
	}
}

func TestUnknownLocalMetadataSurvivesMutationsAndMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unknown.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tr := queryTrack("123", "healthy")
	if err = s.SetLocalFavorite(tr, true); err != nil {
		t.Fatal(err)
	}
	entry := domain.HistoryEntry{HistoryEntryID: "entry", PlayedAt: 5, Track: tr}
	if err = s.AddLocalHistory(entry); err != nil {
		t.Fatal(err)
	}
	if err = s.SetSettings(domain.DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	mustSQL(t, s, `UPDATE local_settings SET metadata=json_set(metadata,'$.future',json('{"value":1}'),'$.desktop.future',json('{"value":2}'),'$.resourceCache.future',json('{"value":3}'))`)
	mustSQL(t, s, `UPDATE local_favorites SET metadata=json_set(metadata,'$.future',7)`)
	mustSQL(t, s, `UPDATE local_history SET metadata=json_set(metadata,'$.track.future',8)`)
	var favorite, history string
	if err = s.db.QueryRow("SELECT metadata FROM local_favorites").Scan(&favorite); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow("SELECT metadata FROM local_history").Scan(&history); err != nil {
		t.Fatal(err)
	}
	mustSQL(t, s, "PRAGMA user_version=5")
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	settings, err := s.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	settings.Volume = .25
	if err = s.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
	var preserved bool
	if err = s.db.QueryRow(`SELECT json_extract(metadata,'$.future.value')=1 AND json_extract(metadata,'$.desktop.future.value')=2 AND json_extract(metadata,'$.resourceCache.future.value')=3 AND json_extract(metadata,'$.volume')=0.25 FROM local_settings`).Scan(&preserved); err != nil || !preserved {
		t.Fatal("unknown settings lost", err)
	}
	if _, err = s.GetLocalFavorites(); err != nil {
		t.Fatal(err)
	}
	if err = s.SetLocalFavorite(tr, true); err == nil || err.Error() != "LOCAL_METADATA_REQUIRES_NEWER_VERSION" {
		t.Fatal(err)
	}
	if err = s.AddLocalHistory(entry); err != nil {
		t.Fatal(err)
	}
	var favoriteAfter, historyAfter string
	if err = s.db.QueryRow("SELECT metadata FROM local_favorites").Scan(&favoriteAfter); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow("SELECT metadata FROM local_history").Scan(&historyAfter); err != nil {
		t.Fatal(err)
	}
	if favoriteAfter != favorite || historyAfter != history {
		t.Fatal("future metadata rewritten")
	}
	if _, err = s.SearchLocalHistory(context.Background(), "healthy", 5, 0, ""); err != nil {
		t.Fatal(err)
	}
}

func TestLocalMetadataBoundsAndTrailingJSON(t *testing.T) {
	raw, _ := json.Marshal(queryTrack("123", "healthy"))
	for _, bad := range []string{string(raw) + " {}", string(raw) + string([]byte{255}), strings.Repeat(" ", maxLibraryMetadataBytes) + string(raw)} {
		var track domain.TrackSnapshot
		if decodeLocalMetadata(bad, &track) == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}

func TestLegacyFavoriteRecoveryPrecedesOrderBackfill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v3.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetLocalFavorite(queryTrack("123", "healthy"), true); err != nil {
		t.Fatal(err)
	}
	mustSQL(t, s, "INSERT INTO local_favorites(song_id,metadata,added_order) VALUES(NULL,'{}',9)")
	for _, q := range []string{"DROP TABLE search_history", "ALTER TABLE local_favorites DROP COLUMN search_text", "ALTER TABLE local_history DROP COLUMN search_text", "DROP INDEX local_favorites_order", "ALTER TABLE local_favorites DROP COLUMN added_order", "PRAGMA user_version=3"} {
		mustSQL(t, s, q)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	tracks, err := s.GetLocalFavorites()
	if err != nil || len(tracks) != 1 {
		t.Fatal(tracks, err)
	}
	var preserved bool
	if err = s.db.QueryRow("SELECT original_id IS NULL AND original_metadata='{}' AND original_value IS NULL AND original_search_text IS NULL FROM local_record_quarantine").Scan(&preserved); err != nil || !preserved {
		t.Fatal("legacy values changed before preservation", err)
	}
}

func TestCurrentSettingsRecoveryPreservesBlobAndIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	blob := []byte{255, 0, 3}
	mustSQL(t, s, "INSERT INTO local_settings(id,metadata) VALUES(1,?)", blob)
	s.Close()
	for i := 0; i < 2; i++ {
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		got, e := s.GetSettings()
		if e != nil || got != domain.DefaultSettings() {
			t.Fatal(got, e)
		}
		var raw []byte
		var count int
		if e = s.db.QueryRow("SELECT original_metadata,(SELECT count(*) FROM local_record_quarantine) FROM local_record_quarantine WHERE source_table='local_settings'").Scan(&raw, &count); e != nil || !bytes.Equal(raw, blob) || count != 1 {
			t.Fatal("settings recovery changed original", e)
		}
		s.Close()
	}
}

func TestFavoriteUpsertPreservesMalformedOriginal(t *testing.T) {
	s := queryStore(t)
	track := queryTrack("123", "healthy")
	data, _ := encodeLocalMetadata(track)
	original := data + " {}"
	mustSQL(t, s, "INSERT INTO local_favorites(song_id,metadata,added_order) VALUES('123',?,1)", original)
	if err := s.SetLocalFavorite(track, true); err == nil {
		t.Fatal("malformed original overwritten")
	}
	var after string
	if err := s.db.QueryRow("SELECT metadata FROM local_favorites").Scan(&after); err != nil || after != original {
		t.Fatal("original changed", err)
	}
}

func TestFavoriteUpsertRejectsInvalidStoredOrderWithoutOverwrite(t *testing.T) {
	for _, order := range []any{int64(0), int64(-1), int64(9007199254740992), "invalid", []byte{0, 255}, 1.5} {
		t.Run(fmt.Sprintf("%T_%v", order, order), func(t *testing.T) {
			s := queryStore(t)
			originalTrack := queryTrack("123", "original")
			raw, _ := encodeLocalMetadata(originalTrack)
			mustSQL(t, s, "INSERT INTO local_favorites(song_id,metadata,added_order) VALUES('123',?,?)", raw, order)
			changed := originalTrack
			changed.Name = "replacement"
			if err := s.SetLocalFavorite(changed, true); err == nil {
				t.Fatal("invalid stored order accepted")
			}
			var preserved string
			if err := s.db.QueryRow("SELECT metadata FROM local_favorites").Scan(&preserved); err != nil || preserved != raw {
				t.Fatal("original metadata overwritten", err)
			}
			if _, err := s.GetLocalFavorites(); err != nil {
				t.Fatal(err)
			}
			if err := s.db.QueryRow("SELECT original_metadata FROM local_record_quarantine").Scan(&preserved); err != nil || preserved != raw {
				t.Fatal("quarantine did not preserve original metadata", err)
			}
		})
	}
}

func TestSettingsOverlayCanonicalizesKnownAliasesRecursively(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aliases.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	initial := domain.DefaultSettings()
	if err := s.SetSettings(initial); err != nil {
		t.Fatal(err)
	}
	mustSQL(t, s, `UPDATE local_settings SET metadata=json_set(metadata,'$.preferredquality','exhigh','$.desktop.lyricsfontsize',28,'$.desktop.miniplacement',json('{"Width":420,"height":176,"futurePlacement":{"keep":3}}'),'$.resourcecache',json('{"futureResource":{"keep":4}}'),'$.desktop.futureDesktop',json('{"keep":2}'),'$.futureRoot',json('{"keep":1}'))`)
	changed := initial
	changed.PreferredQuality = "lossless"
	changed.Desktop.LyricsFontSize = 32
	changed.Desktop.MiniPlacement.Width = 500
	if err := s.SetSettings(changed); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetSettings()
	if err != nil || got != changed {
		t.Fatal("aliases overrode requested known values", got, err)
	}
	var preserved, canonical bool
	if err = s.db.QueryRow(`SELECT json_extract(metadata,'$.futureRoot.keep')=1 AND json_extract(metadata,'$.desktop.futureDesktop.keep')=2 AND json_extract(metadata,'$.desktop.miniPlacement.futurePlacement.keep')=3 AND json_extract(metadata,'$.resourceCache.futureResource.keep')=4, json_type(metadata,'$.preferredquality') IS NULL AND json_type(metadata,'$.desktop.lyricsfontsize') IS NULL AND json_type(metadata,'$.desktop.miniplacement') IS NULL AND json_type(metadata,'$.resourcecache') IS NULL FROM local_settings`).Scan(&preserved, &canonical); err != nil || !preserved || !canonical {
		t.Fatal("aliases or unknown extensions mishandled", preserved, canonical, err)
	}
}

func TestSettingsAliasesDeepMergeUnknownDescendants(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "deep-aliases.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close() }()
			settings := domain.DefaultSettings()
			if err = s.SetSettings(settings); err != nil {
				t.Fatal(err)
			}
			mustSQL(t, s, `UPDATE local_settings SET metadata=json_set(metadata,'$.Desktop',json('{"miniPlacement":{"futureAlias":{"nested":1},"futureShared":{"fromAlias":3}}}'),'$.desktop.miniPlacement.futureCanonical',json('{"nested":2}'),'$.desktop.miniPlacement.futureShared',json('{"fromCanonical":4}'))`)
			if conflict {
				mustSQL(t, s, `UPDATE local_settings SET metadata=json_set(metadata,'$.Desktop.miniPlacement.futureConflict','alias','$.desktop.miniPlacement.futureConflict','canonical')`)
			}
			var before string
			if err = s.db.QueryRow("SELECT metadata FROM local_settings").Scan(&before); err != nil {
				t.Fatal(err)
			}
			settings.Volume = .4
			err = s.SetSettings(settings)
			if conflict {
				if err == nil || err.Error() != "LOCAL_METADATA_REQUIRES_NEWER_VERSION" {
					t.Fatal("unknown conflict silently discarded", err)
				}
				var after string
				if err = s.db.QueryRow("SELECT metadata FROM local_settings").Scan(&after); err != nil || after != before {
					t.Fatal("conflicted raw metadata changed", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			s.Close()
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.GetSettings()
			if err != nil || got != settings {
				t.Fatal(got, err)
			}
			var kept bool
			if err = s.db.QueryRow(`SELECT json_extract(metadata,'$.desktop.miniPlacement.futureAlias.nested')=1 AND json_extract(metadata,'$.desktop.miniPlacement.futureCanonical.nested')=2 AND json_extract(metadata,'$.desktop.miniPlacement.futureShared.fromAlias')=3 AND json_extract(metadata,'$.desktop.miniPlacement.futureShared.fromCanonical')=4 FROM local_settings`).Scan(&kept); err != nil || !kept {
				t.Fatal("unknown descendants lost", err)
			}
		})
	}
}

func TestClearLocalHistoryDeletesOnlyHistoryArchivesAtomically(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(fmt.Sprint(blocked), func(t *testing.T) {
			s := queryStore(t)
			tr := queryTrack("123", "healthy")
			if err := s.AddLocalHistory(domain.HistoryEntry{HistoryEntryID: "keep-until-clear", PlayedAt: 1, Track: tr}); err != nil {
				t.Fatal(err)
			}
			for _, source := range []string{"local_history", "local_favorites", "local_settings"} {
				mustSQL(t, s, "INSERT INTO local_record_quarantine(source_table,original_rowid,reason,original_metadata) VALUES(?,1,'test','original')", source)
			}
			if blocked {
				mustSQL(t, s, "CREATE TRIGGER block_history_archive_delete BEFORE DELETE ON local_record_quarantine WHEN old.source_table='local_history' BEGIN SELECT RAISE(ABORT,'blocked'); END")
			}
			err := s.ClearLocalHistory()
			if (err != nil) != blocked {
				t.Fatal("clear error mismatch", err)
			}
			var active, history, other int
			if err = s.db.QueryRow("SELECT (SELECT count(*) FROM local_history),(SELECT count(*) FROM local_record_quarantine WHERE source_table='local_history'),(SELECT count(*) FROM local_record_quarantine WHERE source_table!='local_history')").Scan(&active, &history, &other); err != nil {
				t.Fatal(err)
			}
			expected := 0
			if blocked {
				expected = 1
			}
			if active != expected || history != expected || other != 2 {
				t.Fatal("history clear partial or overbroad", active, history, other)
			}
		})
	}
}
