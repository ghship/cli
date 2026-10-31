//go:build !windows

package main

import "errors"

func withDeniedDirectoryCreate(string, func() error) error {
	return errors.New("Windows ACL test is unavailable on this platform")
}
