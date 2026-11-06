package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicPlaylistRequestBounds(t *testing.T) {
	for _, id := range []string{"1", "900719925474099312345678901234"} {
		for _, offset := range []int{0, 30, 990} {
			if err := ValidatePublicPlaylistRequest(id, offset); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, id := range []string{"", "0", "01", "1e3", "1.0", "1/2", strings.Repeat("1", 31)} {
		if ValidatePublicPlaylistRequest(id, 0) == nil {
			t.Fatal(id)
		}
	}
	for _, offset := range []int{-30, 1, 29, 1000, 1020} {
		if ValidatePublicPlaylistRequest("1", offset) == nil {
			t.Fatal(offset)
		}
	}
}
func TestDiscoveryJSONUsesStringIDsAndSafeProjection(t *testing.T) {
	encoded, err := json.Marshal(PublicPlaylistPage{PlaylistID: "9007199254740993", Tracks: []TrackSnapshot{}, AvailableCount: 30, NextOffset: 30})
	if err != nil || !strings.Contains(string(encoded), `"playlistId":"9007199254740993"`) || !strings.Contains(string(encoded), `"availableCount":30`) || !strings.Contains(string(encoded), `"nextOffset":30`) || !strings.Contains(string(encoded), `"tracks":[]`) {
		t.Fatal(string(encoded), err)
	}
}
