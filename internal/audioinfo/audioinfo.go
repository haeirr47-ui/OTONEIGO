// Package audioinfo performs bounded structural inspection, not audio decoding.
package audioinfo

import (
	"encoding/binary"
	"errors"
	"io"
)

const PrefixLimit = 256 * 1024
const MaxSize int64 = 512 * 1024 * 1024

var ErrUnsupported = errors.New("unsupported audio format")
var ErrInvalid = errors.New("unverified audio structure")

type Info struct {
	Format        string
	MIME          string
	DurationMs    int64
	Bitrate       int64
	SampleRate    int
	Channels      int
	BitsPerSample int
	Lossless      bool
}

// Inspect reads at most PrefixLimit prefix bytes and a 128-byte MP3 trailer.
// It verifies the declared file extent
// with one byte at EOF; callers remain responsible for transfer/hash integrity.
func Inspect(r io.ReaderAt, total int64) (Info, error) {
	if total <= 0 || total > MaxSize {
		return Info{}, ErrInvalid
	}
	n := total
	if n > PrefixLimit {
		n = PrefixLimit
	}
	p := make([]byte, int(n))
	if got, err := r.ReadAt(p, 0); got != len(p) || err != nil && err != io.EOF {
		return Info{}, ErrInvalid
	}
	var end [1]byte
	if got, _ := r.ReadAt(end[:], total-1); got != 1 {
		return Info{}, ErrInvalid
	}
	if got, _ := r.ReadAt(end[:], total); got != 0 {
		return Info{}, ErrInvalid
	}
	// A bounded ID3v1 trailer is metadata, not MPEG frame bytes. Read it even
	// when the audio prefix does not reach EOF, so Xing sizes stay strict.
	if total >= 128 && (string(p[:3]) == "ID3" || p[0] == 255 && p[1]&224 == 224) {
		var tail [128]byte
		if got, err := r.ReadAt(tail[:], total-128); got != 128 || err != nil && err != io.EOF {
			return Info{}, ErrInvalid
		}
		if string(tail[:3]) == "TAG" {
			total -= 128
			if int64(len(p)) > total {
				p = p[:int(total)]
			}
		}
	}
	return inspectPrefix(p, total, true)
}

func InspectPrefix(p []byte, total int64) (Info, error) {
	if total <= 0 || total > MaxSize || len(p) < 4 || len(p) > PrefixLimit || int64(len(p)) > total {
		return Info{}, ErrInvalid
	}
	// Prefix inspection is provisional; full Inspect verifies any trailer.
	if int64(len(p)) == total && len(p) >= 128 && string(p[len(p)-128:len(p)-125]) == "TAG" && (string(p[:3]) == "ID3" || p[0] == 255 && p[1]&224 == 224) {
		p = p[:len(p)-128]
		total -= 128
	}
	return inspectPrefix(p, total, false)
}
func inspectPrefix(p []byte, total int64, complete bool) (Info, error) {
	if total <= 0 || total > MaxSize || len(p) < 4 || len(p) > PrefixLimit || int64(len(p)) > total {
		return Info{}, ErrInvalid
	}
	var i Info
	var err error
	switch {
	case string(p[:4]) == "fLaC":
		i, err = flac(p, total)
	case string(p[:4]) == "RIFF":
		i, err = wav(p, total)
	case string(p[:3]) == "ID3" || p[0] == 255 && p[1]&224 == 224:
		i, err = mp3(p, total, complete)
	default:
		return Info{}, ErrUnsupported
	}
	if err != nil || i.DurationMs <= 0 || i.DurationMs > 86400000 || i.Bitrate <= 0 {
		return Info{}, ErrInvalid
	}
	return i, nil
}

func flac(p []byte, total int64) (Info, error) {
	bad := func() (Info, error) { return Info{}, ErrInvalid }
	if len(p) < 42 || p[4]&127 != 0 || p[5] != 0 || p[6] != 0 || p[7] != 34 {
		return bad()
	}
	s := p[8:42]
	minBlock, maxBlock := int(binary.BigEndian.Uint16(s)), int(binary.BigEndian.Uint16(s[2:]))
	minFrame := int(s[4])<<16 | int(s[5])<<8 | int(s[6])
	maxFrame := int(s[7])<<16 | int(s[8])<<8 | int(s[9])
	v := binary.BigEndian.Uint64(s[10:18])
	rate := int(v >> 44)
	channels := int(v>>41&7) + 1
	bits := int(v>>36&31) + 1
	samples := int64(v & 0xfffffffff)
	if minBlock < 16 || maxBlock < minBlock || rate < 8000 || rate > 384000 || bits < 8 || bits > 32 || samples == 0 || minFrame > maxFrame && maxFrame != 0 {
		return bad()
	}
	off := 4
	for {
		if off+4 > len(p) {
			return bad()
		}
		last := p[off]&128 != 0
		kind := p[off] & 127
		n := int(p[off+1])<<16 | int(p[off+2])<<8 | int(p[off+3])
		if kind == 127 || off != 4 && kind == 0 || n > len(p)-off-4 {
			return bad()
		}
		off += 4 + n
		if last {
			break
		}
	}
	// A real frame sync and nonreserved block/rate/channel/sample-size codes must
	// follow metadata. STREAMINFO alone is insufficient evidence of audio.
	if off+6 > len(p) || p[off] != 255 || p[off+1]&254 != 248 || p[off+2]>>4 == 0 || p[off+2]&15 == 15 || p[off+3]&1 != 0 || p[off+3]>>4 > 10 || p[off+3]>>1&7 == 3 || p[off+3]>>1&7 == 7 {
		return bad()
	}
	if !flacHeader(p[off:], rate, channels, bits, minBlock, maxBlock) {
		return bad()
	}
	raw := samples * int64(channels) * int64(bits) / 8
	minFrames := (samples + int64(maxBlock) - 1) / int64(maxBlock)
	minimumFrameBytes := int64(minFrame)
	if minimumFrameBytes < 9 {
		minimumFrameBytes = 9
	}
	if total <= int64(off+6) || total-int64(off) > raw+raw/8+65536 || total-int64(off) < minFrames*minimumFrameBytes || maxFrame > 0 && total-int64(off) > ((samples+int64(minBlock)-1)/int64(minBlock))*int64(maxFrame) {
		return bad()
	}
	duration := samples * 1000 / int64(rate)
	return Info{Format: "flac", MIME: "audio/flac", DurationMs: duration, Bitrate: (total - int64(off)) * 8 * int64(rate) / samples, SampleRate: rate, Channels: channels, BitsPerSample: bits, Lossless: true}, nil
}

