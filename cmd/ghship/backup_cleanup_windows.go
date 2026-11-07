//go:build windows

package main

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A running executable can be renamed on Windows but ordinary deletion fails
// while its image is mapped. POSIX disposition removes the name while the
// image remains available to the running process until it exits.
func removeBackup(path string) error {
	if err := os.RemoveAll(path); err == nil {
		return nil
	} else {
		before, statErr := os.Lstat(path)
		if statErr != nil || !before.Mode().IsRegular() {
			return fmt.Errorf("%w; backup stat: %v", err, statErr)
		}
		name, statErr := windows.UTF16PtrFromString(path)
		if statErr != nil {
			return fmt.Errorf("%w; backup path: %v", err, statErr)
		}
		h, statErr := windows.CreateFile(name, windows.DELETE|windows.FILE_READ_ATTRIBUTES,
			windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
			nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if statErr != nil {
			return fmt.Errorf("%w; backup open: %v", err, statErr)
		}
		file := os.NewFile(uintptr(h), path)
		defer file.Close()
		after, statErr := file.Stat()
		if statErr != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
			return fmt.Errorf("%w; backup identity check: %v", err, statErr)
		}
		flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS)
		if statErr = windows.SetFileInformationByHandle(h, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags))); statErr != nil {
			return fmt.Errorf("%w; backup POSIX removal: %v", err, statErr)
		}
		return nil
	}
}
