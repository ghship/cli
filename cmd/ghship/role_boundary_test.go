package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestExplicitDispatcherRefusesOutsideProject(t *testing.T) {
	base, root := fixture(t)
	setupAuthHome(t)
	t.Setenv("GHSHIP_DISPATCHER", "dispatcher")
	t.Setenv("TEST_ACTOR", "dispatcher")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	// Git environment variables can make rev-parse report a work tree even
	// though the physical current directory is outside it.
	t.Setenv("GIT_DIR", filepath.Join(root, ".git"))
	t.Setenv("GIT_WORK_TREE", root)
	if err := github([]string{"dispatcher", "--", "issue", "list"}, nil, &bytes.Buffer{}); err == nil {
		t.Error("gh executed outside repository")
	}
	if err := roleGit([]string{"dispatcher", "--", "--version"}, nil, &bytes.Buffer{}); err == nil {
		t.Error("git executed outside repository")
	}
	if _, err := os.Stat(os.Getenv("CALL_DATA")); !os.IsNotExist(err) {
		t.Fatalf("native gh invoked: %v", err)
	}
}

func TestExplicitDispatcherRespectsExistingBindingOrigin(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	t.Setenv("GHSHIP_DISPATCHER", "dispatcher")
	t.Setenv("TEST_ACTOR", "dispatcher")
	t.Setenv("TEST_ORIGIN", "https://github.com/other/repo.git")
	binding := projectBinding{SchemaVersion: 1, Root: root, Repo: "github.com/fixture/repo", Author: "author", Reviewer: "reviewer", Admin: "dispatcher"}
	data, _ := json.Marshal(binding)
	name, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = atomicConfig(name, data); err != nil {
		t.Fatal(err)
	}
	if err = github([]string{"dispatcher", "--", "issue", "list"}, nil, &bytes.Buffer{}); err == nil {
		t.Error("origin mismatch accepted")
	}
	if _, err = os.Stat(os.Getenv("CALL_DATA")); !os.IsNotExist(err) {
		t.Fatalf("native gh invoked: %v", err)
	}
}

func TestExplicitRolesUseTargetHostAndRejectInvalidBinding(t *testing.T) {
	base, root := fixture(t)
	setupAuthHome(t)
	token := filepath.Join(base, "fixture-token")
	if err := os.WriteFile(token, []byte("synthetic"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_ORIGIN", "https://git.example.com/fixture/repo.git")
	for _, role := range []string{"author", "reviewer", "dispatcher"} {
		t.Setenv("GHSHIP_AUTHOR", "")
		t.Setenv("GHSHIP_REVIEWER", "")
		t.Setenv("GHSHIP_DISPATCHER", "")
		t.Setenv("GHSHIP_AUTHOR_PAT_FILE", "")
		t.Setenv("GHSHIP_REVIEWER_PAT_FILE", "")
		t.Setenv("GHSHIP_"+strings.ToUpper(role), role)
		t.Setenv("TEST_ACTOR", role)
		if role != "dispatcher" {
			t.Setenv("GHSHIP_"+strings.ToUpper(role)+"_PAT_FILE", token)
		}
		env, err := roleEnvironment(role)
		if err != nil {
			t.Fatalf("%s: %v", role, err)
		}
		if !slices.Contains(env, "GH_HOST=git.example.com") {
			t.Fatalf("%s missing target host", role)
		}
	}
	name, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(name, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHSHIP_DISPATCHER", "dispatcher")
	t.Setenv("TEST_ACTOR", "dispatcher")
	if _, err = roleEnvironment("dispatcher"); err == nil {
		t.Fatal("invalid project binding accepted")
	}
}

func TestOverrideDistinguishesAbsentOriginFromReadFailure(t *testing.T) {
	_, _ = fixture(t)
	setupAuthHome(t)
	t.Setenv("GHSHIP_DISPATCHER", "dispatcher")
	t.Setenv("TEST_ACTOR", "dispatcher")
	t.Setenv("TEST_GIT_ORIGIN_ERROR", "1")
	if _, err := roleEnvironment("dispatcher"); err == nil {
		t.Fatal("origin read failure accepted")
	}
	t.Setenv("TEST_GIT_ORIGIN_ERROR", "")
	t.Setenv("TEST_NO_ORIGIN", "1")
	env, err := roleEnvironment("dispatcher")
	if err != nil || !slices.Contains(env, "GH_HOST=github.com") {
		t.Fatalf("unbound repository without origin: %v", err)
	}
}
