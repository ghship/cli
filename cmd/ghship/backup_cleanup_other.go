//go:build !windows

package main

import "os"

func removeBackup(path string) error { return os.RemoveAll(path) }
