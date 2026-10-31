// Real offline product acceptance against a built npm package and its native binary.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	assets "github.com/ghship/cli"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: go run ./tools/smoke <built-npm-directory>")
	}
	pkg, e := filepath.Abs(os.Args[1])
	if e != nil {
		return e
	}
	tmp, e := os.MkdirTemp("", "ghship-acceptance-")
	if e != nil {
		return e
	}
	tmp, e = filepath.EvalSymlinks(tmp)
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	home := filepath.Join(tmp, "home")
	os.Mkdir(home, 0o755)
	env := []string{}
	for _, v := range os.Environ() {
		k := strings.ToUpper(strings.SplitN(v, "=", 2)[0])
		if k != "HOME" && k != "USERPROFILE" && k != "LOCALAPPDATA" && k != "APPDATA" && k != "NPM_CONFIG_PREFIX" && k != "NPM_CONFIG_CACHE" {
			env = append(env, v)
		}
	}
	env = append(env, "HOME="+home, "USERPROFILE="+home, "LOCALAPPDATA="+filepath.Join(home, "local"), "npm_config_cache="+filepath.Join(tmp, "cache"), "APPDATA="+filepath.Join(home, "AppData"), "npm_config_prefix="+filepath.Join(tmp, "global"))
	command := func(name string, args ...string) ([]byte, error) {
		c := exec.Command(name, args...)
		c.Env = env
		b, e := c.CombinedOutput()
		if e != nil {
			return b, fmt.Errorf("%s %v failed: %s", name, args, b)
		}
		return b, nil
	}
	npm := func(args ...string) ([]byte, error) {
		if runtime.GOOS != "windows" {
			return command("npm", args...)
		}
		p, e := exec.LookPath("npm")
		if e != nil {
			return nil, e
		}
		cli := filepath.Join(filepath.Dir(p), "node_modules", "npm", "bin", "npm-cli.js")
		return command("node", append([]string{cli}, args...)...)
	}
	// Pack a complete registry-shaped package. No remote registry writes.
	b, e := npm("pack", pkg, "--json", "--pack-destination", tmp)
	if e != nil {
		return e
	}
	var packs []struct {
		Filename string
		Files    []struct{ Path string }
	}
	if e = json.Unmarshal(b, &packs); e != nil || len(packs) != 1 {
		return fmt.Errorf("invalid npm pack output: %s", b)
	}
	for _, file := range packs[0].Files {
		if file.Path != "package.json" && file.Path != "run.cjs" && file.Path != "README.md" && file.Path != "checksums.json" && !strings.HasPrefix(file.Path, "bin/") {
			return fmt.Errorf("unexpected distribution file: %s", file.Path)
		}
	}
	tarball := filepath.Join(tmp, packs[0].Filename)
	prefix := filepath.Join(tmp, "global")
	foreign := filepath.Join(tmp, "foreign")
	if e = os.Mkdir(foreign, 0755); e != nil {
		return e
	}
	foreignMeta := fmt.Sprintf(`{"name":"@ghship/cli","version":%q,"bin":{"ghship":"run.cjs"},"files":["run.cjs"]}`, assets.Version)
	if e = os.WriteFile(filepath.Join(foreign, "package.json"), []byte(foreignMeta), 0644); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(foreign, "run.cjs"), []byte("#!/usr/bin/env node\nconsole.log('FOREIGN PACKAGE');\n"), 0755); e != nil {
		return e
	}
	foreignPack, e := npm("pack", foreign, "--json", "--pack-destination", foreign)
	if e != nil {
		return e
	}
	var foreignPacks []struct{ Filename string }
	if e = json.Unmarshal(foreignPack, &foreignPacks); e != nil || len(foreignPacks) != 1 {
		return fmt.Errorf("foreign fixture pack failed")
	}
	if _, e = npm("install", "-g", filepath.Join(foreign, foreignPacks[0].Filename), "--ignore-scripts", "--no-audit", "--no-fund"); e != nil {
		return e
	}
	if _, e = npm("exec", "--yes", "--package", tarball, "--", "ghship", "install", "--package-source", tarball, "--user-only", "--json"); e == nil {
		return fmt.Errorf("foreign same-name/version package was accepted")
	}
	if _, e = os.Stat(filepath.Join(home, ".agents", "skills", "ghship")); !os.IsNotExist(e) {
		return fmt.Errorf("skill mutated before foreign-package ownership rejection")
	}
	if _, e = npm("uninstall", "-g", "@ghship/cli", "--ignore-scripts", "--no-audit", "--no-fund"); e != nil {
		return e
	}
	fmt.Println("PASS foreign same-name/version global package preserved and rejected")
	for i := 0; i < 2; i++ {
		b, e = npm("exec", "--yes", "--package", tarball, "--", "ghship", "install", "--package-source", tarball, "--user-only", "--json")
		if e != nil {
			return e
		}
		fmt.Printf("PASS npx install: %s", b)
		if e = verifyInstalledSkill(home); e != nil {
			return e
		}
	}
	launcher := filepath.Join(prefix, "lib", "node_modules", "@ghship", "cli", "run.cjs")
	if runtime.GOOS == "windows" {
		launcher = filepath.Join(prefix, "node_modules", "@ghship", "cli", "run.cjs")
	}
	invoke := func(a ...string) ([]byte, error) { return command("node", append([]string{launcher}, a...)...) }
	b, e = invoke("version")
	if e != nil {
		return e
	}
	fmt.Printf("PASS npm version: %s", b)
	for _, action := range []string{"update", "uninstall"} {
		b, e = npm("exec", "--yes", "--package", tarball, "--", "ghship", action, "--package-source", tarball, "--json")
		if e != nil {
			return e
		}
		fmt.Printf("PASS npx %s: %s", action, b)
		if action == "update" {
			if e = verifyInstalledSkill(home); e != nil {
				return e
			}
		}
	}
	if _, e = command("node", filepath.Join(pkg, "run.cjs"), "unknown-command"); e == nil {
		return fmt.Errorf("launcher lost failing exit code")
	}
	if _, e = os.Stat(launcher); !os.IsNotExist(e) {
		return fmt.Errorf("npm package not removed")
	}
	fmt.Println("PASS npm binary uninstall")
	name := "ghship"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(pkg, "bin", runtime.GOOS+"-"+runtime.GOARCH, name)
	for _, action := range []string{"install", "update", "uninstall"} {
		b, e = command(binary, action, "--standalone", "--user-only", "--json")
		if e != nil {
			return e
		}
		fmt.Printf("PASS standalone %s: %s", action, b)
		if action != "uninstall" {
			if e = verifyInstalledSkill(home); e != nil {
				return e
			}
		}
	}
	skill := filepath.Join(home, ".agents", "skills", "ghship")
	if _, e = os.Stat(skill); !os.IsNotExist(e) {
		return fmt.Errorf("skill remains after uninstall")
	}
	fmt.Println("PASS real npm tarball and standalone lifecycle; temporary home only")
	return nil
}

