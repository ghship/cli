//go:build windows

package main

import (
	"golang.org/x/sys/windows"
	"os"
)

func securityOwner(path string, info os.FileInfo) (string, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return "", err
	}
	return owner.String() + "|" + sd.String(), nil
}
