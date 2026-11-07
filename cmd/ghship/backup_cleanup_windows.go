//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func canDeferBackup(err error) bool {
	return errors.Is(err, os.ErrPermission) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

func explainRenameError(err error) error {
	if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
		return fmt.Errorf("%w; close programs using the installation and retry", err)
	}
	return err
}

// Open regular backups without allowing concurrent writes or renames, then
// delete the same verified file by handle. A mapped executable can remain
// locked until its process exits; callers may explicitly defer that cleanup.
func removeBackup(path string) error {
	before, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if before.IsDir() {
		return os.RemoveAll(path)
	}
	if !before.Mode().IsRegular() {
		return errors.New("backup is not a regular file; preserved")
	}
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(name, windows.DELETE|windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fmt.Errorf("cannot open backup exclusively: %w", err)
	}
	file := os.NewFile(uintptr(h), path)
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return errors.New("backup changed during cleanup; preserved")
	}
	deleteFile := byte(1)
	ordinaryErr := windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, (*byte)(unsafe.Pointer(&deleteFile)), 1)
	if ordinaryErr == nil {
		return nil
	}
	flags := uint32(windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS)
	if err = windows.SetFileInformationByHandle(h, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&flags)), uint32(unsafe.Sizeof(flags))); err != nil {
		return fmt.Errorf("backup removal failed: %w", errors.Join(ordinaryErr, err))
	}
	return nil
}
