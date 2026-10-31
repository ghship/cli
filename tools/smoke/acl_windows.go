//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
	originalACL, err := aclFingerprint(original)
	if err != nil {
		return err
	}
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
			if err == nil {
				var actual string
				actual, err = aclFingerprint(restored)
				if err == nil && actual != originalACL {
					err = errors.New("restored test DACL differs from original")
				}
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

// A DACL's exact ACE sequence, flags and inherited/protected status must be
// restored. Windows may add the derived AI (auto-inherited) metadata bit when
// an unprotected DACL is restored; this bit does not alter the ACEs or the
// protected inheritance setting. Owner/group fields are not part of a DACL.
func aclFingerprint(sd *windows.SECURITY_DESCRIPTOR) (string, error) {
	control, _, err := sd.Control()
	if err != nil {
		return "", err
	}
	sddl := sd.String()
	start := strings.Index(sddl, "D:")
	if start < 0 {
		return "", errors.New("DACL missing from security descriptor")
	}
	dacl := sddl[start+2:]
	if end := strings.Index(dacl, "S:"); end >= 0 {
		dacl = dacl[:end]
	}
	aces := strings.IndexByte(dacl, '(')
	if aces < 0 {
		aces = len(dacl)
	}
	flags := strings.ReplaceAll(dacl[:aces], "AI", "")
	return fmt.Sprintf("%04x:%s%s", control&(windows.SE_DACL_PRESENT|windows.SE_DACL_PROTECTED|windows.SE_DACL_DEFAULTED), flags, dacl[aces:]), nil
}
