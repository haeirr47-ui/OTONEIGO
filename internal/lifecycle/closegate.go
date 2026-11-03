package lifecycle

import (
	"strconv"
	"sync"
	"time"

	"github.com/voici5986/otoneigo/internal/crashreport"
)

// CloseGate allows one bounded frontend persistence attempt before native close.
// Callbacks run outside its lock so close can reenter Request.
type CloseGate struct {
	mu      sync.Mutex
	timeout time.Duration
	close   func()
	timer   *time.Timer
	attempt uint64
	token   string
	ready   bool
	stopped bool
}

func NewCloseGate(timeout time.Duration, close func()) *CloseGate {
	return &CloseGate{timeout: timeout, close: close}
}

// Started latches normal-close intent. A later tray preference must not turn
// an already frozen persistence handshake into a hidden, inert window.
func (g *CloseGate) Started() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.attempt > 0
}

// Request returns whether close may proceed, and a token only for a new attempt.
// Duplicate native requests remain cancelled until acknowledgement or timeout.
func (g *CloseGate) Request() (allow bool, token string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ready || g.stopped {
		return true, ""
	}
	if g.token != "" {
		return false, ""
	}
	g.attempt++
	token = strconv.FormatUint(g.attempt, 10)
	g.token = token
	g.timer = time.AfterFunc(g.timeout, func() {
		defer crashreport.Fatal("close-gate")
		g.Acknowledge(token)
	})
	return false, token
}

func (g *CloseGate) Acknowledge(token string) {
	g.mu.Lock()
	if g.stopped || g.ready || token == "" || token != g.token {
		g.mu.Unlock()
		return
	}
	g.ready = true
	if g.timer != nil {
		g.timer.Stop()
	}
	g.mu.Unlock()
	g.close()
}

// Stop prevents a pending fallback from requesting close after shutdown.
func (g *CloseGate) Stop() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stopped = true
	if g.timer != nil {
		g.timer.Stop()
	}
}
