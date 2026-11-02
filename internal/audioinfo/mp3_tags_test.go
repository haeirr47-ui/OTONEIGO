package audioinfo

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func id3Fixture(size int) []byte {
	p := make([]byte, size+10)
	copy(p, "ID3")
	p[3] = 4
	for i := 9; i >= 6; i-- {
		p[i] = byte(size & 127)
		size >>= 7
	}
	return append(p, mp3Fixture()...)
}

func TestMP3BoundedTagsAndTrailer(t *testing.T) {
	for _, tag := range []string{"", "Xing", "Info", "VBRI"} {
		t.Run("ID3v1/"+tag, func(t *testing.T) {
			p := mp3Fixture()
			if tag == "VBRI" {
				copy(p[36:], tag)
				binary.BigEndian.PutUint16(p[40:], 1)
				binary.BigEndian.PutUint32(p[46:], uint32(len(p)))
				binary.BigEndian.PutUint32(p[50:], 100)
			} else if tag != "" {
				copy(p[36:], tag)
				binary.BigEndian.PutUint32(p[40:], 3)
				binary.BigEndian.PutUint32(p[44:], 100)
				binary.BigEndian.PutUint32(p[48:], uint32(len(p)))
			}
			trailer := make([]byte, 128)
			copy(trailer, "TAG")
			p = append(p, trailer...)
			if _, err := Inspect(bytes.NewReader(p), int64(len(p))); err != nil {
				t.Fatal("valid ID3v1 trailer rejected", err)
			}
			p[len(p)-128] = 'X'
			if _, err := Inspect(bytes.NewReader(p), int64(len(p))); err == nil {
				t.Fatal("arbitrary trailing data accepted")
			}
		})
	}
	p := id3Fixture(32768)
	if _, err := Inspect(bytes.NewReader(p), int64(len(p))); err != nil {
		t.Fatal("bounded 32KiB ID3 tag rejected", err)
	}
	p = id3Fixture(PrefixLimit)
	if _, err := Inspect(bytes.NewReader(p), int64(len(p))); err == nil {
		t.Fatal("unbounded tag accepted")
	}
}

func TestMP3TrailerBeyondPrefixRemainsStrict(t *testing.T) {
	p := bytes.Repeat(mp3Fixture(), 10)
	copy(p[36:], "Xing")
	binary.BigEndian.PutUint32(p[40:], 3)
	binary.BigEndian.PutUint32(p[44:], 1000)
	binary.BigEndian.PutUint32(p[48:], uint32(len(p)))
	trailer := make([]byte, 128)
	copy(trailer, "TAG")
	p = append(p, trailer...)
	if _, err := InspectPrefix(p[:PrefixLimit], int64(len(p))); err != nil {
		t.Fatal("provisional trailer candidate rejected", err)
	}
	if _, err := Inspect(bytes.NewReader(p), int64(len(p))); err != nil {
		t.Fatal("complete tail check rejected valid ID3v1", err)
	}
	p[len(p)-128] = 'X'
	if _, err := Inspect(bytes.NewReader(p), int64(len(p))); err == nil {
		t.Fatal("accepted unconfirmed trailer beyond prefix")
	}
	for n := 1; n < 4; n++ {
		if _, err := Inspect(bytes.NewReader(make([]byte, n)), int64(n)); err == nil {
			t.Fatal("accepted short input")
		}
	}
}

func TestMP3ID3FooterAndReservedFlags(t *testing.T) {
	p := id3Fixture(32768)
	// ID3v2.4 footer follows the declared tag body.
	footer := append([]byte("3DI"), p[3:10]...)
	p[5] = 16
	footer[5] = 16
	p = append(p[:32778], append(footer, p[32778:]...)...)
	if _, err := Inspect(bytes.NewReader(p), int64(len(p))); err != nil {
		t.Fatal("valid ID3 footer rejected", err)
	}
	p[32778] = 'X'
	if _, err := Inspect(bytes.NewReader(p), int64(len(p))); err == nil {
		t.Fatal("malformed footer accepted")
	}
	p = id3Fixture(0)
	p[5] = 1
	if _, err := Inspect(bytes.NewReader(p), int64(len(p))); err == nil {
		t.Fatal("reserved ID3 flags accepted")
	}
}
