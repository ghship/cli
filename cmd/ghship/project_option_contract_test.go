package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRejectsInapplicableOptions(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	for _, args := range [][]string{
		{"status", "--project", root, "--binding-only", "--json"},
		{"update", "--project", root, "--binding-only=false", "--json"},
		{"skill", "--project", root, "--show", "--mode", "supplement", "--json"},
		{"skill", "--project", root, "--show", "--overrides", "testing", "--json"},
	} {
		err := project(args, nil, &bytes.Buffer{})
		var ce cliError
		if !errors.As(err, &ce) || ce.code != 2 {
			t.Errorf("%v: want usage error, got %v", args, err)
		}
	}
}

func TestInitBindingRemovalRejectsIgnoredOptions(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	path, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("preserve binding")
	if err = os.WriteFile(path, sentinel, 0600); err != nil {
		t.Fatal(err)
	}
	err = initialize([]string{"--project", root, "--remove-binding", "--author", "alice", "--json"}, nil, &bytes.Buffer{})
	var ce cliError
	if !errors.As(err, &ce) || ce.code != 2 {
		t.Errorf("want usage error, got %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, sentinel) {
		t.Fatalf("binding changed: %v", err)
	}
}

func TestProjectBindingOnlyIsSafeAndRepeatable(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	path, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("test binding"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err = project([]string{"uninstall", "--project", root, "--binding-only", "--json"}, nil, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("binding remains: %v", err)
	}
}

func TestProjectInstallRejectsIgnoredBindingOptions(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	path, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	before := []byte("preserve binding")
	if err = os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	err = project([]string{"install", "--project", root, "--remove-binding", "--author", "alice"}, nil, &bytes.Buffer{})
	var ce cliError
	if !errors.As(err, &ce) || ce.code != 2 {
		t.Fatalf("expected usage error: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("binding changed: %v", err)
	}
}
