//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

func protectSecret(b []byte) ([]byte, error)   { return b, nil }
func unprotectSecret(b []byte) ([]byte, error) { return b, nil }
func checkSecretMode(st os.FileInfo) error {
	if st.Mode().Perm()&0077 != 0 {
		return errors.New("credential permissions must exclude group and other users")
	}
	return nil
}

func prepareConfigDir(path string, expected os.FileInfo) error {
	f, e := os.Open(path)
	if e != nil {
		return errors.New("configuration directory unavailable")
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !os.SameFile(expected, st) || !st.IsDir() {
		return errors.New("configuration directory changed; preserved")
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return errors.New("configuration directory must belong to current user; preserved")
	}
	if st.Mode().Perm()&0022 != 0 {
		return errors.New("configuration directory is writable by other users; preserved")
	}
	if st.Mode().Perm() != 0700 {
		if e = f.Chmod(0700); e != nil {
			return errors.New("cannot secure configuration directory")
		}
	}
	st, e = f.Stat()
	if e != nil || st.Mode().Perm() != 0700 {
		return errors.New("configuration directory permissions could not be secured")
	}
	return nil
}
