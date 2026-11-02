//go:build windows

package region

import (
	"context"
	"testing"
)

func TestNativeNotificationLifecycle(t *testing.T) {
	// Register and release the actual Windows notifications without changing routes.
	cleanup, count := registerNotifications(make(chan struct{}, 1))
	cleanup()
	if count != 2 {
		t.Fatal("route/interface registration failed", count)
	}
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if e := monitorNetwork(ctx, func() { t.Error("unexpected notification") }); e != nil {
			t.Fatal(e)
		}
	}
	count = 0
	notificationContexts.Range(func(_, _ any) bool { count++; return true })
	if count != 0 {
		t.Fatal("native context registrations leaked", count)
	}
}
