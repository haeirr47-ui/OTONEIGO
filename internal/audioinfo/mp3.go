package audioinfo

import "encoding/binary"

func mp3(p []byte, total int64, complete bool) (Info, error) {
	bad := func() (Info, error) { return Info{}, ErrInvalid }
	off := 0
	if string(p[:3]) == "ID3" {
		if len(p) < 10 || p[3] < 2 || p[3] > 4 || p[4] == 255 {
			return bad()
		}
		for _, b := range p[6:10] {
			if b&128 != 0 {
				return bad()
			}
		}
		off = 10 + (int(p[6]) << 21) + (int(p[7]) << 14) + (int(p[8]) << 7) + int(p[9])
		if p[3] == 4 && p[5]&16 != 0 {
			off += 10
		}
		allowedFlags := byte(0xc0)
		if p[3] == 3 {
			allowedFlags = 0xe0
		}
		if p[3] == 4 {
			allowedFlags = 0xf0
		}
		if p[5] & ^allowedFlags != 0 {
			return bad()
		}
		if off >= len(p) {
			return bad()
		}
		if p[3] == 4 && p[5]&16 != 0 {
			footer := p[off-10 : off]
			if string(footer[:3]) != "3DI" || string(footer[3:]) != string(p[3:10]) {
				return bad()
			}
		}
	}
	start := off
	frames := 0
	var firstBits, rate, channels int
	var frameCount, declaredBytes int64
	variable := false
	var tag string
	for off+4 <= len(p) {
		h := binary.BigEndian.Uint32(p[off:])
		if h&0xffe00000 != 0xffe00000 || h>>19&3 != 3 || h>>17&3 != 1 || h&3 == 2 {
			return bad()
		}
		bi, ri := int(h>>12&15), int(h>>10&3)
		if bi == 0 || bi == 15 || ri == 3 {
			return bad()
		}
		b := []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}[bi] * 1000
		r := []int{44100, 48000, 32000}[ri]
		ch := 2
		if h>>6&3 == 3 {
			ch = 1
		}
		if frames > 0 && (r != rate || ch != channels) {
			return bad()
		}
		if frames > 0 && b != firstBits {
			variable = true
		}
		if frames == 0 {
			firstBits = b
			rate = r
			channels = ch
		}
		n := 144*b/r + int(h>>9&1)
		if off+n > len(p) {
			if int64(len(p)) == total {
				return bad()
			}
			break
		}
		f := p[off : off+n]
		if frames == 0 {
			side := 32
			if ch == 1 {
				side = 17
			}
			pos := 4 + side
			if h>>16&1 == 0 {
				pos += 2
			}
			if pos+8 <= n && (string(f[pos:pos+4]) == "Xing" || string(f[pos:pos+4]) == "Info") {
				tag = string(f[pos : pos+4])
				flags := binary.BigEndian.Uint32(f[pos+4:])
				if flags&1 == 0 || flags & ^uint32(15) != 0 || pos+12 > n {
					return bad()
				}
				frameCount = int64(binary.BigEndian.Uint32(f[pos+8:]))
				if flags&2 != 0 {
					if pos+16 > n {
						return bad()
					}
					declaredBytes = int64(binary.BigEndian.Uint32(f[pos+12:]))
				}
			}
			if n >= 54 && string(f[36:40]) == "VBRI" {
				if tag != "" || binary.BigEndian.Uint16(f[40:]) != 1 {
					return bad()
				}
				tag = "VBRI"
				declaredBytes = int64(binary.BigEndian.Uint32(f[46:]))
				frameCount = int64(binary.BigEndian.Uint32(f[50:]))
			}
		}
		frames++
		off += n
	}
	if frames < 16 || total <= int64(start) || variable && tag == "" {
		return bad()
	}
	if int64(len(p)) == total && off != len(p) {
		return bad()
	}
	audioBytes := total - int64(start)
	if declaredBytes != 0 && declaredBytes != audioBytes {
		// Only provisional prefixes can defer the exact ID3v1 trailer check.
		// Full Inspect reads the tail and demands the exact MPEG byte count.
		if complete || int64(len(p)) == total || declaredBytes != audioBytes-128 {
			return bad()
		}
		audioBytes = declaredBytes
	}
	duration := audioBytes * 8000 / int64(firstBits)
	bits := int64(firstBits)
	if tag != "" {
		if frameCount < int64(frames) || frameCount == 0 {
			return bad()
		}
		duration = frameCount * 1152 * 1000 / int64(rate)
		if audioBytes < frameCount*96 || audioBytes > frameCount*1441 {
			return bad()
		}
		if tag == "Info" {
			expected := audioBytes * 8000 / int64(firstBits)
			delta := duration - expected
			if delta < 0 {
				delta = -delta
			}
			if variable || delta > expected/50+100 {
				return bad()
			}
		}
		// Info describes CBR: frame padding and tag/container overhead must not
		// inflate its verified frame bitrate beyond an access-policy boundary.
		// Xing/VBRI retain the measured average for variable-bitrate streams.
		if tag != "Info" {
			bits = audioBytes * 8 * int64(rate) / (frameCount * 1152)
		}
	}
	return Info{Format: "mp3", MIME: "audio/mpeg", DurationMs: duration, Bitrate: bits, SampleRate: rate, Channels: channels}, nil
}
