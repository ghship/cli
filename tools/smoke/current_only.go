package main

import (
	"fmt"
	assets "github.com/ghship/cli"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// currentOnlySmoke exercises the actual candidate with the npm version under
// test. The legacy baseline has its own npm-version constraints and is covered
// separately by the full lifecycle suite.
func currentOnlySmoke(tmp, home, tarball string, registry *registry, command func(string, ...string) ([]byte, error), npm func(...string) ([]byte, error)) error {
	sentinel := filepath.Join(home, "user-data.txt")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		return err
	}
	launcher := filepath.Join(tmp, "global", "lib", "node_modules", "@ghship", "cli", "run.cjs")
	if runtime.GOOS == "windows" {
		launcher = filepath.Join(tmp, "global", "node_modules", "@ghship", "cli", "run.cjs")
	}
	for _, action := range []string{"install", "update", "uninstall"} {
		args := []string{"exec", "--yes", "--package", tarball, "--", "ghship", action, "--package-source", tarball, "--json"}
		if action == "install" {
			args = append(args, "--user-only")
		}
		output, err := npm(args...)
		if err != nil {
			return fmt.Errorf("npm current %s: %w: %s", action, err, output)
		}
		if action != "uninstall" {
			version, err := command("node", launcher, "version")
			if err != nil || strings.TrimSpace(string(version)) != assets.Version {
				return fmt.Errorf("installed CLI version mismatch: %s", version)
			}
			if err = verifyInstalledSkill(home, assets.Version); err != nil {
				return err
			}
		} else if _, err := os.Stat(launcher); !os.IsNotExist(err) {
			return fmt.Errorf("npm launcher remains")
		}
		data, err := os.ReadFile(sentinel)
		if err != nil || string(data) != "preserve" {
			return fmt.Errorf("user data changed")
		}
	}
	platform, arch := runtime.GOOS, runtime.GOARCH
	if platform == "windows" {
		platform = "win32"
	}
	if arch == "amd64" {
		arch = "x64"
	}
	if err := registry.assertDownloads(platform + "-" + arch); err != nil {
		return err
	}
	fmt.Println("PASS current npm lifecycle with only the matching platform binary")
	return nil
}
