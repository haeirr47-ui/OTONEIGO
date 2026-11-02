package audioinfo

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"testing"
)

func flacFixture() []byte {
	p := make([]byte, 8192)
	copy(p, "fLaC")
	p[4] = 128
	p[7] = 34
	binary.BigEndian.PutUint16(p[8:], 4096)
	binary.BigEndian.PutUint16(p[10:], 4096)
	binary.BigEndian.PutUint64(p[18:], uint64(96000)<<44|uint64(1)<<41|uint64(23)<<36|uint64(96000*10))
	h := []byte{255, 248, 0xc0, 0x1c, 0}
	var crc byte
	for _, b := range h {
		crc ^= b
		for j := 0; j < 8; j++ {
			if crc&128 != 0 {
				crc = crc<<1 ^ 7
			} else {
				crc <<= 1
			}
		}
	}
	h = append(h, crc)
	copy(p[42:], h)
	return p
}
func wavFixture() []byte {
	p := make([]byte, 44+44100*4)
	copy(p, "RIFF")
	binary.LittleEndian.PutUint32(p[4:], uint32(len(p)-8))
	copy(p[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(p[16:], 16)
	binary.LittleEndian.PutUint16(p[20:], 1)
	binary.LittleEndian.PutUint16(p[22:], 2)
	binary.LittleEndian.PutUint32(p[24:], 44100)
	binary.LittleEndian.PutUint32(p[28:], 176400)
	binary.LittleEndian.PutUint16(p[32:], 4)
	binary.LittleEndian.PutUint16(p[34:], 16)
	copy(p[36:], "data")
	binary.LittleEndian.PutUint32(p[40:], 176400)
	return p
}
func mp3Fixture() []byte {
	p := make([]byte, 417*100)
	for off := 0; off < len(p); off += 417 {
		binary.BigEndian.PutUint32(p[off:], 0xfffb9000)
	}
	return p
}
func TestFormatsAndBoundedCompleteInspection(t *testing.T) {
	for _, tc := range []struct {
		name       string
		p          []byte
		duration   int64
		bits, rate int
	}{{"flac", flacFixture(), 10000, 24, 96000}, {"wav", wavFixture(), 1000, 16, 44100}, {"mp3", mp3Fixture(), 2606, 0, 44100}} {
		t.Run(tc.name, func(t *testing.T) {
			i, err := Inspect(bytes.NewReader(tc.p), int64(len(tc.p)))
			if err != nil || i.Format != tc.name || i.DurationMs != tc.duration || i.BitsPerSample != tc.bits || i.SampleRate != tc.rate {
				t.Fatal(i, err)
			}
			if _, err = Inspect(bytes.NewReader(tc.p[:len(tc.p)-1]), int64(len(tc.p))); err == nil {
				t.Fatal("accepted truncated file")
			}
			if _, err = Inspect(bytes.NewReader(append(tc.p, 0)), int64(len(tc.p))); err == nil {
				t.Fatal("accepted size mismatch")
			}
		})
	}
}
func TestVBRFrameCountsAndAverage(t *testing.T) {
	for _, tag := range []string{"Xing", "Info", "VBRI"} {
		t.Run(tag, func(t *testing.T) {
			p := mp3Fixture()
			if tag == "VBRI" {
				copy(p[36:], tag)
				binary.BigEndian.PutUint16(p[40:], 1)
				binary.BigEndian.PutUint32(p[46:], uint32(len(p)))
				binary.BigEndian.PutUint32(p[50:], 100)
			} else {
				copy(p[36:], tag)
				binary.BigEndian.PutUint32(p[40:], 3)
				binary.BigEndian.PutUint32(p[44:], 100)
				binary.BigEndian.PutUint32(p[48:], uint32(len(p)))
			}
			i, err := InspectPrefix(p, int64(len(p)))
			if err != nil || i.DurationMs != 2612 {
				t.Fatal(i, err)
			}
			if tag != "Info" && i.Bitrate != 127706 {
				t.Fatal("not measured average", i)
			}
			if tag == "VBRI" {
				binary.BigEndian.PutUint32(p[46:], 42)
			} else {
				binary.BigEndian.PutUint32(p[48:], 42)
			}
			if _, err := InspectPrefix(p, int64(len(p))); err == nil {
				t.Fatal("accepted false byte count")
			}
		})
	}
}
func TestRejectMalformed(t *testing.T) {
	cases := [][]byte{[]byte("<html>not audio</html>"), []byte("OggSunsupported"), flacFixture()[:42], wavFixture()[:20]}
	p := flacFixture()
	p[45] ^= 2
	cases = append(cases, p)
	p = wavFixture()
	binary.LittleEndian.PutUint32(p[28:], 1)
	cases = append(cases, p)
	for n, p := range cases {
		if _, err := InspectPrefix(p, int64(len(p))); err == nil {
			t.Fatalf("case %d accepted", n)
		}
	}
	if _, err := InspectPrefix(make([]byte, PrefixLimit+1), MaxSize); err == nil {
		t.Fatal("prefix unbounded")
	}
	if _, err := InspectPrefix(flacFixture(), MaxSize+1); err == nil {
		t.Fatal("size unbounded")
	}
}

func TestVariableBitrateRequiresFrameCount(t *testing.T) {
	var p []byte
	for n := 0; n < 100; n++ {
		bits, index := 128000, 9
		if n%2 != 0 {
			bits, index = 192000, 11
		}
		frame := make([]byte, 144*bits/44100)
		binary.BigEndian.PutUint32(frame, 0xfffb0000|uint32(index)<<12)
		p = append(p, frame...)
	}
	if _, err := InspectPrefix(p, int64(len(p))); err == nil {
		t.Fatal("accepted unknown VBR duration")
	}
	copy(p[36:], "Xing")
	binary.BigEndian.PutUint32(p[40:], 3)
	binary.BigEndian.PutUint32(p[44:], 100)
	binary.BigEndian.PutUint32(p[48:], uint32(len(p)))
	i, err := InspectPrefix(p, int64(len(p)))
	if err != nil || i.DurationMs != 2612 || i.Bitrate != int64(len(p))*8*44100/(100*1152) {
		t.Fatal(i, err)
	}
}

func TestCBRInfoPreservesFrameBitrateDespiteOverhead(t *testing.T) {
	for _, tc := range []struct {
		bits, index      int
		containerAverage int64
	}{
		{320000, 14, 320033}, {192000, 11, 192020},
	} {
		t.Run(strconv.Itoa(tc.bits), func(t *testing.T) {
			const frameCount int64 = 10000
			frameSize := 144 * tc.bits / 44100
			p := make([]byte, frameSize*32)
			for off := 0; off < len(p); off += frameSize {
				binary.BigEndian.PutUint32(p[off:], 0xfffb0000|uint32(tc.index)<<12)
			}
			// Select a valid declared extent whose container-derived average is
			// just above the nominal bitrate, as in real CBR Info files.
			total := (tc.containerAverage*frameCount*1152 + 8*44100 - 1) / (8 * 44100)
			if average := total * 8 * 44100 / (frameCount * 1152); average != tc.containerAverage {
				t.Fatalf("invalid fixture average: %d", average)
			}
			copy(p[36:], "Info")
			binary.BigEndian.PutUint32(p[40:], 3)
			binary.BigEndian.PutUint32(p[44:], uint32(frameCount))
			binary.BigEndian.PutUint32(p[48:], uint32(total))
			i, err := InspectPrefix(p, total)
			if err != nil || i.Bitrate != int64(tc.bits) || i.DurationMs != frameCount*1152*1000/44100 {
				t.Fatal(i, err)
			}
		})
	}
}
