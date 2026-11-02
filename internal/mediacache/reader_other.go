//go:build !windows

package mediacache

import "os"

func openStreamReader(path string) (*os.File, error) { return os.Open(path) }
