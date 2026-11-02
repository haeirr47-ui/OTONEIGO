//go:build windows

package mediacache

import (
	"golang.org/x/sys/windows"
	"os"
)

// The download can rename the growing part while an HTTP response is reading
// it. Delete sharing permits that rename on Windows; the request pin still
// prevents cache eviction until this handle is closed.
func openStreamReader(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}
