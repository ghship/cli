package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
)

func expectRefusal(output []byte, err error, code int, message string) error {
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != code || !strings.Contains(string(output), message) {
		return fmt.Errorf("expected exit %d containing %q; got error %v and output %q", code, message, err, output)
	}
	return nil
}

type entrySnapshot struct {
	mode   fs.FileMode
	digest [32]byte
	link   string
	owner  string
}

// Snapshot full trees without following symlinks. Files, metadata, directories,
// executables, package receipts and user files are compared after a refusal.
func snapshotTrees(roots ...string) (map[string]entrySnapshot, error) {
	snapshot := map[string]entrySnapshot{}
	for _, root := range roots {
		if _, err := os.Lstat(root); os.IsNotExist(err) {
			snapshot[root] = entrySnapshot{}
			continue
		} else if err != nil {
			return nil, err
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			info, e := os.Lstat(path)
			if e != nil {
				return e
			}
			owner, e := securityOwner(path, info)
			if e != nil {
				return e
			}
			record := entrySnapshot{mode: info.Mode(), owner: owner}
			if info.Mode()&os.ModeSymlink != 0 {
				record.link, e = os.Readlink(path)
			} else if info.Mode().IsRegular() {
				var data []byte
				data, e = os.ReadFile(path)
				if e == nil {
					record.digest = sha256.Sum256(data)
				}
			}
			if e != nil {
				return e
			}
			snapshot[path] = record
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return snapshot, nil
}

func unchangedTrees(before map[string]entrySnapshot, roots ...string) error {
	after, err := snapshotTrees(roots...)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(before, after) {
		return errors.New("protected installation or user data changed after refusal")
	}
	return nil
}