func verifyInstalledSkill(home string) error {
	dir := filepath.Join(home, ".agents", "skills", "ghship")
	public := map[string]bool{"SKILL.md": true, "references/cli.md": true, "references/pipeline.md": true, "references/setup.md": true, "references/workflow.md": true}
	for name := range public {
		want, e := os.ReadFile(filepath.FromSlash(name))
		if e != nil {
			return e
		}
		got, e := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if e != nil {
			return e
		}
		if !bytes.Equal(want, got) {
			return fmt.Errorf("installed public guidance differs: %s", name)
		}
	}
	if e := filepath.WalkDir(dir, func(path string, entry os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(dir, path)
		if e != nil {
			return e
		}
		if rel != ".ghship.json" && !public[filepath.ToSlash(rel)] {
			return fmt.Errorf("unexpected installed skill file: %s", rel)
		}
		return nil
	}); e != nil {
		return e
	}
	b, e := os.ReadFile(filepath.Join(dir, ".ghship.json"))
	if e != nil {
		return e
	}
	var metadata struct{ Version string }
	if e = json.Unmarshal(b, &metadata); e != nil {
		return e
	}
	if metadata.Version != assets.Version {
		return fmt.Errorf("installed skill version differs")
	}
	fmt.Println("PASS installed skill: public files match source; internal files excluded; version matches")
	return nil
}
