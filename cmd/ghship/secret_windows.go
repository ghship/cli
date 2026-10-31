package main

import (
	"golang.org/x/sys/windows"
	"os"
	"unsafe"
)

func cryptSecret(b []byte, decrypt bool) ([]byte, error) {
	input := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var result windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &result)
	} else {
		err = windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &result)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(result.Data))))
	return append([]byte(nil), unsafe.Slice(result.Data, int(result.Size))...), nil
}
func protectSecret(b []byte) ([]byte, error)                   { return cryptSecret(b, false) }
func unprotectSecret(b []byte) ([]byte, error)                 { return cryptSecret(b, true) }
func checkSecretMode(st os.FileInfo) error                     { return nil } // DPAPI restricts decrypting to this Windows user.
func prepareConfigDir(path string, expected os.FileInfo) error { return nil }
