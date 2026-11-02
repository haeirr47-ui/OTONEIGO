//go:build windows

package mediacache

import "golang.org/x/sys/windows"

func freeBytes(root string) (uint64, bool) {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return 0, false
	}
	var available, total, free uint64
	if windows.GetDiskFreeSpaceEx(p, &available, &total, &free) != nil {
		return 0, false
	}
	return available, true
}
