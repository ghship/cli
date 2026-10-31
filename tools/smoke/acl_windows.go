//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// withDeniedDirectoryCreate temporarily changes the DACL of the test-owned
// directory only. The original descriptor is restored even when the probe or
// the operation fails. A real access-denied probe prevents false positives.
func withDeniedDirectoryCreate(parent string, operation func() error) (result error) {
	original, err := windows.GetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	originalText := original.String()
	dacl, _, err := original.DACL()
	if err != nil {
		return err
	}
	control, _, err := original.Control()
	if err != nil {
		return err
	}
	flags := windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION
	if control&windows.SE_DACL_PROTECTED != 0 {
		flags = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	defer func() {
		err := windows.SetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT, windows.SECURITY_INFORMATION(flags), nil, nil, dacl, nil)
		if err == nil {
			var restored *windows.SECURITY_DESCRIPTOR
			restored, err = windows.GetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
			if err == nil && restored.String() != originalText {
				err = errors.New("restored test ACL differs from original")
			}
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("restore test ACL: %w", err))
		}
	}()
	// AD is FILE_ADD_SUBDIRECTORY on a directory; deny only the current user.
	cmd := exec.Command("icacls", parent, "/deny", "*"+user.User.Sid.String()+":(AD)")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("set test ACL: %w: %s", err, output)
	}
	denied, err := windows.GetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	deniedText := denied.String()
	probe := filepath.Join(parent, ".ghship-acl-probe")
	if _, err := os.Lstat(probe); !os.IsNotExist(err) {
		return errors.New("ACL probe destination exists or is inaccessible")
	}
	if err := os.Mkdir(probe, 0700); err == nil {
		_ = os.Remove(probe)
		return errors.New("Windows ACL did not deny directory creation")
	} else if !errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("ACL probe failed for a different reason: %w", err)
	}
	if err := operation(); err != nil {
		return err
	}
	after, err := windows.GetNamedSecurityInfo(parent, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if after.String() != deniedText {
		return errors.New("operation changed test ACL")
	}
	return nil
}
