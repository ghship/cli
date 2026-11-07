//go:build windows

package main

import (
	"errors"

	"golang.org/x/sys/windows"
)

// Simulate another process holding the executable without sharing deletion.
func withLockedBinary(path string, operation func() error) error {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err
	}
	result := operation()
	return errors.Join(result, windows.CloseHandle(handle))
}
