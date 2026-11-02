package domain

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"unicode/utf8"
)

type WindowPlacement struct {
	Valid  bool `json:"valid"`
	X      int  `json:"x"`
	Y      int  `json:"y"`
	Width  int  `json:"width"`
	Height int  `json:"height"`
}
type DesktopSettings struct {
	MiniAlwaysOnTop    bool            `json:"miniAlwaysOnTop"`
	LyricsAlwaysOnTop  bool            `json:"lyricsAlwaysOnTop"`
	LyricsLocked       bool            `json:"lyricsLocked"`
	LyricsFontSize     int             `json:"lyricsFontSize"`
	LyricsOpacity      float64         `json:"lyricsOpacity"`
	NotifyTrackChanges bool            `json:"notifyTrackChanges"`
	TaskbarProgress    bool            `json:"taskbarProgress"`
	MiniPlacement      WindowPlacement `json:"miniPlacement"`
	LyricsPlacement    WindowPlacement `json:"lyricsPlacement"`
}

func DefaultDesktopSettings() DesktopSettings {
	return DesktopSettings{MiniAlwaysOnTop: true, LyricsAlwaysOnTop: true, LyricsFontSize: 28, LyricsOpacity: .9, TaskbarProgress: true, MiniPlacement: WindowPlacement{Width: 420, Height: 176}, LyricsPlacement: WindowPlacement{Width: 820, Height: 156}}
}
func NormalizeDesktopSettings(s DesktopSettings) DesktopSettings {
	if s == (DesktopSettings{}) {
		return DefaultDesktopSettings()
	}
	return s
}
func ValidateWindowPlacement(p WindowPlacement) error {
	if p.X < -100000 || p.X > 100000 || p.Y < -100000 || p.Y > 100000 || p.Width < 100 || p.Width > 8192 || p.Height < 80 || p.Height > 8192 {
		return errors.New("INVALID_DESKTOP_PLACEMENT")
	}
	return nil
}
func ValidateDesktopSettings(s DesktopSettings) error {
	// Schema-five settings did not carry this section. Only the complete zero
	// value receives legacy defaults; malformed partial settings stay errors.
	if s == (DesktopSettings{}) {
		return nil
	}
	if s.LyricsFontSize < 16 || s.LyricsFontSize > 64 || !finiteRange(s.LyricsOpacity, .3, 1) || ValidateWindowPlacement(s.MiniPlacement) != nil || ValidateWindowPlacement(s.LyricsPlacement) != nil {
		return errors.New("INVALID_DESKTOP_SETTINGS")
	}
	return nil
}

type DesktopSnapshot struct {
	Epoch         int64           `json:"epoch"`
	Sequence      int64           `json:"sequence"`
	Ready         bool            `json:"ready"`
	Closing       bool            `json:"closing"`
	QueueEntryID  string          `json:"queueEntryId"`
	SongID        string          `json:"songId"`
	Title         string          `json:"title"`
	Artist        string          `json:"artist"`
	Album         string          `json:"album"`
	CoverRef      string          `json:"coverRef"`
	Playback      string          `json:"playback"`
	PositionMs    int64           `json:"positionMs"`
	DurationMs    int64           `json:"durationMs"`
	Volume        float64         `json:"volume"`
	Favorite      bool            `json:"favorite"`
	CanPrevious   bool            `json:"canPrevious"`
	CanNext       bool            `json:"canNext"`
	CacheHit      bool            `json:"cacheHit"`
	Provider      string          `json:"provider"`
	Quality       string          `json:"quality"`
	CurrentLyric  string          `json:"currentLyric"`
	NextLyric     string          `json:"nextLyric"`
	Translation   string          `json:"translation"`
	LyricProgress float64         `json:"lyricProgress"`
	Error         string          `json:"error"`
	Options       DesktopSettings `json:"options"`
}
type DesktopCommand struct {
	Epoch   int64   `json:"epoch"`
	Command string  `json:"command"`
	SongID  string  `json:"songId"`
	Value   float64 `json:"value"`
}
type DesktopNativeState struct {
	MainVisible          bool   `json:"mainVisible"`
	MainMinimized        bool   `json:"mainMinimized"`
	MiniVisible          bool   `json:"miniVisible"`
	LyricsVisible        bool   `json:"lyricsVisible"`
	LyricsLocked         bool   `json:"lyricsLocked"`
	TaskbarAvailable     bool   `json:"taskbarAvailable"`
	TaskbarState         string `json:"taskbarState"`
	TaskbarPositionMs    int64  `json:"taskbarPositionMs"`
	TaskbarDurationMs    int64  `json:"taskbarDurationMs"`
	NotificationRequests int64  `json:"notificationRequests"`
	NotificationAccepted int64  `json:"notificationAccepted"`
	LastError            string `json:"lastError"`
}

func finiteRange(v, min, max float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= min && v <= max
}
func ValidateDesktopSnapshot(s DesktopSnapshot) error {
	bad := errors.New("INVALID_DESKTOP_SNAPSHOT")
	if s.Epoch < 1 || s.Epoch > 9007199254740991 || s.Sequence < 1 || s.Sequence > 9007199254740991 || (s.SongID != "" && !ValidID(s.SongID)) || (s.QueueEntryID != "" && !entryID.MatchString(s.QueueEntryID)) || !ValidCover(s.CoverRef) || s.PositionMs < 0 || s.PositionMs > 86400000 || s.DurationMs < 0 || s.DurationMs > 86400000 || !finiteRange(s.Volume, 0, 1) || !finiteRange(s.LyricProgress, 0, 1) {
		return bad
	}
	if s.Playback != "paused" && s.Playback != "playing" && s.Playback != "loading" {
		return bad
	}
	if s.Provider != "" && s.Provider != "netease-direct" && s.Provider != "gd-netease" {
		return bad
	}
	for _, p := range []struct {
		s string
		n int
	}{{s.Title, 1024}, {s.Artist, 1024}, {s.Album, 1024}, {s.Quality, 128}, {s.Error, 1024}, {s.CurrentLyric, 4096}, {s.NextLyric, 4096}, {s.Translation, 4096}} {
		if len(p.s) > p.n || !utf8.ValidString(p.s) || strings.ContainsAny(p.s, "\x00") {
			return bad
		}
	}
	b, e := json.Marshal(s)
	if e != nil || len(b) > 65536 {
		return bad
	}
	return nil
}
func ValidateDesktopCommand(c DesktopCommand) error {
	if c.Epoch < 1 || c.Epoch > 9007199254740991 || !finiteRange(c.Value, 0, 86400000) {
		return errors.New("INVALID_DESKTOP_COMMAND")
	}
	switch c.Command {
	case "play", "pause", "toggle-play", "previous", "next", "toggle-lyrics-lock", "toggle-mini-topmost", "toggle-lyrics-topmost":
	case "seek", "toggle-favorite":
		if !ValidID(c.SongID) {
			return errors.New("INVALID_DESKTOP_COMMAND")
		}
	case "volume":
		if c.Value > 1 {
			return errors.New("INVALID_DESKTOP_COMMAND")
		}
	default:
		return errors.New("INVALID_DESKTOP_COMMAND")
	}
	return nil
}
