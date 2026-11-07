package domain

import "testing"

func TestEntityKindsAndDetailValidation(t *testing.T) {
	for _, kind := range []string{EntityAlbum, EntityArtist, EntityPlaylist} {
		if !ValidEntitySearchKind(kind) {
			t.Fatal(kind)
		}
	}
	for _, kind := range []string{"song", "", "ALBUM"} {
		if ValidEntitySearchKind(kind) {
			t.Fatal(kind)
		}
	}
	if !ValidEntityDetailKind(EntityAlbum) || !ValidEntityDetailKind(EntityArtist) || ValidEntityDetailKind(EntityPlaylist) {
		t.Fatal("detail kind boundary")
	}

	track := TrackSnapshot{NeteaseSongID: "123456789", Name: "song", Artists: []Artist{{Name: "artist"}}, DurationMs: 1000}
	valid := EntityDetail{Kind: EntityAlbum, ID: "9007199254740993", Name: "album", Tracks: []TrackSnapshot{track}, Total: 1}
	if ValidateEntityDetail(valid) != nil {
		t.Fatal("valid detail rejected")
	}
	invalid := []EntityDetail{
		{Kind: "song", ID: valid.ID, Name: valid.Name, Tracks: valid.Tracks, Total: 1},
		{Kind: EntityAlbum, ID: "0", Name: valid.Name, Tracks: valid.Tracks, Total: 1},
		{Kind: EntityAlbum, ID: valid.ID, Name: " ", Tracks: valid.Tracks, Total: 1},
		{Kind: EntityAlbum, ID: valid.ID, Name: valid.Name, Description: string(make([]byte, 4097)), Tracks: valid.Tracks, Total: 1},
		{Kind: EntityAlbum, ID: valid.ID, Name: valid.Name, CoverRef: "https://evil.test/a.jpg?q=1", Tracks: valid.Tracks, Total: 1},
		{Kind: EntityAlbum, ID: valid.ID, Name: valid.Name, Tracks: valid.Tracks},
		{Kind: EntityAlbum, ID: valid.ID, Name: valid.Name, Tracks: []TrackSnapshot{{}}, Total: 1},
	}
	for i, d := range invalid {
		if ValidateEntityDetail(d) == nil {
			t.Fatalf("invalid detail %d accepted", i)
		}
	}
}
