//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func startDeferredBackupCleanup(binary, backup, hash string) error {
	cmd := exec.Command(binary)
	cmd.Env = append(os.Environ(), "GHSHIP_INTERNAL_BACKUP_CLEANUP=1", "GHSHIP_CLEANUP_PARENT="+strconv.Itoa(os.Getpid()), "GHSHIP_CLEANUP_PATH="+backup, "GHSHIP_CLEANUP_HASH="+hash)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// The helper waits until the original process no longer maps the old image.
// It only deletes the exact regular file in the new executable's directory
// when its digest matches the previously verified managed executable.
func runDeferredBackupCleanup(args []string) error {
	if len(args) != 3 {
		return errors.New("invalid cleanup arguments")
	}
	pid, err := strconv.ParseUint(args[0], 10, 32)
	if err != nil || pid == 0 {
		return errors.New("invalid cleanup parent")
	}
	backup, err := filepath.Abs(args[1])
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Dir(backup), filepath.Dir(exe)) || !strings.HasPrefix(filepath.Base(backup), ".ghship-backup-") || len(args[2]) != 64 {
		return errors.New("invalid cleanup target")
	}
	if _, err = hex.DecodeString(args[2]); err != nil {
		return errors.New("invalid cleanup digest")
	}
	parent, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err == nil {
		defer windows.CloseHandle(parent)
		status, waitErr := windows.WaitForSingleObject(parent, 120000)
		if waitErr != nil || status != windows.WAIT_OBJECT_0 {
			return errors.New("cleanup parent still running")
		}
	} else if !errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return err
	}
	if err = noLinks(backup); err != nil {
		return err
	}
	before, err := os.Lstat(backup)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !before.Mode().IsRegular() {
		return errors.New("cleanup target is not a regular file")
	}
	name, err := windows.UTF16PtrFromString(backup)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(name, windows.DELETE|windows.GENERIC_READ,
		windows.FILE_SHARE_READ,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(h), backup)
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return errors.New("cleanup target changed")
	}
	data, err := io.ReadAll(io.LimitReader(file, 64<<20+1))
	if err != nil || len(data) > 64<<20 {
		return errors.New("cannot verify cleanup target")
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != args[2] {
		return errors.New("cleanup digest mismatch")
	}
	deleteFile := byte(1)
	return windows.SetFileInformationByHandle(h, windows.FileDispositionInfo, (*byte)(unsafe.Pointer(&deleteFile)), 1)
}
