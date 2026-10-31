package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	assets "github.com/ghship/cli"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// buildFixture builds an internally consistent, synthetic older GHShip from the
// current sources. The Go overlay replaces only version metadata; it never
// modifies the checkout and is not a claim that this version was released.
func buildFixture(pkg, tmp string) (string, string, error) {
	const oldVersion = "0.0.1"
	if assets.Version == oldVersion {
		return "", "", fmt.Errorf("fixture version must differ")
	}
	root, err := os.Getwd()
	if err != nil {
		return "", "", err
	}
	overlayDir := filepath.Join(tmp, "fixture-overlay")
	if err = os.MkdirAll(overlayDir, 0755); err != nil {
		return "", "", err
	}
	replacements := map[string]string{}
	for _, file := range []struct{ name, old, new string }{
		{"assets.go", `const Version = "` + assets.Version + `"`, `const Version = "` + oldVersion + `"`},
		{"SKILL.md", `version: "` + assets.Version + `"`, `version: "` + oldVersion + `"`},
	} {
		src := filepath.Join(root, file.name)
		b, e := os.ReadFile(src)
		if e != nil {
			return "", "", e
		}
		if strings.Count(string(b), file.old) != 1 {
			return "", "", fmt.Errorf("unexpected fixture source: %s", file.name)
		}
		dst := filepath.Join(overlayDir, file.name)
		if e = os.WriteFile(dst, []byte(strings.Replace(string(b), file.old, file.new, 1)), 0644); e != nil {
			return "", "", e
		}
		replacements[src] = dst
	}
	overlay, err := json.Marshal(struct{ Replace map[string]string }{replacements})
	if err != nil {
		return "", "", err
	}
	overlayPath := filepath.Join(overlayDir, "overlay.json")
	if err = os.WriteFile(overlayPath, overlay, 0644); err != nil {
		return "", "", err
	}
	dir := filepath.Join(tmp, "fixture-package")
	binaryName := "ghship"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}
	rel := filepath.Join("bin", runtime.GOOS+"-"+runtime.GOARCH, binaryName)
	binary := filepath.Join(dir, rel)
	if err = os.MkdirAll(filepath.Dir(binary), 0755); err != nil {
		return "", "", err
	}
	c := exec.Command("go", "build", "-trimpath", "-overlay", overlayPath, "-o", binary, "./cmd/ghship")
	c.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, e := c.CombinedOutput(); e != nil {
		return "", "", fmt.Errorf("build synthetic baseline: %w: %s", e, b)
	}
	for _, name := range []string{"package.json", "run.cjs", "README.md"} {
		b, e := os.ReadFile(filepath.Join(pkg, name))
		if e != nil {
			return "", "", e
		}
		if name == "package.json" {
			var v map[string]any
			if e = json.Unmarshal(b, &v); e != nil {
				return "", "", e
			}
			v["version"] = oldVersion
			b, e = json.MarshalIndent(v, "", "  ")
			if e != nil {
				return "", "", e
			}
		}
		if e = os.WriteFile(filepath.Join(dir, name), b, 0644); e != nil {
			return "", "", e
		}
	}
	b, err := os.ReadFile(binary)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(b)
	checks, err := json.Marshal(map[string]string{filepath.ToSlash(rel): hex.EncodeToString(sum[:])})
	if err != nil {
		return "", "", err
	}
	if err = os.WriteFile(filepath.Join(dir, "checksums.json"), checks, 0644); err != nil {
		return "", "", err
	}
	return dir, binary, nil
}
