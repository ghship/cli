//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestWindowsDPAPIProtectsAndRejectsTampering(t *testing.T) {
	setupAuthHome(t)
	plain := []byte("synthetic-value-for-dpapi")
	protected, err := protectSecret(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(protected, plain) {
		t.Fatal("DPAPI output contains plaintext")
	}
	got, err := unprotectSecret(protected)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("DPAPI round trip failed")
	}
	a := account{SchemaVersion: 1, Host: "github.com", Login: "developer", Secret: protected}
	path, err := accountPath(a.Host, a.Login)
	if err != nil {
		t.Fatal(err)
	}
	a.Secret = append([]byte(nil), protected...)
	a.Secret[len(a.Secret)/2] ^= 0xff
	corrupt, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	if err = atomicConfig(path, corrupt); err != nil {
		t.Fatal(err)
	}
	if _, err = accountAPI(a.Host, a.Login); err == nil || !strings.Contains(err.Error(), "credential cannot be decrypted") {
		t.Fatal("tampered DPAPI credential was not rejected")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, corrupt) {
		t.Fatal("tampered account data was changed")
	}
}
