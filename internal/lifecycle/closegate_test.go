package lifecycle

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcknowledgementReentersCloseOnce(t *testing.T) {
	var closes atomic.Int32
	var gate *CloseGate
	gate = NewCloseGate(time.Hour, func() {
		closes.Add(1)
		if allow, _ := gate.Request(); !allow {
			t.Error("acknowledged close blocked")
		}
	})
	defer gate.Stop()
	allow, token := gate.Request()
	if allow || token == "" {
		t.Fatal("first close must prepare")
	}
	if allow, duplicate := gate.Request(); allow || duplicate != "" {
		t.Fatal("duplicate close started another attempt")
	}
	gate.Acknowledge("stale")
	gate.Acknowledge("")
	if closes.Load() != 0 {
		t.Fatal("invalid acknowledgement closed window")
	}
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() { gate.Acknowledge(token) })
	}
	group.Wait()
	if closes.Load() != 1 {
		t.Fatal("close must run once")
	}
}

func TestFallbackAndShutdown(t *testing.T) {
	closed := make(chan struct{}, 1)
	gate := NewCloseGate(10*time.Millisecond, func() { closed <- struct{}{} })
	_, token := gate.Request()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("fallback did not close")
	}
	gate.Acknowledge(token)
	gate.Stop()
	select {
	case <-closed:
		t.Fatal("late ack repeated close")
	default:
	}

	stopped := NewCloseGate(10*time.Millisecond, func() { closed <- struct{}{} })
	_, token = stopped.Request()
	stopped.Stop()
	stopped.Acknowledge(token)
	select {
	case <-closed:
		t.Fatal("shutdown requested another close")
	case <-time.After(30 * time.Millisecond):
	}
}
