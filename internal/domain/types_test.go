package domain

import (
	"strings"
	"testing"
)

func TestCoverOnlyBoundedImages(t *testing.T) {
	for _, ref := range []string{"https://p1.music.126.net/a.mp3", "https://p1.music.126.net/a.jpg?token=x", "https://p1.music.126.net/" + strings.Repeat("x", 4096) + ".jpg"} {
		if ValidCover(ref) {
			t.Fatal("unsafe cover accepted")
		}
	}
	if !ValidCover("https://p1.music.126.net/a.jpg") {
		t.Fatal("image rejected")
	}
}
