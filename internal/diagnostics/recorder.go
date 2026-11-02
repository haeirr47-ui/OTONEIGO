// Package diagnostics records a fixed metadata schema, never arbitrary errors,
// response bodies, addresses, cookies or signed URLs.
package diagnostics

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/voici5986/otoneigo/internal/domain"
)

type Event struct {
	Time             int64  `json:"time"`
	Stage            string `json:"stage"`
	SongID           string `json:"songId,omitempty"`
	Country          string `json:"country,omitempty"`
	Strategy         string `json:"strategy,omitempty"`
	Provider         string `json:"provider,omitempty"`
	Status           string `json:"status,omitempty"`
	RequestedQuality string `json:"requestedQuality,omitempty"`
	ActualQuality    string `json:"actualQuality,omitempty"`
	ActualFormat     string `json:"actualFormat,omitempty"`
	ActualBitrate    int64  `json:"actualBitrate,omitempty"`
	ErrorCode        string `json:"errorCode,omitempty"`
	ElapsedMs        int64  `json:"elapsedMs"`
}

type Recorder struct {
	mu         sync.Mutex
	file       *os.File
	path       string
	size       int64
	closed     bool
	retryAfter time.Time
	failure    error
	statusCode string
	dropped    uint64
}

const maxRecordBytes = 1024 * 1024

var errRecorderIO = errors.New("DIAGNOSTICS_IO_FAILED")

// RecorderStatus contains only fixed codes and a count, never filesystem errors.
type RecorderStatus struct {
	Code         string `json:"code,omitempty"`
	DroppedCount uint64 `json:"droppedCount"`
}

func (r *Recorder) Status() RecorderStatus {
	if r == nil {
		return RecorderStatus{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return RecorderStatus{Code: r.statusCode, DroppedCount: r.dropped}
}

func (r *Recorder) failed() {
	r.failure = errRecorderIO
	r.statusCode = errRecorderIO.Error()
	r.retryAfter = time.Now().Add(time.Second)
}

// reopen restores the active handle without truncating an existing log.
func (r *Recorder) reopen() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	r.file, r.size = f, info.Size()
	return nil
}

func (r *Recorder) rotate() error {
	// Leave the active handle usable when the previous file cannot be removed.
	if err := os.Remove(r.path + ".old"); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := r.file.Close(); err != nil {
		r.file = nil
		_ = r.reopen()
		return err
	}
	r.file = nil
	if err := os.Rename(r.path, r.path+".old"); err != nil {
		_ = r.reopen()
		return err
	}
	if err := r.reopen(); err != nil {
		// Retain the previous active log if creating its replacement fails.
		_ = os.Rename(r.path+".old", r.path)
		_ = r.reopen()
		return err
	}
	return nil
}

var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

func permitted(value string, allowed ...string) string {
	if value == "" {
		return ""
	}
	for _, item := range allowed {
		if value == item {
			return value
		}
	}
	return "unknown"
}

func sanitize(e Event) Event {
	if !domain.ValidID(e.SongID) {
		e.SongID = ""
	}
	if e.Country != "" && !countryCode.MatchString(e.Country) {
		e.Country = "unknown"
	}
	e.Stage = permitted(e.Stage, "normal-api", "compatible-api", "gd-api", "resolve", "region-detection", "cache-hit", "media-cache", "desktop", "storage")
	e.Strategy = permitted(e.Strategy, "normal", "region-header", "cdn-compat", "cache")
	e.Provider = permitted(e.Provider, "netease-direct", "gd-netease")
	e.Status = permitted(e.Status, "full", "trial", "unavailable", "unauthorized", "unknown")
	qualities := []string{"standard", "higher", "exhigh", "lossless", "hires", "jyeffect", "sky", "jymaster", "unknown"}
	e.RequestedQuality = permitted(e.RequestedQuality, qualities...)
	e.ActualQuality = permitted(e.ActualQuality, qualities...)
	e.ActualFormat = permitted(e.ActualFormat, "mp3", "flac", "aac", "m4a", "wav", "ogg", "opus", "unknown")
	// Keep provider-supplied bitrate bounded even when its response is malformed.
	if e.ActualBitrate < 0 || e.ActualBitrate > 100_000_000 {
		e.ActualBitrate = 0
	}
	code := permitted(e.ErrorCode, "LOCAL_RECORDS_QUARANTINED", "TRAY_UNAVAILABLE", "GD_BUDGET_EXHAUSTED", "EMPTY_URL", "UNAUTHORIZED", "RESOLVE_TIMEOUT", "GD_METADATA_UNVERIFIED", "GD_QUALITY_UNAVAILABLE", "GD_UNSUPPORTED_FORMAT", "QUALITY_NOT_ALLOWED", "GD_REQUEST_FAILED", "GD_DURATION_UNVERIFIED", "GD_MEDIA_UNVERIFIED", "GD_API_REDIRECT", "PROVIDER_REDIRECT_REJECTED", "COMPATIBILITY_ENDPOINT_REJECTED", "COMPATIBILITY_COOLDOWN", "CANCELLED", "TIMEOUT", "LOGIN_REQUIRED", "RATE_LIMITED", "INVALID_SONG_ID", "INVALID_QUALITY", "MISMATCHED_SONG_ID", "UNTRUSTED_MEDIA_URL", "INVALID_PROVIDER_RESPONSE", "INVALID_PROVIDER_REQUEST", "REQUEST_ENCODING_FAILED", "PROVIDER_UNAVAILABLE", "PROVIDER_RESPONSE_TOO_LARGE", "NO_AUDIO_URL", "REGION_DETECTION_FAILED", "REGION_SUSPECTED", "TRIAL_ONLY", "GD_UNAVAILABLE", "GD_RATE_LIMITED", "MEDIA_FORMAT_UNSUPPORTED", "CDN_LOAD_FAILED", "UNKNOWN")
	if code == "unknown" {
		code = "UNKNOWN"
	}
	e.ErrorCode = code
	if e.ElapsedMs < 0 {
		e.ElapsedMs = 0
	}
	return e
}

func Open(dir string) (*Recorder, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "playback.jsonl")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &Recorder{file: f, path: p, size: info.Size()}, nil
}

func (r *Recorder) Record(e Event) {
	if r == nil {
		return
	}
	e = sanitize(e)
	e.Time = time.Now().UnixMilli()
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	b = append(b, '\n')
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	if !r.retryAfter.IsZero() && time.Now().Before(r.retryAfter) {
		r.dropped++
		return
	}
	if r.file == nil {
		if err := r.reopen(); err != nil {
			r.failed()
			r.dropped++
			return
		}
	}
	if r.size+int64(len(b)) > maxRecordBytes {
		if err := r.rotate(); err != nil {
			r.failed()
			r.dropped++
			return
		}
	}
	n, err := r.file.Write(b)
	r.size += int64(n)
	if err != nil {
		r.failed()
		r.dropped++
		_ = r.file.Close()
		r.file = nil
		return
	}
	r.retryAfter = time.Time{}
	r.statusCode = ""
}

func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.failure
	}
	r.closed = true
	if r.file != nil {
		if err := r.file.Close(); err != nil {
			r.failure = errRecorderIO
			r.statusCode = errRecorderIO.Error()
		}
		r.file = nil
	}
	return r.failure
}
