package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// verifyCandidate binds the unpacked package and its binaries to the exact
// tarball digest reported by the build job, rather than a locally rebuilt one.
func verifyCandidate(pkg, tarball, expected string) error {
	if len(expected) != 64 {
		return fmt.Errorf("invalid candidate SHA-256")
	}
	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		f.Close()
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != expected {
		f.Close()
		return fmt.Errorf("candidate SHA-256 mismatch")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasPrefix(hdr.Name, "package/") {
			return fmt.Errorf("unexpected candidate entry")
		}
		name := strings.TrimPrefix(hdr.Name, "package/")
		if !fs.ValidPath(name) || seen[name] {
			return fmt.Errorf("unsafe or duplicate candidate entry")
		}
		seen[name] = true
		path := filepath.Join(pkg, filepath.FromSlash(name))
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("candidate extracted file unavailable: %s", name)
		}
		disk, err := os.Open(path)
		if err != nil {
			return err
		}
		a, b := sha256.New(), sha256.New()
		_, e1 := io.Copy(a, tr)
		_, e2 := io.Copy(b, disk)
		closeErr := disk.Close()
		if e1 != nil || e2 != nil || closeErr != nil || !strings.EqualFold(hex.EncodeToString(a.Sum(nil)), hex.EncodeToString(b.Sum(nil))) {
			return fmt.Errorf("candidate extracted content mismatch: %s", name)
		}
	}
	if err := filepath.WalkDir(pkg, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(pkg, path)
		if err != nil || !seen[filepath.ToSlash(rel)] || !d.Type().IsRegular() {
			return fmt.Errorf("unexpected extracted candidate file")
		}
		return nil
	}); err != nil {
		return err
	}
	var checks map[string]string
	b, err := os.ReadFile(filepath.Join(pkg, "checksums.json"))
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &checks); err != nil {
		return err
	}
	for _, target := range []string{"darwin", "linux", "windows"} {
		for _, arch := range []string{"amd64", "arm64"} {
			name := "ghship"
			if target == "windows" {
				name += ".exe"
			}
			rel := "bin/" + target + "-" + arch + "/" + name
			if !seen[rel] {
				return fmt.Errorf("candidate binary missing: %s", rel)
			}
			data, err := os.ReadFile(filepath.Join(pkg, filepath.FromSlash(rel)))
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			if checks[rel] != hex.EncodeToString(sum[:]) {
				return fmt.Errorf("candidate binary checksum mismatch: %s", rel)
			}
		}
	}
	if len(checks) != 6 {
		return fmt.Errorf("unexpected candidate checksums")
	}
	fmt.Printf("PASS shared candidate SHA-256 %s and six binary checksums\n", got)
	return nil
}
