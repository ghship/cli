package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateArchiveValidation(t *testing.T) {
	var data bytes.Buffer
	gz := gzip.NewWriter(&data)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "package/bin/ghship", Mode: 0755, Size: 3, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("bin")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha512.Sum512(data.Bytes())
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(sum[:])
	if err := verifyUpdateIntegrity(data.Bytes(), integrity); err != nil {
		t.Fatal(err)
	}
	if err := verifyUpdateIntegrity(data.Bytes(), "sha512-invalid"); err == nil {
		t.Fatal("invalid integrity accepted")
	}
	b, err := updateArchiveFile(data.Bytes(), "package/bin/ghship", 8)
	if err != nil || string(b) != "bin" {
		t.Fatalf("valid file: %q %v", b, err)
	}
	if _, err := updateArchiveFile(data.Bytes(), "package/bin/ghship", 2); err == nil {
		t.Fatal("oversize file accepted")
	}
	if _, err := updateArchiveFile(data.Bytes(), "package/bin/missing", 8); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestWindowsCleanupStatusAndStartFailure(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "ghship.exe")
	backup := filepath.Join(dir, ".ghship-backup-123")
	child, err := json.Marshal(map[string]string{"version": "1.0.7", "pendingBackup": backup})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	startErr := errors.New("start failed")
	err = finishStandaloneWindowsUpdate(&output, child, dir, "1.0.7", binary, strings.Repeat("a", 64), true, func(_, _, _ string) error { return startErr })
	var pending cliError
	if !errors.As(err, &pending) || pending.code != 4 || !strings.Contains(err.Error(), backup) || !strings.Contains(err.Error(), "cleanup could not start") {
		t.Fatalf("cleanup failure not reported: %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("reported success despite cleanup start failure")
	}
	err = finishStandaloneWindowsUpdate(&output, child, dir, "1.0.7", binary, strings.Repeat("a", 64), true, func(gotBinary, gotBackup, hash string) error {
		if gotBinary != binary || gotBackup != backup || hash != strings.Repeat("a", 64) {
			t.Fatal("wrong cleanup request")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	if json.Unmarshal(output.Bytes(), &result) != nil || result["pendingBackup"] != backup || !strings.Contains(result["cleanup"], "may remain") {
		t.Fatalf("cleanup status missing: %s", output.String())
	}
}

func TestStandaloneUpdateRejectsManifestVersionMismatch(t *testing.T) {
	old := manifest{Version: "0.0.0", Binary: &binaryRecord{Path: "unused"}}
	var output bytes.Buffer
	err := standaloneDirectUpdate(old, t.TempDir(), nil, &output)
	if err == nil || !strings.Contains(err.Error(), "manifest version does not match") {
		t.Fatalf("mismatched manifest accepted: %v", err)
	}
}

func TestUpdateDownloadRejectsUntrustedOriginAndOversize(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "12345") }))
	defer server.Close()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = updateDownload(server.Client(), base, server.URL+"/package", 4); err == nil {
		t.Fatal("oversize response accepted")
	}
	if _, err = updateDownload(server.Client(), base, "https://example.com/package", 64); err == nil {
		t.Fatal("foreign origin accepted")
	}
	data, err := updateDownload(server.Client(), base, server.URL+"/package", 5)
	if err != nil || string(data) != "12345" {
		t.Fatalf("valid download: %q %v", data, err)
	}
}

func TestUpdateArchiveRejectsDuplicateAndLink(t *testing.T) {
	for _, kind := range []string{"duplicate", "link"} {
		t.Run(kind, func(t *testing.T) {
			var data bytes.Buffer
			gz := gzip.NewWriter(&data)
			tw := tar.NewWriter(gz)
			h := &tar.Header{Name: "package/bin/ghship", Mode: 0755, Size: 1, Typeflag: tar.TypeReg}
			if kind == "link" {
				h.Typeflag = tar.TypeSymlink
				h.Size = 0
				h.Linkname = "/tmp/other"
			}
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if kind == "duplicate" {
				if _, err := tw.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
				if err := tw.WriteHeader(h); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write([]byte("y")); err != nil {
					t.Fatal(err)
				}
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			if err := gz.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := updateArchiveFile(data.Bytes(), "package/bin/ghship", 8); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}
