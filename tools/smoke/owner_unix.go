//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

func securityOwner(path string, info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("owner unavailable")
	}
	return fmt.Sprintf("%d:%d", stat.Uid, stat.Gid), nil
}
