//go:build !windows

package main

import "errors"

func startDeferredBackupCleanup(binary, backup, hash string) error {
	return errors.New("deferred cleanup is only available on Windows")
}
func runDeferredBackupCleanup(args []string) error {
	return errors.New("deferred cleanup is only available on Windows")
}
