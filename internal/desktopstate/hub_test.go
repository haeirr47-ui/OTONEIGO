package desktopstate

import (
	"encoding/json"
	"github.com/voici5986/otoneigo/internal/domain"
	"strings"
	"sync"
	"testing"
)

func TestHubStringPublicationBoundedBeforeDecode(t *testing.T) {
	h := New(domain.DefaultDesktopSettings())
	s := projection()
	raw, _ := json.Marshal(s)
	if _, _, e := h.Publish("main", string(raw)); e != nil {
		t.Fatal("frontend JSON-string protocol rejected", e)
	}
	if _, _, e := h.Publish("main", strings.Repeat(" ", 65537)); e == nil {
		t.Fatal("oversized raw payload accepted")
	}
	s.Sequence++
	raw, _ = json.Marshal(s)
	if _, _, e := h.Publish("main", string(raw)+" {}"); e == nil {
		t.Fatal("trailing document accepted")
	}
}

func projection() domain.DesktopSnapshot {
	return domain.DesktopSnapshot{Epoch: 1, Sequence: 1, Ready: true, QueueEntryID: "entry1", SongID: "123", Title: "Song", Playback: "playing", DurationMs: 10000, Options: domain.DefaultDesktopSettings()}
}
func TestHubPublicationEpochPrivacyAndSettings(t *testing.T) {
	settings := domain.DefaultDesktopSettings()
	h := New(settings)
	s := projection()
	s.Options.NotifyTrackChanges = true
	if _, _, e := h.Publish("mini", s); e == nil {
		t.Fatal("aux publisher accepted")
	}
	if _, notify, e := h.Publish("main", s); e != nil || !notify {
		t.Fatal(e)
	}
	if h.Snapshot().Options != settings {
		t.Fatal("uncommitted options accepted")
	}
	if _, _, e := h.Publish("main", s); e == nil {
		t.Fatal("reordered sequence accepted")
	}
	b, _ := json.Marshal(s)
	var data map[string]any
	_ = json.Unmarshal(b, &data)
	data["sequence"] = float64(2)
	data["audioUrl"] = "secret"
	if _, _, e := h.Publish("main", data); e == nil {
		t.Fatal("unknown credential field accepted")
	}
	epoch := h.Navigate()
	if h.Snapshot().Ready || h.Snapshot().Sequence != 0 {
		t.Fatal("navigation kept ready")
	}
	s.Sequence = 2
	if _, _, e := h.Publish("main", s); e == nil {
		t.Fatal("old epoch accepted")
	}
	s.Epoch = epoch
	if _, notify, e := h.Publish("main", s); e != nil || notify {
		t.Fatal("navigation caused repeated notification", e)
	}
	h.Close()
	s.Sequence++
	if _, _, e := h.Publish("main", s); e == nil {
		t.Fatal("closing accepted publish")
	}
}
func TestHubCommandsAndNotificationDecisions(t *testing.T) {
	h := New(domain.DefaultDesktopSettings())
	c := domain.DesktopCommand{Epoch: 1, Command: "toggle-play"}
	if h.Command(c) == nil {
		t.Fatal("not ready")
	}
	s := projection()
	_, notify, _ := h.Publish("main", s)
	if !notify {
		t.Fatal("first playing not consumed")
	}
	if h.Command(c) != nil {
		t.Fatal("ready command")
	}
	c.Command = "seek"
	c.SongID = "124"
	if h.Command(c) == nil {
		t.Fatal("wrong song seek")
	}
	c.SongID = "123"
	c.Value = 10001
	if h.Command(c) == nil {
		t.Fatal("out of duration seek")
	}
	c.Value = 500
	if h.Command(c) != nil {
		t.Fatal("valid seek rejected")
	}
	for _, state := range []string{"playing", "paused", "playing"} {
		s.Sequence++
		s.Playback = state
		if _, notify, e := h.Publish("main", s); e != nil || notify {
			t.Fatal("tick/pause/resume notified", e)
		}
	}
	s.QueueEntryID = "entry2"
	s.Sequence++
	if _, notify, e := h.Publish("main", s); e != nil || !notify {
		t.Fatal("new entry did not notify", e)
	}
	h.Close()
	c.Command = "toggle-play"
	if h.Command(c) == nil {
		t.Fatal("closing command accepted")
	}
}
func TestHubConcurrentEpochs(t *testing.T) {
	h := New(domain.DefaultDesktopSettings())
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				h.Navigate()
				h.Snapshot()
				h.Settings(domain.DefaultDesktopSettings())
			}
		})
	}
	wg.Wait()
	if h.Epoch() != 801 {
		t.Fatal(h.Epoch())
	}
}

func TestHubClosePublishesMonotonicTerminalStateOnce(t *testing.T) {
	for _, sequence := range []int64{1, terminalSequence - 1} {
		h := New(domain.DefaultDesktopSettings())
		s := projection()
		s.Sequence = sequence
		if _, _, err := h.Publish("main", s); err != nil {
			t.Fatal(err)
		}
		h.Close()
		closed := h.Snapshot()
		if !closed.Closing || closed.Ready || closed.Epoch != s.Epoch || closed.Sequence != sequence+1 {
			t.Fatal("terminal projection not monotonic", closed)
		}
		if err := domain.ValidateDesktopSnapshot(closed); err != nil {
			t.Fatal("terminal projection not JS safe", err)
		}
		h.Close()
		if h.Snapshot() != closed {
			t.Fatal("repeated close changed terminal projection")
		}
	}
	h := New(domain.DefaultDesktopSettings())
	s := projection()
	s.Sequence = terminalSequence
	if _, _, err := h.Publish("main", s); err == nil {
		t.Fatal("publisher consumed reserved terminal sequence")
	}
}
func TestClampPlacementNegativeMonitorsAndDisconnectedDisplay(t *testing.T) {
	areas := []domain.WindowPlacement{{Valid: true, X: 0, Y: 0, Width: 1920, Height: 1040}, {Valid: true, X: -1600, Y: -100, Width: 1600, Height: 900}}
	p := domain.WindowPlacement{Valid: true, X: -1500, Y: -50, Width: 420, Height: 176}
	if got := ClampPlacement(p, areas); got != p {
		t.Fatal(got)
	}
	p.X = -8000
	p.Y = 9000
	got := ClampPlacement(p, areas)
	if got.X < 0 || got.Y < 0 || got.X+got.Width > 1920 || got.Y+got.Height > 1040 {
		t.Fatal(got)
	}
	got = ClampPlacement(domain.WindowPlacement{Width: 420, Height: 176}, areas)
	if got.X != 750 || got.Y != 432 {
		t.Fatal(got)
	}
}
