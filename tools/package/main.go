// Release artifact builder, not a second product CLI.
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
)

func main() {
	if e := build(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func build() error {
	out := "dist/npm"
	native := false
	for _, arg := range os.Args[1:] {
		if arg == "--native" {
			native = true
		} else if len(arg) > 0 && arg[0] != '-' {
			out = arg
		} else {
			return fmt.Errorf("usage: go run ./tools/package [--native] [new-output-directory]")
		}
	}
	if _, e := os.Lstat(out); e == nil {
		return fmt.Errorf("output exists: %s; choose a fresh directory", out)
	} else if !os.IsNotExist(e) {
		return e
	}
	meta, e := os.ReadFile("npm/package.json")
	if e != nil {
		return e
	}
	var pkg struct{ Version string }
	if e = json.Unmarshal(meta, &pkg); e != nil {
		return e
	}
	if pkg.Version != assets.Version {
		return fmt.Errorf("npm and CLI versions differ")
	}
	if e := os.MkdirAll(out, 0o755); e != nil {
		return e
	}
	for _, name := range []string{"package.json", "run.cjs", "README.md"} {
		b, e := os.ReadFile(filepath.Join("npm", name))
		if e != nil {
			return e
		}
		mode := os.FileMode(0o644)
		if name == "run.cjs" {
			mode = 0o755
		}
		if e = os.WriteFile(filepath.Join(out, name), b, mode); e != nil {
			return e
		}
	}
	sums := map[string]string{}
	targets, arches := []string{"darwin", "linux", "windows"}, []string{"amd64", "arm64"}
	if native {
		targets = []string{runtime.GOOS}
		arches = []string{runtime.GOARCH}
	}
	for _, target := range targets {
		for _, arch := range arches {
			name := "ghship"
			if target == "windows" {
				name += ".exe"
			}
			rel := filepath.Join("bin", target+"-"+arch, name)
			p := filepath.Join(out, rel)
			if e := os.MkdirAll(filepath.Dir(p), 0o755); e != nil {
				return e
			}
			c := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", p, "./cmd/ghship")
			c.Env = append(os.Environ(), "GOOS="+target, "GOARCH="+arch, "CGO_ENABLED=0")
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if e := c.Run(); e != nil {
				return e
			}
			b, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			h := sha256.Sum256(b)
			sums[filepath.ToSlash(rel)] = hex.EncodeToString(h[:])
		}
	}
	b, e := json.MarshalIndent(sums, "", "  ")
	if e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(out, "checksums.json"), b, 0o644); e != nil {
		return e
	}
	fmt.Println(out)
	return nil
}
