package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// The committed master is a build input: `wails3 generate icons` reads it and the
// resulting .ico is embedded in the executable. Every other gate only counts
// RT_ICON entries, so a hand-edited PNG would ship unnoticed. This test is what
// makes the "rendered by cmd/appicon" claim in main.go and docs/delivery/packaging.md true
// rather than asserted; a Go release that changes the encoder also shows up here,
// because docs/delivery/source-provenance.json records the byte hash of the same file.
func TestCommittedIconIsRendererOutput(t *testing.T) {
	path := filepath.Join("..", "..", "packaging", "windows", "appicon.png")
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read committed icon: %v", err)
	}
	var rendered bytes.Buffer
	if err := png.Encode(&rendered, render(1024)); err != nil {
		t.Fatalf("encode rendered icon: %v", err)
	}
	if !bytes.Equal(rendered.Bytes(), committed) {
		t.Fatalf("%s is not this renderer's output (committed sha256 %s, rendered %s); regenerate with `go run ./cmd/appicon`",
			path, digest(committed), digest(rendered.Bytes()))
	}
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
