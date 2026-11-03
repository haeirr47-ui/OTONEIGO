//go:build !windows

package crashreport

func isReparse(string) bool { return false }
