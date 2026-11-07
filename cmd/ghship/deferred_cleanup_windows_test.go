//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDeferredCleanupValidatesDigest(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(filepath.Dir(exe), ".ghship-backup-")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	t.Cleanup(func() { _ = os.Remove(path) })
	if _, err = f.Write([]byte("managed-old-binary")); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	args := []string{"4294967295", path, strings.Repeat("0", 64)}
	if err = runDeferredBackupCleanup(args); err == nil {
		t.Fatal("wrong digest accepted")
	}
	if _, err = os.Stat(path); err != nil {
		t.Fatalf("backup changed after rejection: %v", err)
	}
	sum := sha256.Sum256([]byte("managed-old-binary"))
	args[2] = hex.EncodeToString(sum[:])
	if err = runDeferredBackupCleanup(args); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("verified backup remains: %v", err)
	}
}

func TestDeferredCleanupPreservesFileWithCompetingHandles(t *testing.T) {
	for _, mode := range []string{"write", "rename"} {
		t.Run(mode, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			exe, err = filepath.EvalSymlinks(exe)
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.CreateTemp(filepath.Dir(exe), ".ghship-backup-")
			if err != nil {
				t.Fatal(err)
			}
			path := f.Name()
			t.Cleanup(func() { _ = os.Remove(path) })
			initial := []byte("managed-old-binary")
			if _, err = f.Write(initial); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
			name, err := windows.UTF16PtrFromString(path)
			if err != nil {
				t.Fatal(err)
			}
			access := uint32(windows.GENERIC_WRITE)
			if mode == "rename" {
				access = windows.DELETE
			}
			h, err := windows.CreateFile(name, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer windows.CloseHandle(h)
			sum := sha256.Sum256(initial)
			if err = runDeferredBackupCleanup([]string{"4294967295", path, hex.EncodeToString(sum[:])}); err == nil {
				t.Fatal("cleanup accepted competing handle")
			}
			if err = removeBackup(path); err == nil {
				t.Fatal("immediate cleanup accepted competing handle")
			}
			if mode == "write" {
				var written uint32
				if err = windows.WriteFile(h, []byte("changed"), &written, nil); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = os.Stat(path); err != nil {
				t.Fatalf("backup was removed: %v", err)
			}
			if mode == "write" {
				data, err := os.ReadFile(path)
				if err != nil || !strings.HasPrefix(string(data), "changed") {
					t.Fatalf("writer data not preserved: %q %v", data, err)
				}
			}
		})
	}
}
