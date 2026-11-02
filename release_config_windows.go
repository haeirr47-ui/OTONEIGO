//go:build windows && !p0debug

package main

func debugBrowserArgs() []string { return nil }
func debugToolsEnabled() bool    { return false }

func gdEnabledForBuild() bool { return gdReleaseValidated }
