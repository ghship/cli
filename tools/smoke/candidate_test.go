package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyCandidate(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "package")
	archive := filepath.Join(root, "candidate.tgz")
	files := map[string][]byte{"package.json": []byte(`{"name":"@ghship/cli"}`)}
	checks := map[string]string{}
	for _, osname := range []string{"darwin", "linux", "win32"} {
		for _, arch := range []string{"x64", "arm64"} {
			name := "ghship"
			if osname == "win32" {
				name += ".exe"
			}
			target := osname + "-" + arch
			binary := filepath.Join(pkg+"-platforms", target, "bin", name)
			if err := os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(binary, []byte(target), 0644); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte(target))
			checks[target] = hex.EncodeToString(sum[:])
		}
	}
	writeCandidate := func() string {
		t.Helper()
		files["checksums.json"], _ = json.Marshal(checks)
		f, err := os.Create(archive)
		if err != nil {
			t.Fatal(err)
		}
		gz := gzip.NewWriter(f)
		tw := tar.NewWriter(gz)
		for name, content := range files {
			path := filepath.Join(pkg, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, content, 0644); err != nil {
				t.Fatal(err)
			}
			if err := tw.WriteHeader(&tar.Header{Name: "package/" + name, Mode: 0644, Size: int64(len(content))}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(content); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		if err := gz.Close(); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(archive)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	}
	sha := writeCandidate()
	if err := verifyCandidate(pkg, archive, sha); err != nil {
		t.Fatal(err)
	}
	if err := verifyCandidate(pkg, archive, strings.Repeat("0", 64)); err == nil {
		t.Fatal("accepted wrong build-job digest")
	}
	if err := os.WriteFile(filepath.Join(pkg, "package.json"), []byte("modified"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyCandidate(pkg, archive, sha); err == nil {
		t.Fatal("accepted modified extracted package")
	}
	checks["linux-x64"] = strings.Repeat("0", 64)
	sha = writeCandidate()
	if err := verifyCandidate(pkg, archive, sha); err == nil {
		t.Fatal("accepted candidate whose binary disagrees with its checksum")
	}
}
