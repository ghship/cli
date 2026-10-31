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
			return b, fmt.Errorf("%s %v failed: %w: %s", name, args, e, b)
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
	fixtureDir, fixtureBinary, e := buildFixture(pkg, tmp)
	if e != nil {
		return e
	}
	fixturePacksJSON, e := npm("pack", fixtureDir, "--json", "--pack-destination", fixtureDir)
	if e != nil {
		return e
	}
	var fixturePacks []struct{ Filename string }
	if e = json.Unmarshal(fixturePacksJSON, &fixturePacks); e != nil || len(fixturePacks) != 1 {
		return fmt.Errorf("invalid synthetic baseline pack")
	}
	fixtureTar := filepath.Join(fixtureDir, fixturePacks[0].Filename)
	fixtureVersion, e := command(fixtureBinary, "version")
	if e != nil || strings.TrimSpace(string(fixtureVersion)) != "0.0.1" {
		return fmt.Errorf("synthetic baseline CLI version mismatch")
	}
	// A throwaway Git project and non-secret account data use real product paths.
	projectDir := filepath.Join(tmp, "project")
	if e = os.MkdirAll(projectDir, 0700); e != nil {
		return e
	}
	if _, e = command("git", "init", "--quiet", projectDir); e != nil {
		return e
	}
	configRoot := filepath.Join(home, ".config", "ghship")
	if runtime.GOOS == "windows" {
		configRoot = filepath.Join(home, "AppData", "ghship")
	}
	preserved := map[string]string{
		filepath.Join(configRoot, "accounts", "example.invalid", "user.json"): `{"schemaVersion":1,"host":"example.invalid","login":"user","secret":"ZmFrZQ=="}`,
		filepath.Join(projectDir, ".ghship", "skill.md"):                      "---\nmode: supplement\n---\n\nuser-owned data\n",
	}
	for p, content := range preserved {
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return e
		}
		if e = os.WriteFile(p, []byte(content), 0600); e != nil {
			return e
		}
	}
	checkPreserved := func() error {
		for p, content := range preserved {
			b, err := os.ReadFile(p)
			if err != nil || string(b) != content {
				return fmt.Errorf("user data was changed")
			}
		}
		return nil
	}
	checkProject := func(executable string) error {
		b, err := command(executable, "project", "skill", "--show", "--project", projectDir, "--json")
		if err != nil || !strings.Contains(string(b), "supplement") {
			return fmt.Errorf("project customization inaccessible: %s", b)
		}
		return checkPreserved()
	}
	prefix := filepath.Join(tmp, "global")
	launcher := filepath.Join(prefix, "lib", "node_modules", "@ghship", "cli", "run.cjs")
	if runtime.GOOS == "windows" {
		launcher = filepath.Join(prefix, "node_modules", "@ghship", "cli", "run.cjs")
	}
	skillDir := filepath.Join(home, ".agents", "skills", "ghship")
	standaloneDir := filepath.Join(home, ".local", "bin")
	if runtime.GOOS == "windows" {
		standaloneDir = filepath.Join(home, "local", "Programs", "ghship")
	}
	protected := []string{skillDir, prefix, configRoot, projectDir, standaloneDir}
	refused := func(operation func() ([]byte, error), message string) error {
		before, err := snapshotTrees(protected...)
		if err != nil {
			return err
		}
		output, err := operation()
		if e := expectRefusal(output, err, 1, message); e != nil {
			return e
		}
		return unchangedTrees(before, protected...)
	}
	testSkillConflicts := func(operation func() ([]byte, error)) error {
		unknown := filepath.Join(skillDir, "custom.txt")
		if err := os.WriteFile(unknown, []byte("do not delete"), 0600); err != nil {
			return err
		}
		if err := refused(operation, "custom file custom.txt; preserved"); err != nil {
			return err
		}
		if err := os.Remove(unknown); err != nil {
			return err
		}
		file := filepath.Join(skillDir, "SKILL.md")
		original, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if err = os.WriteFile(file, []byte("modified by user"), 0600); err != nil {
			return err
		}
		if err = refused(operation, "modified file SKILL.md; preserved"); err != nil {
			return err
		}
		if err = os.WriteFile(file, original, 0644); err != nil {
			return err
		}
		if err = os.Remove(file); err != nil {
			return err
		}
		if err = refused(operation, "managed file missing or modified; preserved"); err != nil {
			return err
		}
		if err = os.WriteFile(file, original, 0644); err != nil {
			return err
		}
		manifest := filepath.Join(skillDir, ".ghship.json")
		original, err = os.ReadFile(manifest)
		if err != nil {
			return err
		}
		if err = os.WriteFile(manifest, []byte("{"), 0600); err != nil {
			return err
		}
		if err = refused(operation, "unexpected end of JSON input"); err != nil {
			return err
		}
		return os.WriteFile(manifest, original, 0644)
	}
	checkNpmVersion := func(expected string) error {
		b, err := command("node", launcher, "version")
		if err != nil || strings.TrimSpace(string(b)) != expected {
			return fmt.Errorf("installed npm CLI version mismatch: %s", b)
		}
		return nil
	}
	foreign := filepath.Join(tmp, "foreign")
	if e = os.Mkdir(foreign, 0755); e != nil {
		return e
	}
	foreignMeta := fmt.Sprintf(`{"name":"@ghship/cli","version":%q,"bin":{"ghship":"run.cjs"},"files":["run.cjs","bin"]}`, assets.Version)
	if e = os.WriteFile(filepath.Join(foreign, "package.json"), []byte(foreignMeta), 0644); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(foreign, "run.cjs"), []byte("#!/usr/bin/env node\nconsole.log('FOREIGN PACKAGE');\n"), 0755); e != nil {
		return e
	}
	foreignBinary := filepath.Join(foreign, "bin", runtime.GOOS+"-"+runtime.GOARCH, filepath.Base(fixtureBinary))
	if e = os.MkdirAll(filepath.Dir(foreignBinary), 0755); e != nil {
		return e
	}
	foreignBytes, e := os.ReadFile(fixtureBinary)
	if e != nil {
		return e
	}
	if e = os.WriteFile(foreignBinary, foreignBytes, 0755); e != nil {
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
	if e = refused(func() ([]byte, error) {
		return npm("exec", "--yes", "--package", tarball, "--", "ghship", "install", "--package-source", tarball, "--user-only", "--json")
	}, "Global package ownership/content is unverified; preserved"); e != nil {
		return fmt.Errorf("foreign package: %w", e)
	}
	if _, e = os.Stat(filepath.Join(home, ".agents", "skills", "ghship")); !os.IsNotExist(e) {
		return fmt.Errorf("skill mutated before foreign-package ownership rejection")
	}
	if _, e = npm("uninstall", "-g", "@ghship/cli", "--ignore-scripts", "--no-audit", "--no-fund"); e != nil {
		return e
	}
	fmt.Println("PASS foreign same-name/version global package preserved and rejected")
	if _, e = npm("exec", "--yes", "--package", fixtureTar, "--", "ghship", "install", "--package-source", fixtureTar, "--user-only", "--json"); e != nil {
		return e
	}
	if e = verifyInstalledSkill(home, "0.0.1"); e != nil {
		return e
	}
	if e = checkNpmVersion("0.0.1"); e != nil {
		return e
	}
	if e = checkProject(fixtureBinary); e != nil {
		return e
	}
	if e = testSkillConflicts(func() ([]byte, error) {
		return npm("exec", "--yes", "--package", tarball, "--", "ghship", "update", "--package-source", tarball, "--json")
	}); e != nil {
		return fmt.Errorf("npm conflict: %w", e)
	}
	// A different failure, even with exit 1, must not pass as a downgrade.
	beforeUnrelated, e := snapshotTrees(protected...)
	if e != nil {
		return e
	}
	unrelatedOutput, unrelatedErr := npm("exec", "--yes", "--package", fixtureTar, "--", "ghship", "update", "--package-source", filepath.Join(tmp, "missing.tgz"), "--json")
	if e = expectRefusal(unrelatedOutput, unrelatedErr, 1, "npm pack failed"); e != nil {
		return e
	}
	if e = expectRefusal(unrelatedOutput, unrelatedErr, 1, "downgrade refused"); e == nil {
		return fmt.Errorf("unrelated npm failure accepted as downgrade")
	}
	if e = unchangedTrees(beforeUnrelated, protected...); e != nil {
		return e
	}
	if _, e = npm("exec", "--yes", "--package", tarball, "--", "ghship", "update", "--package-source", tarball, "--json"); e != nil {
		return e
	}
	if e = verifyInstalledSkill(home, assets.Version); e != nil {
		return e
	}
	if _, e = npm("exec", "--yes", "--package", tarball, "--", "ghship", "update", "--package-source", tarball, "--json"); e != nil {
		return e
	}
	if e = verifyInstalledSkill(home, assets.Version); e != nil {
		return e
	}
	if e = checkNpmVersion(assets.Version); e != nil {
		return e
	}
	currentBinary := filepath.Join(pkg, "bin", runtime.GOOS+"-"+runtime.GOARCH, filepath.Base(fixtureBinary))
	if e = checkProject(currentBinary); e != nil {
		return e
	}
	if e = projectLifecycle(tmp, home, projectDir, currentBinary, env); e != nil {
		return e
	}
	if e = checkPreserved(); e != nil {
		return e
	}
	if e = refused(func() ([]byte, error) {
		return npm("exec", "--yes", "--package", fixtureTar, "--", "ghship", "update", "--package-source", fixtureTar, "--json")
	}, "downgrade refused"); e != nil {
		return fmt.Errorf("npm downgrade: %w", e)
	}
	if e = checkNpmVersion(assets.Version); e != nil {
		return e
	}
	if e = verifyInstalledSkill(home, assets.Version); e != nil {
		return e
	}
	if e = checkPreserved(); e != nil {
		return e
	}
	if _, e = npm("exec", "--yes", "--package", tarball, "--", "ghship", "uninstall", "--package-source", tarball, "--json"); e != nil {
		return e
	}
	fmt.Println("PASS npm synthetic baseline upgrade and downgrade refusal")
	for i := 0; i < 2; i++ {
		b, e = npm("exec", "--yes", "--package", tarball, "--", "ghship", "install", "--package-source", tarball, "--user-only", "--json")
		if e != nil {
			return e
		}
		fmt.Printf("PASS npx install: %s", b)
		if e = verifyInstalledSkill(home, assets.Version); e != nil {
			return e
		}
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
			if e = verifyInstalledSkill(home, assets.Version); e != nil {
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
	installedBinary := filepath.Join(standaloneDir, name)
	checkStandaloneVersion := func(expected string) error {
		b, err := command(installedBinary, "version")
		if err != nil || strings.TrimSpace(string(b)) != expected {
			return fmt.Errorf("standalone CLI version mismatch: %s", b)
		}
		return nil
	}
	if _, e = command(fixtureBinary, "install", "--standalone", "--user-only", "--json"); e != nil {
		return e
	}
	if e = verifyInstalledSkill(home, "0.0.1"); e != nil {
		return e
	}
	if e = checkStandaloneVersion("0.0.1"); e != nil {
		return e
	}
	for i := 0; i < 2; i++ {
		if _, e = command(binary, "update", "--standalone", "--user-only", "--json"); e != nil {
			return e
		}
		if e = verifyInstalledSkill(home, assets.Version); e != nil {
			return e
		}
	}
	if e = checkStandaloneVersion(assets.Version); e != nil {
		return e
	}
	if e = testSkillConflicts(func() ([]byte, error) {
		return command(binary, "update", "--standalone", "--user-only", "--json")
	}); e != nil {
		return fmt.Errorf("standalone conflict: %w", e)
	}
	// Refuse a non-writable staging parent on Unix; Windows ACLs need a
	// separate controlled scenario and must not be inferred from chmod.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		parent := filepath.Dir(skillDir)
		info, err := os.Stat(parent)
		if err != nil {
			return err
		}
		if err = os.Chmod(parent, 0555); err != nil {
			return err
		}
		err = refused(func() ([]byte, error) { return command(binary, "update", "--standalone", "--user-only", "--json") }, "permission denied")
		restore := os.Chmod(parent, info.Mode().Perm())
		if err != nil {
			return fmt.Errorf("standalone permissions: %w", err)
		}
		if restore != nil {
			return restore
		}
	}
	if runtime.GOOS == "windows" {
		parent := filepath.Dir(skillDir)
		if e = withDeniedDirectoryCreate(parent, func() error {
			return refused(func() ([]byte, error) {
				return command(binary, "update", "--standalone", "--user-only", "--json")
			}, "Access is denied")
		}); e != nil {
			return fmt.Errorf("standalone Windows ACL: %w", e)
		}
		fmt.Println("PASS Windows ACL denied directory creation and preserved installation")
	}
	if e = refused(func() ([]byte, error) {
		return command(fixtureBinary, "update", "--standalone", "--user-only", "--json")
	}, "downgrade refused"); e != nil {
		return fmt.Errorf("standalone downgrade: %w", e)
	}
	if e = checkStandaloneVersion(assets.Version); e != nil {
		return e
	}
	if e = verifyInstalledSkill(home, assets.Version); e != nil {
		return e
	}
	if e = checkPreserved(); e != nil {
		return e
	}
	if _, e = command(binary, "uninstall", "--standalone", "--user-only", "--json"); e != nil {
		return e
	}
	fmt.Println("PASS standalone synthetic baseline upgrade and downgrade refusal")
	for _, action := range []string{"install", "update", "uninstall"} {
		b, e = command(binary, action, "--standalone", "--user-only", "--json")
		if e != nil {
			return e
		}
		fmt.Printf("PASS standalone %s: %s", action, b)
		if action != "uninstall" {
			if e = verifyInstalledSkill(home, assets.Version); e != nil {
				return e
			}
		}
	}
	skill := filepath.Join(home, ".agents", "skills", "ghship")
	if _, e = os.Stat(skill); !os.IsNotExist(e) {
		return fmt.Errorf("skill remains after uninstall")
	}
	if e = checkPreserved(); e != nil {
		return e
	}
	fmt.Println("PASS real npm tarball and standalone lifecycle; temporary home only")
	return nil
}

func verifyInstalledSkill(home, expectedVersion string) error {
	dir := filepath.Join(home, ".agents", "skills", "ghship")
	public := map[string]bool{"SKILL.md": true, "references/cli.md": true, "references/pipeline.md": true, "references/setup.md": true, "references/workflow.md": true}
	for name := range public {
		want, e := os.ReadFile(filepath.FromSlash(name))
		if e != nil {
			return e
		}
		if name == "SKILL.md" && expectedVersion != assets.Version {
			want = bytes.Replace(want, []byte(`version: "`+assets.Version+`"`), []byte(`version: "`+expectedVersion+`"`), 1)
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
	if metadata.Version != expectedVersion {
		return fmt.Errorf("installed skill version differs")
	}
	fmt.Println("PASS installed skill: public files match source; internal files excluded; version matches")
	return nil
}
