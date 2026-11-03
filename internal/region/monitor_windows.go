//go:build windows

package region

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/voici5986/otoneigo/internal/crashreport"
	"golang.org/x/sys/windows"
)

var (
	ipHelper             = windows.NewLazySystemDLL("iphlpapi.dll")
	notifyRoute          = ipHelper.NewProc("NotifyRouteChange2")
	notifyInterface      = ipHelper.NewProc("NotifyIpInterfaceChange")
	cancelNotification   = ipHelper.NewProc("CancelMibChangeNotify2")
	notificationContexts sync.Map
	notificationSequence atomic.Uintptr
	// Windows callback thunks cannot be freed: allocate exactly one for all controllers.
	notificationCallback = syscall.NewCallback(func(contextID, row, kind uintptr) uintptr {
		defer crashreport.Fatal("region-network")
		if value, ok := notificationContexts.Load(contextID); ok {
			select {
			case value.(chan struct{}) <- struct{}{}:
			default:
			}
		}
		return 0
	})
)

// registerNotifications retains only a signal channel, never OS-owned row pointers.
func registerNotifications(events chan struct{}) (func(), int) {
	key := notificationSequence.Add(1)
	notificationContexts.Store(key, events)
	var handles []uintptr
	for _, proc := range []*windows.LazyProc{notifyRoute, notifyInterface} {
		if proc.Find() != nil {
			continue
		}
		var handle uintptr
		code, _, _ := proc.Call(0, notificationCallback, key, 0, uintptr(unsafe.Pointer(&handle)))
		if code == 0 {
			handles = append(handles, handle)
		}
	}
	// Never cancel from the native callback or while holding a callback lock.
	return func() {
		for _, h := range handles {
			_, _, _ = cancelNotification.Call(h)
		}
		notificationContexts.Delete(key)
	}, len(handles)
}

// Native notifications include route-only VPN/exit changes. Fingerprinting and a
// resume-gap check supplement notifications, without claiming identical exits
// for the trace service, catalog API and WebView2 media traffic.
func monitorNetwork(ctx context.Context, changed func()) error {
	events := make(chan struct{}, 1)
	cleanup, _ := registerNotifications(events)
	defer cleanup()
	fingerprint := func() string {
		var values []string
		ifaces, _ := net.Interfaces()
		for _, n := range ifaces {
			a, _ := n.Addrs()
			for _, v := range a {
				values = append(values, n.Name+":"+n.Flags.String()+":"+v.String())
			}
		}
		sort.Strings(values)
		return strings.Join(values, "|")
	}
	previous := fingerprint()
	lastTick := time.Now()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var debounce *time.Timer
	var debounceC <-chan time.Time
	defer func() {
		if debounce != nil {
			debounce.Stop()
		}
	}()
	schedule := func() {
		if debounce == nil {
			debounce = time.NewTimer(500 * time.Millisecond)
		} else {
			if !debounce.Stop() {
				select {
				case <-debounce.C:
				default:
				}
			}
			debounce.Reset(500 * time.Millisecond)
		}
		debounceC = debounce.C
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-events:
			schedule()
		case <-debounceC:
			debounceC = nil
			changed()
		case now := <-ticker.C:
			next := fingerprint()
			if next != previous || now.Sub(lastTick) > 45*time.Second {
				previous = next
				schedule()
			}
			lastTick = now
		}
	}
}
