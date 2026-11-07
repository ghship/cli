package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectOperationsIgnoreGitEnvironmentRedirects(t *testing.T) {
	base, root := fixture(t)
	setupAuthHome(t)
	other := filepath.Join(base, "other")
	gitCmd(t, "init", "--quiet", other)
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	for _, command := range [][]string{{"status", "--project", root, "--json"}, {"skill", "--show", "--project", root, "--json"}} {
		var out bytes.Buffer
		if err := project(command, nil, &out); err != nil {
			t.Fatal(err)
		}
		var got struct {
			Project string `json:"project"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Project != root {
			t.Fatalf("wrong project for %v: %v", command, err)
		}
	}
	var out bytes.Buffer
	if err := project([]string{"skill", "--create", "--project", root, "--json"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".ghship", "skill.md")); err != nil {
		t.Fatal("intended project unchanged", err)
	}
	if _, err := os.Stat(filepath.Join(other, ".ghship", "skill.md")); !os.IsNotExist(err) {
		t.Fatal("other project changed", err)
	}
}

func TestTokenFileScopeUsesPhysicalFilePathWithoutGit(t *testing.T) {
	base, root := fixture(t)
	setupAuthHome(t)
	other := filepath.Join(base, "other")
	gitCmd(t, "init", "--quiet", other)
	inside := filepath.Join(root, "input.json")
	if err := os.WriteFile(inside, []byte(`{"token":"synthetic"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	if _, err := readTokenFile(inside); err == nil || !strings.Contains(err.Error(), "outside repository") {
		t.Fatalf("repository input accepted: %v", err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := readTokenFile(inside); err == nil || !strings.Contains(err.Error(), "outside repository") {
		t.Fatalf("repository input accepted without Git: %v", err)
	}
	external := filepath.Join(outside, "input.json")
	if err := os.WriteFile(external, []byte(`{"token":"synthetic"}`), 0600); err != nil {
		t.Fatal(err)
	}
	token, err := readTokenFile(external)
	if err != nil || token != "synthetic" {
		t.Fatalf("safe external input rejected: %v", err)
	}
}

func TestTidyChecksPhysicalWorkingTree(t *testing.T) {
	base, _ := fixture(t)
	setupAuthHome(t)
	other := filepath.Join(base, "other")
	gitCmd(t, "init", "--quiet", other)
	if err := os.WriteFile(filepath.Join(other, "untracked"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("PR_DATA"), []byte(`{"state":"OPEN"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_WORK_TREE", other)
	var out bytes.Buffer
	err := tidy([]string{"1", "--json"}, &out)
	if err == nil || !strings.Contains(err.Error(), "requires merged") {
		t.Fatalf("wrong working tree checked: %v", err)
	}
	if _, err = os.Stat(filepath.Join(other, "untracked")); err != nil {
		t.Fatal("other project changed", err)
	}
}

func TestTokenFileRejectsLinkedWorktreeAndBareRepository(t *testing.T) {
	base, root := fixture(t)
	setupAuthHome(t)
	gitCmd(t, "-C", root, "commit", "--allow-empty", "-m", "fixture")
	linked := filepath.Join(base, "linked")
	gitCmd(t, "-C", root, "worktree", "add", "--detach", linked)
	bare := filepath.Join(base, "bare.git")
	gitCmd(t, "init", "--bare", "--quiet", bare)
	for _, dir := range []string{linked, bare} {
		input := filepath.Join(dir, "input.json")
		if err := os.WriteFile(input, []byte(`{"token":"synthetic"}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := readTokenFile(input); err == nil || !strings.Contains(err.Error(), "outside repository") {
			t.Fatalf("repository input accepted in %s: %v", dir, err)
		}
	}
}

func TestEmptyExplicitProjectIsRejected(t *testing.T) {
	fixture(t)
	for _, command := range [][]string{{"status", "--project", ""}, {"skill", "--show", "--project", ""}} {
		if err := project(command, nil, &bytes.Buffer{}); err == nil {
			t.Fatalf("empty project accepted: %v", command)
		}
	}
	if err := initialize([]string{"--project", "", "--remove-binding"}, nil, &bytes.Buffer{}); err == nil {
		t.Fatal("empty init project accepted")
	}
}
