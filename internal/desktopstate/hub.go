// Package desktopstate owns the bounded presentation projection, never playback.
package desktopstate

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/voici5986/otoneigo/internal/domain"
	"io"
	"sync"
)

type Hub struct {
	mu       sync.Mutex
	snapshot domain.DesktopSnapshot
	notified string
}

// Keep one JS-safe sequence available for the terminal closing projection.
const terminalSequence int64 = 9007199254740991

func New(options domain.DesktopSettings) *Hub {
	return &Hub{snapshot: domain.DesktopSnapshot{Epoch: 1, Playback: "paused", Options: options}}
}
func (h *Hub) Epoch() int64                     { h.mu.Lock(); defer h.mu.Unlock(); return h.snapshot.Epoch }
func (h *Hub) Snapshot() domain.DesktopSnapshot { h.mu.Lock(); defer h.mu.Unlock(); return h.snapshot }
func (h *Hub) Navigate() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.snapshot.Closing {
		return h.snapshot.Epoch
	}
	h.snapshot.Epoch++
	h.snapshot.Sequence = 0
	h.snapshot.Ready = false
	return h.snapshot.Epoch
}
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.snapshot.Closing {
		return
	}
	h.snapshot.Sequence++
	h.snapshot.Closing = true
	h.snapshot.Ready = false
}
func (h *Hub) Settings(s domain.DesktopSettings) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.snapshot.Options = s
}

// Publish uses the framework sender, not a claimed role in the payload. Reject
// unknown properties before decoding so credentials cannot enter the projection.
func (h *Hub) Publish(sender string, data any) (domain.DesktopSnapshot, bool, error) {
	if sender != "main" {
		return domain.DesktopSnapshot{}, false, errors.New("DESKTOP_SENDER_REJECTED")
	}
	var b []byte
	var e error
	if raw, ok := data.(string); ok {
		b = []byte(raw)
	} else {
		b, e = json.Marshal(data)
	}
	if e != nil || len(b) > 65536 {
		return domain.DesktopSnapshot{}, false, errors.New("INVALID_DESKTOP_SNAPSHOT")
	}
	var s domain.DesktopSnapshot
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if dec.Decode(&s) != nil || dec.Decode(new(any)) != io.EOF || domain.ValidateDesktopSnapshot(s) != nil {
		return s, false, errors.New("INVALID_DESKTOP_SNAPSHOT")
	}
	if s.Sequence >= terminalSequence {
		return s, false, errors.New("INVALID_DESKTOP_SNAPSHOT")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.snapshot.Closing || s.Closing || s.Epoch != h.snapshot.Epoch || s.Sequence <= h.snapshot.Sequence {
		return s, false, errors.New("STALE_DESKTOP_SNAPSHOT")
	}
	s.Options = h.snapshot.Options
	h.snapshot = s
	notify := s.Ready && s.Playback == "playing" && s.QueueEntryID != "" && h.notified != s.QueueEntryID
	if notify {
		h.notified = s.QueueEntryID
	}
	return s, notify, nil
}
func (h *Hub) Command(c domain.DesktopCommand) error {
	if e := domain.ValidateDesktopCommand(c); e != nil {
		return e
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.snapshot
	if s.Closing || !s.Ready || c.Epoch != s.Epoch {
		return errors.New("DESKTOP_NOT_READY")
	}
	if (c.Command == "seek" || c.Command == "toggle-favorite") && c.SongID != s.SongID {
		return errors.New("STALE_DESKTOP_TRACK")
	}
	if c.Command == "seek" && c.Value > float64(s.DurationMs) {
		return errors.New("INVALID_DESKTOP_COMMAND")
	}
	return nil
}

// ClampPlacement uses DIP work areas, including negative monitor origins.
func ClampPlacement(p domain.WindowPlacement, areas []domain.WindowPlacement) domain.WindowPlacement {
	if len(areas) == 0 {
		return p
	}
	best := areas[0]
	maxArea := -1
	for _, a := range areas {
		w := min(p.X+p.Width, a.X+a.Width) - max(p.X, a.X)
		h := min(p.Y+p.Height, a.Y+a.Height) - max(p.Y, a.Y)
		overlap := max(0, w) * max(0, h)
		if overlap > maxArea {
			maxArea = overlap
			best = a
		}
	}
	p.Width = min(p.Width, best.Width)
	p.Height = min(p.Height, best.Height)
	if !p.Valid {
		p.X = best.X + (best.Width-p.Width)/2
		p.Y = best.Y + (best.Height-p.Height)/2
	} else {
		p.X = max(best.X, min(p.X, best.X+best.Width-p.Width))
		p.Y = max(best.Y, min(p.Y, best.Y+best.Height-p.Height))
	}
	p.Valid = true
	return p
}
