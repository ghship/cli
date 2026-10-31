package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Exercise the actual packaged CLI without granting it a GitHub identity.
// The gh stub supplies only doctor capabilities; authentication always fails.
func projectLifecycle(tmp, home, project, binary string, env []string) error {
	stubDir := filepath.Join(tmp, "gh-stub")
	if err := os.Mkdir(stubDir, 0700); err != nil {
		return err
	}
	name := "gh"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	build := exec.Command("go", "build", "-o", filepath.Join(stubDir, name), "./tools/smoke/testdata/ghstub")
	if output, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build gh stub: %w: %s", err, output)
	}
	testEnv := make([]string, 0, len(env)+1)
	path := ""
	for _, item := range env {
		if strings.EqualFold(strings.SplitN(item, "=", 2)[0], "PATH") {
			path = strings.SplitN(item, "=", 2)[1]
			continue
		}
		testEnv = append(testEnv, item)
	}
	testEnv = append(testEnv, "PATH="+stubDir+string(os.PathListSeparator)+path)
	run := func(executable string, args ...string) ([]byte, error) {
		cmd := exec.Command(executable, args...)
		cmd.Env = testEnv
		output, err := cmd.CombinedOutput()
		if err != nil {
			return output, fmt.Errorf("%s failed: %w", filepath.Base(executable), err)
		}
		return output, nil
	}
	if _, err := run("git", "-C", project, "remote", "add", "origin", "https://github.com/example/project.git"); err != nil {
		return fmt.Errorf("create test origin: %w", err)
	}
	agentsPath := filepath.Join(project, "AGENTS.md")
	originalAgents := []byte("# User rules\nKeep this content.\n")
	if err := os.WriteFile(agentsPath, originalAgents, 0644); err != nil {
		return err
	}
	status := func(installed bool) error {
		b, err := run(binary, "project", "status", "--project", project, "--json")
		if err != nil {
			return fmt.Errorf("project status: %w", err)
		}
		var result struct {
			Installed bool `json:"installed"`
			Bound     bool `json:"bound"`
			Owned     bool `json:"managedFilesUnmodified"`
		}
		if err = json.Unmarshal(b, &result); err != nil || result.Installed != installed || result.Bound || result.Owned != installed {
			return fmt.Errorf("unexpected project state")
		}
		return nil
	}
	if err := status(false); err != nil {
		return err
	}
	setupArgs := []string{"--project", project, "--repo", "github.com/example/project", "--author", "developer", "--reviewer", "reviewer", "--admin", "admin", "--non-interactive", "--json"}
	for _, prefix := range [][]string{{"project", "install"}, {"init"}} {
		args := append(append([]string{}, prefix...), setupArgs...)
		output, err := run(binary, args...)
		if err = expectRefusal(output, err, 1, "administrator gh login unavailable"); err != nil {
			return fmt.Errorf("controlled unauthenticated setup: %w", err)
		}
		if err = status(true); err != nil {
			return err
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := run(binary, "project", "update", "--project", project, "--json"); err != nil {
			return fmt.Errorf("project update: %w", err)
		}
		if err := status(true); err != nil {
			return err
		}
	}
	readme := filepath.Join(project, ".ghship", "README.md")
	originalReadme, err := os.ReadFile(readme)
	if err != nil {
		return err
	}
	if err = os.WriteFile(readme, []byte("user modified instructions\n"), 0644); err != nil {
		return err
	}
	roots := []string{project, filepath.Join(home, ".config", "ghship"), filepath.Join(home, "AppData", "ghship")}
	before, err := snapshotTrees(roots...)
	if err != nil {
		return err
	}
	for _, action := range []string{"update", "uninstall"} {
		output, failure := run(binary, "project", action, "--project", project, "--json")
		if err = expectRefusal(output, failure, 1, "project instructions changed; preserved"); err != nil {
			return fmt.Errorf("modified project %s: %w", action, err)
		}
		if err = unchangedTrees(before, roots...); err != nil {
			return err
		}
	}
	if err = os.WriteFile(readme, originalReadme, 0644); err != nil {
		return err
	}
	if _, err = run(binary, "project", "uninstall", "--project", project, "--json"); err != nil {
		return fmt.Errorf("project uninstall: %w", err)
	}
	if err = status(false); err != nil {
		return err
	}
	afterAgents, err := os.ReadFile(agentsPath)
	if err != nil || !bytes.Contains(afterAgents, originalAgents) || bytes.Contains(afterAgents, []byte("<!-- ghship:begin -->")) {
		return fmt.Errorf("user rules not preserved after uninstall")
	}
	for _, p := range []string{readme, filepath.Join(project, ".ghship", "install.json")} {
		if _, err = os.Stat(p); !os.IsNotExist(err) {
			return fmt.Errorf("managed project file remains")
		}
	}
	if _, err = run(binary, "project", "uninstall", "--project", project, "--json"); err != nil {
		return fmt.Errorf("repeated project uninstall: %w", err)
	}
	config := filepath.Join(home, ".config", "ghship")
	if runtime.GOOS == "windows" {
		config = filepath.Join(home, "AppData", "ghship")
	}
	accounts := filepath.Join(config, "accounts", "example.invalid")
	other := filepath.Join(accounts, "other.json")
	if err = os.WriteFile(other, []byte(`{"schemaVersion":1,"host":"example.invalid","login":"other","secret":"ZmFrZQ=="}`), 0600); err != nil {
		return err
	}
	before, err = snapshotTrees(roots...)
	if err != nil {
		return err
	}
	output, failure := run(binary, "user", "add", "--host", "example.invalid", "--user", "other", "--non-interactive", "--json")
	if err = expectRefusal(output, failure, 1, "account exists; use ghship user update"); err != nil {
		return fmt.Errorf("duplicate account: %w", err)
	}
	if err = unchangedTrees(before, roots...); err != nil {
		return err
	}
	if _, err = run(binary, "user", "delete", "--host", "example.invalid", "--user", "other", "--json"); err != nil {
		return fmt.Errorf("delete isolated account: %w", err)
	}
	if _, err = os.Stat(other); !os.IsNotExist(err) {
		return fmt.Errorf("isolated account not removed")
	}
	before, err = snapshotTrees(roots...)
	if err != nil {
		return err
	}
	output, failure = run(binary, "user", "delete", "--host", "example.invalid", "--user", "other", "--json")
	if err = expectRefusal(output, failure, 1, "account unavailable; use ghship user add"); err != nil {
		return fmt.Errorf("repeated account deletion: %w", err)
	}
	if err = unchangedTrees(before, roots...); err != nil {
		return err
	}
	fmt.Println("PASS packaged project and offline account lifecycle; no real GitHub authentication")
	return nil
}
