//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
