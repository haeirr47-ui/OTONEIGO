//go:build windows && p0debug

package main

import "os"

// Only the explicitly tagged acceptance binary exposes CDP. Production builds
// do not compile this code or open a debugging listener.
func debugBrowserArgs() []string { return []string{"--remote-debugging-port=9225"} }
func debugToolsEnabled() bool    { return true }

func gdEnabledForBuild() bool { return gdReleaseValidated || os.Getenv("OTONEIGO_P0_GD_ENABLE") == "1" }
