package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// The parent belongs to this test. Verify effective access before and after
// temporarily denying only the current user directory creation on Windows.
func withDeniedDirectoryCreate(parent string, operation func() error) (result error) {
	current, err := user.Current()
	if err != nil {
		return err
	}
	probe := filepath.Join(parent, ".ghship-acl-probe")
	checkWritable := func() error {
		if err := os.Mkdir(probe, 0700); err != nil {
			return err
		}
		return os.Remove(probe)
	}
	if err := checkWritable(); err != nil {
		return fmt.Errorf("test directory is not initially writable: %w", err)
	}
	// Both commands affect only the disposable directory created by this test.
	defer func() {
		if out, err := exec.Command("icacls", parent, "/remove:d", current.Username).CombinedOutput(); err != nil {
			result = errors.Join(result, fmt.Errorf("remove test denial: %w: %s", err, out))
		} else if err := checkWritable(); err != nil {
			result = errors.Join(result, fmt.Errorf("test directory access was not restored: %w", err))
		}
	}()
	if out, err := exec.Command("icacls", parent, "/deny", current.Username+":(AD)").CombinedOutput(); err != nil {
		return fmt.Errorf("set test denial: %w: %s", err, out)
	}
	if err := os.Mkdir(probe, 0700); err == nil {
		_ = os.Remove(probe)
		return errors.New("Windows ACL did not deny directory creation")
	} else if !errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("ACL probe failed for another reason: %w", err)
	}
	return operation()
}
