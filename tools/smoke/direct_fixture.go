package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// directFixture constructs a synthetic older split npm installation from the
// current launcher and the existing 0.0.1 fixture binary. It is not a release.
func directFixture(pkg, binary, tmp string, npm func(...string) ([]byte, error)) (string, string, string, error) {
	osName, arch := runtime.GOOS, runtime.GOARCH
	if osName == "windows" {
		osName = "win32"
	}
	if arch == "amd64" {
		arch = "x64"
	}
	target := osName + "-" + arch
	name := "@ghship/cli-" + target
	root := filepath.Join(tmp, "direct-older")
	mainDir, platformDir := filepath.Join(root, "main"), filepath.Join(root, "platform")
	if err := os.MkdirAll(mainDir, 0755); err != nil {
		return "", "", "", err
	}
	if err := os.MkdirAll(filepath.Join(platformDir, "bin"), 0755); err != nil {
		return "", "", "", err
	}
	for _, file := range []string{"run.cjs", "README.md"} {
		b, err := os.ReadFile(filepath.Join(pkg, file))
		if err != nil {
			return "", "", "", err
		}
		if err = os.WriteFile(filepath.Join(mainDir, file), b, 0644); err != nil {
			return "", "", "", err
		}
	}
	metaBytes, err := os.ReadFile(filepath.Join(pkg, "package.json"))
	if err != nil {
		return "", "", "", err
	}
	var meta map[string]any
	if err = json.Unmarshal(metaBytes, &meta); err != nil {
		return "", "", "", err
	}
	meta["version"] = "0.0.1"
	meta["optionalDependencies"] = map[string]string{name: "0.0.1"}
	metaBytes, err = json.Marshal(meta)
	if err != nil {
		return "", "", "", err
	}
	if err = os.WriteFile(filepath.Join(mainDir, "package.json"), metaBytes, 0644); err != nil {
		return "", "", "", err
	}
	platformMeta, err := json.Marshal(map[string]any{"name": name, "version": "0.0.1", "os": []string{osName}, "cpu": []string{arch}, "files": []string{"bin"}})
	if err != nil {
		return "", "", "", err
	}
	if err = os.WriteFile(filepath.Join(platformDir, "package.json"), platformMeta, 0644); err != nil {
		return "", "", "", err
	}
	b, err := os.ReadFile(binary)
	if err != nil {
		return "", "", "", err
	}
	if err = os.WriteFile(filepath.Join(platformDir, "bin", filepath.Base(binary)), b, 0755); err != nil {
		return "", "", "", err
	}
	sum := sha256.Sum256(b)
	checks, err := json.Marshal(map[string]string{target: hex.EncodeToString(sum[:])})
	if err != nil {
		return "", "", "", err
	}
	if err = os.WriteFile(filepath.Join(mainDir, "checksums.json"), checks, 0644); err != nil {
		return "", "", "", err
	}
	pack := func(dir, expected string) (string, error) {
		out, e := npm("pack", dir, "--json", "--pack-destination", dir)
		if e != nil {
			return "", e
		}
		record, e := parseNpmPack(out, expected)
		if e != nil {
			return "", fmt.Errorf("invalid direct fixture pack: %w", e)
		}
		return filepath.Join(dir, record.Filename), nil
	}
	platformTar, err := pack(platformDir, name)
	if err != nil {
		return "", "", "", err
	}
	mainTar, err := pack(mainDir, "@ghship/cli")
	if err != nil {
		return "", "", "", err
	}
	return mainTar, platformDir, platformTar, nil
}
