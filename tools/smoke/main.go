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
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 2 && len(os.Args) != 4 {
		return fmt.Errorf("usage: go run ./tools/smoke <built-npm-directory> [npm-tarball sha256]")
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
		if k != "HOME" && k != "USERPROFILE" && k != "LOCALAPPDATA" && k != "APPDATA" && k != "NPM_CONFIG_PREFIX" && k != "NPM_CONFIG_CACHE" && k != "NPM_CONFIG_REGISTRY" && k != "NPM_CONFIG_USERCONFIG" {
			env = append(env, v)
		}
	}
	env = append(env, "HOME="+home, "USERPROFILE="+home, "LOCALAPPDATA="+filepath.Join(home, "local"), "npm_config_cache="+filepath.Join(tmp, "pack-cache"), "APPDATA="+filepath.Join(home, "AppData"), "npm_config_prefix="+filepath.Join(tmp, "global"))
	command := func(name string, args ...string) ([]byte, error) {
		return runCommand(env, name, args...)
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
	registry, e := startRegistry(pkg+"-platforms", tmp, npm)
	if e != nil {
		return e
	}
	defer registry.server.Close()
	env = append(env, "npm_config_cache="+filepath.Join(tmp, "install-cache"), "npm_config_registry="+registry.server.URL, "npm_config_userconfig="+filepath.Join(tmp, "npmrc"), "npm_config_audit=false", "npm_config_fund=false")
	// With an external candidate, inspect and exercise the exact same tarball.
	// The CI job verifies its SHA-256 before extracting pkg.
	tarball := ""
	packArgs := []string{"pack", pkg, "--json", "--pack-destination", tmp}
	if len(os.Args) == 4 {
		tarball, e = filepath.Abs(os.Args[2])
		if e != nil {
			return e
		}
		if e = verifyCandidate(pkg, tarball, os.Args[3]); e != nil {
			return e
		}
		packArgs = []string{"pack", tarball, "--dry-run", "--json"}
	}
	b, e := npm(packArgs...)
	if e != nil {
		return e
	}
	pack, e := parseNpmPack(b, "@ghship/cli")
	if e != nil {
		return fmt.Errorf("invalid npm pack output: %s", b)
	}
	for _, file := range pack.Files {
		if file.Path != "package.json" && file.Path != "run.cjs" && file.Path != "README.md" && file.Path != "checksums.json" && !strings.HasPrefix(file.Path, "bin/") {
			return fmt.Errorf("unexpected distribution file: %s", file.Path)
		}
	}
	if tarball == "" {
		tarball = filepath.Join(tmp, pack.Filename)
	}
	if e = registry.registerMain(pkg, tarball); e != nil {
		return e
	}
	// Invalid initialization must be rejected by the packaged npm entry before
	// it installs any files, even when a local package source was supplied.
	invalidDir := filepath.Join(tmp, "invalid-arguments-skill")
	invalidOutput, invalidErr := npm("exec", "--yes", "--package", tarball, "--", "ghship", "install", "--package-source", tarball, "--dir", invalidDir, "--author", "bad@user", "--json")
	if e = expectRefusal(invalidOutput, invalidErr, 2, "invalid author username"); e != nil {
		return fmt.Errorf("invalid npm init argument: %w", e)
	}
	if _, e = os.Stat(invalidDir); !os.IsNotExist(e) {
		return fmt.Errorf("invalid npm init argument changed installation")
	}
	usageOutput, usageErr := npm("exec", "--yes", "--package", tarball, "--", "ghship", "install", "--dir", invalidDir, "--unknown-option", "--json")
	if e = expectRefusal(usageOutput, usageErr, 2, "Unknown lifecycle option"); e != nil {
		return fmt.Errorf("npm usage exit: %w", e)
	}
	if _, e = os.Stat(invalidDir); !os.IsNotExist(e) {
		return fmt.Errorf("invalid npm option changed installation")
	}
	for _, action := range []string{"install", "update", "uninstall"} {
		output, err := npm("exec", "--yes", "--package", tarball, "--", "ghship", action, "--check", "--dir", invalidDir, "--json")
		if err != nil {
			return fmt.Errorf("npm %s check failed: %w", action, err)
		}
		var state struct {
			Managed bool `json:"managed"`
		}
		if json.Unmarshal(output, &state) != nil || state.Managed {
			return fmt.Errorf("npm %s check returned invalid state", action)
		}
		if _, err = os.Stat(invalidDir); !os.IsNotExist(err) {
			return fmt.Errorf("npm %s check changed installation", action)
		}
	}
	fmt.Println("PASS invalid npm initialization rejected before installation")
	if os.Getenv("GHSHIP_SMOKE_CURRENT_ONLY") == "1" {
		return currentOnlySmoke(tmp, home, tarball, registry, command, npm)
	}
	omitDir := filepath.Join(tmp, "omit-optional")
	if _, e = npm("install", "--prefix", omitDir, "--omit=optional", "--ignore-scripts", tarball); e != nil {
		return e
	}
	omitLauncher := filepath.Join(omitDir, "node_modules", "@ghship", "cli", "run.cjs")
	omitOutput, omitErr := command("node", omitLauncher, "version")
	if e = expectRefusal(omitOutput, omitErr, 1, "is missing; enable optional dependencies"); e != nil {
		return fmt.Errorf("omitted platform package: %w", e)
	}
	fmt.Println("PASS omitted optional platform dependency explains how to recover")
	fixtureDir, fixtureBinary, e := buildFixture(pkg, tmp, registry.server.URL)
	if e != nil {
		return e
	}
	fixturePacksJSON, e := npm("pack", fixtureDir, "--json", "--pack-destination", fixtureDir)
	if e != nil {
		return e
	}
	fixturePack, e := parseNpmPack(fixturePacksJSON, "@ghship/cli")
	if e != nil {
		return fmt.Errorf("invalid synthetic baseline pack")
	}
	fixtureTar := filepath.Join(fixtureDir, fixturePack.Filename)
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
		filepath.Join(configRoot, "user-settings.json"):                       `{"preference":"keep"}`,
		filepath.Join(home, "user-file.txt"):                                  "keep user file\n",
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
	invoke := func(a ...string) ([]byte, error) {
		if runtime.GOOS == "windows" {
			entry := filepath.Join(tmp, "global", "ghship.cmd")
			return command("cmd.exe", append([]string{"/d", "/c", entry}, a...)...)
		}
		return command(filepath.Join(tmp, "global", "bin", "ghship"), a...)
	}
	skillDir := filepath.Join(home, ".agents", "skills", "ghship")
	standaloneDir := filepath.Join(home, ".local", "bin")
	if runtime.GOOS == "windows" {
		standaloneDir = filepath.Join(home, "local", "Programs", "ghship")
	}
	checkUninstalled := func(executable string) error {
		for _, path := range []string{executable, skillDir} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				return fmt.Errorf("managed installation remains or cannot be checked: %s", filepath.Base(path))
			}
		}
		return checkPreserved()
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
	npmOS, npmArch := runtime.GOOS, runtime.GOARCH
	if npmOS == "windows" {
		npmOS = "win32"
	}
	if npmArch == "amd64" {
		npmArch = "x64"
	}
	target := npmOS + "-" + npmArch
	candidateBinary := filepath.Join(pkg+"-platforms", target, "bin", filepath.Base(fixtureBinary))
	checkBinary := func(installed string) error {
		want, err := os.ReadFile(candidateBinary)
		if err != nil {
			return err
		}
		got, err := os.ReadFile(installed)
		if err != nil || !bytes.Equal(want, got) {
			return fmt.Errorf("installed CLI differs from shared candidate")
		}
		return nil
	}
	checkNpmVersion := func(expected string) error {
		b, err := command("node", launcher, "version")
		if err != nil || strings.TrimSpace(string(b)) != expected {
			return fmt.Errorf("installed npm CLI version mismatch: %s", b)
		}
		if expected == assets.Version {
			binary, err := command("node", "-e", "process.stdout.write(require(process.argv[1]).binaryPath())", launcher)
			if err != nil {
				return fmt.Errorf("installed platform package resolution: %w: %s", err, binary)
			}
			return checkBinary(string(binary))
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
	foreignRecord, e := parseNpmPack(foreignPack, "@ghship/cli")
	if e != nil {
		return fmt.Errorf("foreign fixture pack failed")
	}
	if _, e = npm("install", "-g", filepath.Join(foreign, foreignRecord.Filename), "--ignore-scripts", "--no-audit", "--no-fund"); e != nil {
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
	if baseline := os.Getenv("GHSHIP_BASELINE_TARBALL"); baseline != "" {
		baseline, e = filepath.Abs(baseline)
		if e != nil {
			return e
		}
		if _, e = npm("exec", "--yes", "--package", baseline, "--", "ghship", "install", "--package-source", baseline, "--user-only", "--json"); e != nil {
			return fmt.Errorf("released baseline install: %w", e)
		}
		if e = checkNpmVersion("1.0.2"); e != nil {
			return e
		}
		if _, e = npm("exec", "--yes", "--package", tarball, "--", "ghship", "update", "--package-source", tarball, "--json"); e != nil {
			return fmt.Errorf("released baseline update: %w", e)
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
		if _, e = npm("exec", "--yes", "--package", tarball, "--", "ghship", "uninstall", "--json"); e != nil {
			return e
		}
		if e = checkUninstalled(launcher); e != nil {
			return e
		}
		fmt.Println("PASS real 1.0.2 npm installation to split candidate")
	}
	directTar, olderPlatformDir, olderPlatformTar, e := directFixture(pkg, fixtureBinary, tmp, npm)
	if e != nil {
		return e
	}
	restorePlatform, e := registry.swapPlatform(olderPlatformDir, olderPlatformTar)
	if e != nil {
		return e
	}
	defer restorePlatform()
	if b, e = npm("exec", "--yes", "--package", directTar, "--", "ghship", "install", "--package-source", directTar, "--user-only", "--json"); e != nil {
		return fmt.Errorf("install synthetic direct-update baseline: %w: %s", e, b)
	}
	if e = checkNpmVersion("0.0.1"); e != nil {
		return e
	}
	if e = verifyInstalledSkill(home, "0.0.1"); e != nil {
		return e
	}
	restorePlatform()
	if b, e = invoke("update", "--json"); e != nil {
		return fmt.Errorf("direct npm upgrade from older version: %w: %s", e, b)
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
	if _, e = npm("exec", "--yes", "--package", tarball, "--", "ghship", "uninstall", "--json"); e != nil {
		return e
	}
	if e = checkUninstalled(launcher); e != nil {
		return e
	}
	fmt.Println("PASS direct npm upgrade from synthetic older split installation")
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
	if e = testSkillConflicts(func() ([]byte, error) {
		return npm("exec", "--yes", "--package", tarball, "--", "ghship", "install", "--package-source", tarball, "--user-only", "--json")
	}); e != nil {
		return fmt.Errorf("npm reinstall conflict: %w", e)
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
	if e = checkNpmVersion(assets.Version); e != nil {
		return e
	}
	if e = checkProject(candidateBinary); e != nil {
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
	currentBinary := candidateBinary
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
	}, "ENOENT"); e != nil {
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
	if _, e = npm("exec", "--yes", "--package", tarball, "--", "ghship", "uninstall", "--json"); e != nil {
		return e
	}
	if e = checkUninstalled(launcher); e != nil {
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
		if e = checkNpmVersion(assets.Version); e != nil {
			return e
		}
		if e = checkProject(currentBinary); e != nil {
			return e
		}
	}
	b, e = invoke("version")
	if e != nil {
		return e
	}
	fmt.Printf("PASS npm version: %s", b)
	if b, e = invoke("update", "--json"); e != nil {
		return fmt.Errorf("direct npm update: %w: %s", e, b)
	}
	if e = verifyInstalledSkill(home, assets.Version); e != nil {
		return e
	}
	if e = checkPreserved(); e != nil {
		return e
	}
	if e = testSkillConflicts(func() ([]byte, error) { return invoke("update", "--json") }); e != nil {
		return fmt.Errorf("direct npm update conflict: %w", e)
	}
	failNpm := filepath.Join(tmp, "fail-npm.cjs")
	if e = os.WriteFile(failNpm, []byte("console.error('simulated npm failure');process.exit(7);"), 0o644); e != nil {
		return e
	}
	beforeFailure, e := snapshotTrees(protected...)
	if e != nil {
		return e
	}
	failedEnv := append(append([]string{}, env...), "npm_execpath="+failNpm)
	failedOutput, failedErr := runCommand(failedEnv, "node", launcher, "update", "--json")
	if e = expectRefusal(failedOutput, failedErr, 7, "simulated npm failure"); e != nil {
		return e
	}
	if e = unchangedTrees(beforeFailure, protected...); e != nil {
		return e
	}
	fmt.Println("PASS direct npm update from installed launcher")
	for _, action := range []string{"update", "uninstall"} {
		args := []string{"exec", "--yes", "--package", tarball, "--", "ghship", action, "--json"}
		if action != "uninstall" {
			args = append(args, "--package-source", tarball)
		}
		b, e = npm(args...)
		if e != nil {
			return e
		}
		fmt.Printf("PASS npx %s: %s", action, b)
		if action == "update" {
			if e = verifyInstalledSkill(home, assets.Version); e != nil {
				return e
			}
			if e = checkNpmVersion(assets.Version); e != nil {
				return e
			}
			if e = checkProject(currentBinary); e != nil {
				return e
			}
		} else if e = checkUninstalled(launcher); e != nil {
			return e
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
	binary := candidateBinary
	installedBinary := filepath.Join(standaloneDir, name)
	checkStandaloneVersion := func(expected string) error {
		b, err := command(installedBinary, "version")
		if err != nil || strings.TrimSpace(string(b)) != expected {
			return fmt.Errorf("standalone CLI version mismatch: %s", b)
		}
		if expected == assets.Version {
			return checkBinary(installedBinary)
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
	if e = checkProject(installedBinary); e != nil {
		return e
	}
	standaloneEnv := append(append([]string{}, env...), "PATH="+filepath.Join(tmp, "empty-path"))
	for _, failure := range []string{"disconnect", "redirect"} {
		restore := registry.setMainFailure(failure)
		err := refused(func() ([]byte, error) { return runCommand(standaloneEnv, installedBinary, "update", "--json") }, "cannot download update")
		restore()
		if err != nil {
			return fmt.Errorf("standalone %s failure preservation: %w", failure, err)
		}
	}
	restoreTruncated := registry.setPlatformFailure("truncate")
	truncatedErr := refused(func() ([]byte, error) { return runCommand(standaloneEnv, installedBinary, "update", "--json") }, "unexpected EOF")
	restoreTruncated()
	if truncatedErr != nil {
		return fmt.Errorf("standalone interrupted platform download preservation: %w", truncatedErr)
	}
	invalidTemp := filepath.Join(tmp, "not-a-directory")
	if e = os.WriteFile(invalidTemp, []byte("not a directory"), 0600); e != nil {
		return e
	}
	writeFailureEnv := append(append([]string{}, standaloneEnv...), "TMPDIR="+invalidTemp, "TMP="+invalidTemp, "TEMP="+invalidTemp)
	if e = refused(func() ([]byte, error) { return runCommand(writeFailureEnv, installedBinary, "update", "--json") }, "not-a-directory"); e != nil {
		return fmt.Errorf("standalone temporary write failure preservation: %w", e)
	}
	parent := filepath.Dir(skillDir)
	targetPermissionChecked := false
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		info, err := os.Stat(parent)
		if err != nil {
			return err
		}
		if err = os.Chmod(parent, 0555); err != nil {
			return err
		}
		err = refused(func() ([]byte, error) { return runCommand(standaloneEnv, installedBinary, "update", "--json") }, "permission denied")
		restoreErr := os.Chmod(parent, info.Mode().Perm())
		if err != nil {
			return fmt.Errorf("direct standalone target permission: %w", err)
		}
		if restoreErr != nil {
			return restoreErr
		}
		targetPermissionChecked = true
	}
	if runtime.GOOS == "windows" {
		if err := withDeniedDirectoryCreate(parent, func() error {
			return refused(func() ([]byte, error) { return runCommand(standaloneEnv, installedBinary, "update", "--json") }, "Access is denied")
		}); err != nil {
			return fmt.Errorf("direct standalone target ACL: %w", err)
		}
		targetPermissionChecked = true
	}
	fmt.Println("PASS direct standalone interrupted download preserves installation")
	if targetPermissionChecked {
		fmt.Println("PASS direct standalone target permission denial preserves installation")
	} else {
		fmt.Println("SKIP direct standalone target permission denial when running as root")
	}
	// A registry can return a well-formed platform tarball with a valid SRI
	// that nevertheless does not match the candidate main package's checksum.
	// Preserve every installed file on that mismatch.
	platformMetaPath := filepath.Join(olderPlatformDir, "package.json")
	platformMetaBytes, e := os.ReadFile(platformMetaPath)
	if e != nil {
		return e
	}
	var wrongPlatformMeta map[string]any
	if e = json.Unmarshal(platformMetaBytes, &wrongPlatformMeta); e != nil {
		return e
	}
	wrongPlatformMeta["version"] = assets.Version
	platformMetaBytes, e = json.Marshal(wrongPlatformMeta)
	if e != nil {
		return e
	}
	if e = os.WriteFile(platformMetaPath, platformMetaBytes, 0644); e != nil {
		return e
	}
	wrongPack, e := npm("pack", olderPlatformDir, "--json", "--pack-destination", olderPlatformDir)
	if e != nil {
		return e
	}
	wrongRecord, e := parseNpmPack(wrongPack, wrongPlatformMeta["name"].(string))
	if e != nil {
		return e
	}
	restoreWrong, e := registry.swapPlatform(olderPlatformDir, filepath.Join(olderPlatformDir, wrongRecord.Filename))
	if e != nil {
		return e
	}
	defer restoreWrong()
	if e = refused(func() ([]byte, error) { return runCommand(standaloneEnv, installedBinary, "update", "--json") }, "update binary checksum mismatch"); e != nil {
		return e
	}
	restoreWrong()
	if runtime.GOOS == "windows" {
		if err := withLockedBinary(installedBinary, func() error {
			return refused(func() ([]byte, error) { return runCommand(standaloneEnv, installedBinary, "update", "--json") }, "close programs using the installation and retry")
		}); err != nil {
			return fmt.Errorf("locked standalone update preservation: %w", err)
		}
		if e = checkStandaloneVersion("0.0.1"); e != nil {
			return e
		}
		fmt.Println("PASS locked standalone update preserves installation and explains retry")
	}
	if b, e = runCommand(standaloneEnv, installedBinary, "update", "--json"); e != nil {
		return fmt.Errorf("direct standalone upgrade: %w: %s", e, b)
	}
	if runtime.GOOS == "windows" {
		var result map[string]string
		if json.Unmarshal(b, &result) != nil || result["pendingBackup"] == "" || !strings.Contains(result["cleanup"], "scheduled") || !strings.Contains(result["cleanup"], "may remain") {
			return fmt.Errorf("Windows update did not report pending cleanup: %s", b)
		}
		if filepath.Dir(result["pendingBackup"]) != standaloneDir || !strings.HasPrefix(filepath.Base(result["pendingBackup"]), ".ghship-backup-") {
			return fmt.Errorf("Windows update reported invalid backup path")
		}
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
	for attempt := 0; ; attempt++ {
		entries, err := os.ReadDir(standaloneDir)
		if err != nil {
			return err
		}
		pending := ""
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".ghship-backup-") {
				pending = entry.Name()
				break
			}
		}
		if pending == "" {
			break
		}
		if attempt == 100 {
			return fmt.Errorf("direct standalone update left a backup: %s", pending)
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Println("PASS direct standalone upgrade without Node or npm")
	if _, e = command(binary, "uninstall", "--standalone", "--json"); e != nil {
		return e
	}
	if _, e = command(fixtureBinary, "install", "--standalone", "--user-only", "--json"); e != nil {
		return e
	}
	npxStandaloneUpdate := func() ([]byte, error) {
		return npm("exec", "--yes", "--package", tarball, "--", "ghship", "update", "--package-source", tarball, "--json")
	}
	if e = testSkillConflicts(npxStandaloneUpdate); e != nil {
		return fmt.Errorf("npx standalone conflict: %w", e)
	}
	originalBinary, e := os.ReadFile(installedBinary)
	if e != nil {
		return e
	}
	if e = os.WriteFile(installedBinary, []byte("modified executable"), 0755); e != nil {
		return e
	}
	if e = refused(npxStandaloneUpdate, "installed executable missing or modified; preserved"); e != nil {
		return fmt.Errorf("npx standalone modified executable: %w", e)
	}
	if e = os.WriteFile(installedBinary, originalBinary, 0755); e != nil {
		return e
	}
	if _, e = npm("install", "-g", fixtureTar, "--ignore-scripts"); e != nil {
		return e
	}
	if e = refused(npxStandaloneUpdate, "Standalone and npm installations both exist; inspect both before updating"); e != nil {
		return fmt.Errorf("npx ambiguous installation: %w", e)
	}
	if _, e = npm("uninstall", "-g", "@ghship/cli"); e != nil {
		return e
	}
	if e = refused(func() ([]byte, error) {
		return npm("exec", "--yes", "--package", tarball, "--", "ghship", "uninstall", "--json")
	}, "Standalone installation exists; uninstall with a separate downloaded executable and --standalone"); e != nil {
		return fmt.Errorf("npx standalone uninstall: %w", e)
	}
	// A standalone user must be able to use the documented npx entry without
	// replacing the installation channel or losing their managed files.
	for _, action := range []string{"update", "install"} {
		args := []string{"exec", "--yes", "--package", tarball, "--", "ghship", action, "--package-source", tarball, "--json"}
		if action == "install" {
			args = append(args, "--user-only")
		}
		if b, e = npm(args...); e != nil {
			return fmt.Errorf("npx %s of standalone installation: %w: %s", action, e, b)
		}
		if e = checkStandaloneVersion(assets.Version); e != nil {
			return e
		}
		if e = verifyInstalledSkill(home, assets.Version); e != nil {
			return e
		}
		if e = checkProject(installedBinary); e != nil {
			return e
		}
	}
	if e = refused(func() ([]byte, error) {
		return npm("exec", "--yes", "--package", fixtureTar, "--", "ghship", "update", "--package-source", fixtureTar, "--json")
	}, "downgrade refused"); e != nil {
		return fmt.Errorf("npx standalone downgrade: %w", e)
	}
	fmt.Println("PASS npx standalone update, reinstall, conflict and downgrade protections")
	for i := 0; i < 2; i++ {
		if _, e = command(binary, "update", "--standalone", "--json"); e != nil {
			return e
		}
		if e = verifyInstalledSkill(home, assets.Version); e != nil {
			return e
		}
		if e = checkStandaloneVersion(assets.Version); e != nil {
			return e
		}
		if e = checkProject(installedBinary); e != nil {
			return e
		}
	}
	if e = checkStandaloneVersion(assets.Version); e != nil {
		return e
	}
	if e = testSkillConflicts(func() ([]byte, error) {
		return command(binary, "update", "--standalone", "--json")
	}); e != nil {
		return fmt.Errorf("standalone conflict: %w", e)
	}
	if e = testSkillConflicts(func() ([]byte, error) {
		return command(binary, "install", "--standalone", "--user-only", "--json")
	}); e != nil {
		return fmt.Errorf("standalone reinstall conflict: %w", e)
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
		err = refused(func() ([]byte, error) { return command(binary, "update", "--standalone", "--json") }, "permission denied")
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
				return command(binary, "update", "--standalone", "--json")
			}, "Access is denied")
		}); e != nil {
			return fmt.Errorf("standalone Windows ACL: %w", e)
		}
		fmt.Println("PASS Windows ACL denied directory creation and preserved installation")
	}
	if e = refused(func() ([]byte, error) {
		return command(fixtureBinary, "update", "--standalone", "--json")
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
	if _, e = command(binary, "uninstall", "--standalone", "--json"); e != nil {
		return e
	}
	if e = checkUninstalled(installedBinary); e != nil {
		return e
	}
	fmt.Println("PASS standalone synthetic baseline upgrade and downgrade refusal")
	for _, action := range []string{"install", "install", "update", "uninstall"} {
		args := []string{action, "--standalone", "--json"}
		if action == "install" {
			args = append(args, "--user-only")
		}
		b, e = command(binary, args...)
		if e != nil {
			return e
		}
		fmt.Printf("PASS standalone %s: %s", action, b)
		if action != "uninstall" {
			if e = verifyInstalledSkill(home, assets.Version); e != nil {
				return e
			}
			if e = checkStandaloneVersion(assets.Version); e != nil {
				return e
			}
			if e = checkProject(installedBinary); e != nil {
				return e
			}
		} else if e = checkUninstalled(installedBinary); e != nil {
			return e
		}
	}
	skill := filepath.Join(home, ".agents", "skills", "ghship")
	if _, e = os.Stat(skill); !os.IsNotExist(e) {
		return fmt.Errorf("skill remains after uninstall")
	}
	if e = checkPreserved(); e != nil {
		return e
	}
	if e = registry.assertDownloads(target); e != nil {
		return e
	}
	fmt.Println("PASS only matching platform binary downloaded")
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

func runCommand(env []string, name string, args ...string) ([]byte, error) {
	c := exec.Command(name, args...)
	c.Env = env
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	err := c.Run()
	if err != nil {
		output := append(stdout.Bytes(), stderr.Bytes()...)
		return output, fmt.Errorf("%s %v failed: %w: %s", name, args, err, output)
	}
	if stderr.Len() != 0 {
		_, _ = os.Stderr.Write(stderr.Bytes())
	}
	return stdout.Bytes(), nil
}
