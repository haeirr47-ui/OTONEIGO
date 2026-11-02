//go:build !windows

package region

import "context"

func monitorNetwork(ctx context.Context, changed func()) error { <-ctx.Done(); return nil }
