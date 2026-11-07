//go:build !windows

package main

import "os"

func canDeferBackup(error) bool      { return false }
func removeBackup(path string) error { return os.RemoveAll(path) }
