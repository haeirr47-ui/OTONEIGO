package domain

import (
	"strings"
	"testing"
)

func TestPlaylistBoundaryValidation(t *testing.T) {
	if name, err := NormalizePlaylistName("  我的歌单  "); err != nil || name != "我的歌单" {
		t.Fatal(name, err)
	}
	for _, name := range []string{" ", "a\x00b", "a\nb", strings.Repeat("曲", 129), string([]byte{255})} {
		if _, err := NormalizePlaylistName(name); err == nil {
			t.Fatalf("accepted invalid name %q", name)
		}
	}
	track := TrackSnapshot{NeteaseSongID: "9007199254740993", Name: "song"}
	if ValidatePlaylistTracks([]TrackSnapshot{track}) != nil {
		t.Fatal("string ID rejected")
	}
	if ValidatePlaylistTracks([]TrackSnapshot{track, track}) == nil {
		t.Fatal("duplicate ID accepted")
	}
	track.CoverRef = "https://m7.music.126.net/song.mp3?token=secret"
	if ValidatePlaylistTracks([]TrackSnapshot{track}) == nil {
		t.Fatal("media URL accepted")
	}
	for _, id := range []string{"", "../playlist", strings.Repeat("a", 129)} {
		if ValidatePlaylistID(id) == nil {
			t.Fatal("invalid ID accepted")
		}
	}
	for _, revision := range []int64{0, -1, MaxPlaylistInteger + 1} {
		if ValidatePlaylistRevision(revision) == nil {
			t.Fatal("unsafe revision accepted")
		}
	}
}
