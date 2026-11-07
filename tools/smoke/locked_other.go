//go:build !windows

package main

import "errors"

func withLockedBinary(string, func() error) error {
	return errors.New("binary lock test requires Windows")
}
