//go:build windows

package crashreport

import "golang.org/x/sys/windows"

func isReparse(path string) bool {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return true
	}
	attributes, err := windows.GetFileAttributes(ptr)
	return err != nil || attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