func flacHeader(p []byte, rate, channels, bits, minBlock, maxBlock int) bool {
	code := int(p[3] >> 4)
	ch := code + 1
	if code >= 8 {
		ch = 2
	}
	if ch != channels {
		return false
	}
	sizes := []int{0, 8, 12, 0, 16, 20, 24, 0}
	if s := sizes[p[3]>>1&7]; s != 0 && s != bits {
		return false
	}
	pos := 4
	first := p[pos]
	pos++
	count := 0
	if first&128 != 0 {
		for mask := byte(128); first&mask != 0 && mask != 0; mask >>= 1 {
			count++
		}
		if count < 2 || count > 7 {
			return false
		}
		for j := 1; j < count; j++ {
			if pos >= len(p) || p[pos]&192 != 128 {
				return false
			}
			pos++
		}
	}
	read := func() (int, bool) {
		if pos >= len(p) {
			return 0, false
		}
		v := int(p[pos])
		pos++
		return v, true
	}
	bc := p[2] >> 4
	block := 0
	switch {
	case bc == 1:
		block = 192
	case bc >= 2 && bc <= 5:
		block = 576 << uint(bc-2)
	case bc == 6:
		v, ok := read()
		if !ok {
			return false
		}
		block = v + 1
	case bc == 7:
		a, ok := read()
		if !ok {
			return false
		}
		b, ok := read()
		if !ok {
			return false
		}
		block = a*256 + b + 1
	case bc >= 8:
		block = 256 << uint(bc-8)
	}
	if block < minBlock || block > maxBlock {
		return false
	}
	rc := p[2] & 15
	rates := []int{rate, 88200, 176400, 192000, 8000, 16000, 22050, 24000, 32000, 44100, 48000, 96000}
	frameRate := 0
	if rc < 12 {
		frameRate = rates[rc]
	} else if rc == 12 {
		v, ok := read()
		if !ok {
			return false
		}
		frameRate = v * 1000
	} else {
		a, ok := read()
		if !ok {
			return false
		}
		b, ok := read()
		if !ok {
			return false
		}
		frameRate = a*256 + b
		if rc == 14 {
			frameRate *= 10
		}
	}
	if frameRate != rate || pos >= len(p) {
		return false
	}
	var crc byte
	for _, b := range p[:pos] {
		crc ^= b
		for j := 0; j < 8; j++ {
			if crc&128 != 0 {
				crc = crc<<1 ^ 7
			} else {
				crc <<= 1
			}
		}
	}
	return crc == p[pos]
}

func wav(p []byte, total int64) (Info, error) {
	bad := func() (Info, error) { return Info{}, ErrInvalid }
	if len(p) < 12 || string(p[8:12]) != "WAVE" || int64(binary.LittleEndian.Uint32(p[4:8]))+8 != total {
		return bad()
	}
	var rate, channels, bits, align, byteRate int
	var encoding uint16
	found := false
	for off := 12; off+8 <= len(p); {
		n := int64(binary.LittleEndian.Uint32(p[off+4 : off+8]))
		start := int64(off + 8)
		if start+n > total {
			return bad()
		}
		switch string(p[off : off+4]) {
		case "fmt ":
			if found || n < 16 || start+n > int64(len(p)) {
				return bad()
			}
			s := p[off+8:]
			encoding = binary.LittleEndian.Uint16(s)
			channels = int(binary.LittleEndian.Uint16(s[2:]))
			rate = int(binary.LittleEndian.Uint32(s[4:]))
			byteRate = int(binary.LittleEndian.Uint32(s[8:]))
			align = int(binary.LittleEndian.Uint16(s[12:]))
			bits = int(binary.LittleEndian.Uint16(s[14:]))
			found = true
			if encoding != 1 && encoding != 3 || channels < 1 || channels > 8 || rate < 8000 || rate > 384000 || bits != 8 && bits != 16 && bits != 24 && bits != 32 && bits != 64 || encoding == 3 && bits != 32 && bits != 64 || encoding == 1 && bits == 64 || align != channels*bits/8 || byteRate != rate*align {
				return bad()
			}
		case "data":
			if !found || n == 0 || n%int64(align) != 0 || start+int64(align) > int64(len(p)) {
				return bad()
			}
			return Info{Format: "wav", MIME: "audio/wav", DurationMs: n * 1000 / int64(byteRate), Bitrate: int64(byteRate) * 8, SampleRate: rate, Channels: channels, BitsPerSample: bits, Lossless: encoding == 1}, nil
		}
		next := start + n + (n & 1)
		if next > int64(len(p)) {
			return bad()
		}
		off = int(next)
	}
	return bad()
}
