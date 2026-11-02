package gd

import (
	"context"
	"encoding/binary"
	"testing"
)

// A structural fixture, not native decoder or real member-rights evidence.
func TestMeasuredFLACFormatAndQuality(t *testing.T) {
	for _, tc := range []struct {
		rate, bits int
		quality    string
	}{{44100, 16, "lossless"}, {96000, 24, "hires"}} {
		t.Run(tc.quality, func(t *testing.T) {
			p := make([]byte, 8192)
			copy(p, "fLaC")
			p[4] = 128
			p[7] = 34
			binary.BigEndian.PutUint16(p[8:], 4096)
			binary.BigEndian.PutUint16(p[10:], 4096)
			samples := int64(tc.rate) * 267232 / 1000
			binary.BigEndian.PutUint64(p[18:], uint64(tc.rate)<<44|uint64(1)<<41|uint64(tc.bits-1)<<36|uint64(samples))
			rateCode := byte(9)
			sizeCode := byte(4)
			if tc.rate == 96000 {
				rateCode = 11
			}
			if tc.bits == 24 {
				sizeCode = 6
			}
			h := []byte{255, 248, 0xc0 | rateCode, 0x10 | sizeCode<<1, 0}
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
			copy(p[42:], append(h, crc))
			total := int64(12 * 1024 * 1024)
			f := fixtureClient(t, "https://m701.music.126.net/file.flac", "128", p, total, nil)
			got, err := f.c.Resolve(context.Background(), "33894312", "hires")
			if err != nil {
				t.Fatal(err)
			}
			if got.ActualFormat != "flac" || got.ActualQuality != tc.quality || got.ActualBitrate != (total-42)*8*int64(tc.rate)/samples || got.DurationMs < 267231 || got.DurationMs > 267232 {
				t.Fatalf("unmeasured metadata: %+v", got)
			}
		})
	}
}

func TestCBRInfoQualityUsesFrameBitrate(t *testing.T) {
	for _, tc := range []struct {
		bits    int
		average int64
		quality string
	}{
		{320000, 320033, "exhigh"}, {192000, 192020, "higher"},
	} {
		t.Run(tc.quality, func(t *testing.T) {
			p, _ := audioPrefix(tc.bits)
			p = p[:(144*tc.bits/44100)*32]
			const frames int64 = 10230
			total := (tc.average*frames*1152 + 8*44100 - 1) / (8 * 44100)
			copy(p[36:], "Info")
			binary.BigEndian.PutUint32(p[40:], 3)
			binary.BigEndian.PutUint32(p[44:], uint32(frames))
			binary.BigEndian.PutUint32(p[48:], uint32(total))
			f := fixtureClient(t, "https://m701.music.126.net/file.mp3", "320", p, total, nil)
			got, err := f.c.Resolve(context.Background(), "33894312", "exhigh")
			if err != nil || got.ActualBitrate != int64(tc.bits) || got.ActualQuality != tc.quality || got.ActualFormat != "mp3" {
				t.Fatal(got, err)
			}
		})
	}
}
