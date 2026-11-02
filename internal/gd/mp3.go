package gd

import "github.com/voici5986/otoneigo/internal/audioinfo"

func mp3(sample []byte, total int64) (int64, int64, bool) {
	info, err := audioinfo.InspectPrefix(sample, total)
	return info.DurationMs, info.Bitrate, err == nil && info.Format == "mp3"
}
