package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIndependentProjectNamespacePreservesOtherIntegration(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	if err := lifecycle("install", nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	legacy := "# Existing rules\n<!-- foreign-tool:begin -->\nlegacy integration\n<!-- foreign-tool:end -->\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(root, ".foreign-tool", "install.json")
	if err := os.MkdirAll(filepath.Dir(old), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old project state"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := installProjectFiles(root, false); err != nil {
		t.Fatal(err)
	}
	agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if !strings.Contains(string(agents), legacy) || !strings.Contains(string(agents), "<!-- ghship:begin -->") {
		t.Fatalf("both integrations must coexist")
	}
	if err := project([]string{"update", "--project", root}, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := project([]string{"uninstall", "--project", root}, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	oldAfter, _ := os.ReadFile(old)
	if !strings.Contains(string(after), legacy) || strings.Contains(string(after), "<!-- ghship:begin -->") || string(oldAfter) != "old project state" {
		t.Fatal("other integration not preserved")
	}
}

func TestIndependentConfigNamespace(t *testing.T) {
	home := setupAuthHome(t)
	got, err := configRoot()
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(home, ".config", "ghship")
	if runtime.GOOS == "windows" {
		expected = filepath.Join(home, "AppData", "ghship")
	}
	if got != expected {
		t.Fatalf("unexpected config root: %s", got)
	}
	oldRoot := filepath.Join(home, ".config", "foreign-tool")
	if runtime.GOOS == "windows" {
		oldRoot = filepath.Join(home, "AppData", "foreign-tool")
	}
	oldAccount := filepath.Join(oldRoot, "accounts", "fixture.json")
	if err := os.MkdirAll(filepath.Dir(oldAccount), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldAccount, []byte("old account data"), 0600); err != nil {
		t.Fatal(err)
	}
	_, root := fixture(t)
	if err := initialize([]string{"--project", root, "--remove-binding"}, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(oldAccount)
	if err != nil || string(after) != "old account data" {
		t.Fatal("other product account changed", err)
	}
}

func TestForeignRoleOverridesAreNotUsed(t *testing.T) {
	setupAuthHome(t)
	t.Setenv("GHSHIP_AUTHOR", "")
	t.Setenv("GHSHIP_AUTHOR_PAT_FILE", "")
	t.Setenv("FOREIGN_TOOL_AUTHOR", "old-author")
	t.Setenv("FOREIGN_AUTHOR_PAT_FILE", "/old-path-must-not-be-read")
	if _, err := roleEnvironment("author"); err == nil {
		t.Fatal("unexpected role from old product environment")
	}
}
