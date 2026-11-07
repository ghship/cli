package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLifecycleArgumentsNeverMutateOnHelpOrUsage(t *testing.T) {
	dir := installDir(t)
	bin := filepath.Join(filepath.Dir(dir), "bin")
	base := []string{"--standalone", "--dir", dir, "--bin-dir", bin}
	for _, flag := range []string{"--help", "-h"} {
		var out bytes.Buffer
		args := append(append([]string{"install"}, base...), "--user-only", flag)
		if err := run(args, nil, &out); err != nil || !strings.Contains(out.String(), "ghship install") {
			t.Fatalf("help %q: %v %s", flag, err, out.String())
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatal("help changed skill")
		}
		if _, err := os.Stat(bin); !os.IsNotExist(err) {
			t.Fatal("help changed executable")
		}
	}
	for _, args := range [][]string{
		{"install", "--standalone", "--dir", dir, "--bin-dir", bin, "--unknown"},
		{"install", "--standalone", "--dir", dir, "--bin-dir", bin, "--acceptance-pr", "bad"},
		{"install", "--standalone", "--user-only", "--dir", dir, "--project", dir},
		{"update", "--standalone", "--dir", dir, "--project", dir},
		{"install", "--check", "--dir", dir, "--author", "bad@user"},
		{"install", "--standalone", "--dir", dir, "--author", "bad@user"},
		{"install", "--standalone", "--dir", dir, "--author", "bad--name"},
		{"install", "--standalone", "--dir", dir, "--author", "bad-"},
		{"install", "--standalone", "--dir", dir, "--repo", "github.com/bad owner/repo"},
		{"install", "--standalone", "--dir", dir, "--repo", "github.com/owner/bad repo"},
		{"install", "--standalone", "--dir", dir, "--acceptance-pr", "-1"},
		{"install", "--standalone", "--dir", dir, "--acceptance-pr", "0"},
		{"install", "--standalone", "--dir", dir, "--author", "Alice", "--reviewer", "alice"},
		{"install", "--standalone", "--dir", dir, "--remove-binding", "--author", "alice"},
		{"install", "--standalone", "--dir", dir, "--json=true"},
		{"install", "--standalone", "--dir", dir, "-json"},
		{"uninstall", "--standalone", "--dir", dir, "--user-only"},
		{"install", "--skill-only", "--standalone", "--dir", dir},
		{"uninstall", "--standalone", "--dir", dir, "--unknown"},
	} {
		err := run(args, nil, &bytes.Buffer{})
		var ce cliError
		if !errors.As(err, &ce) || ce.code != 2 {
			t.Fatalf("%v: want usage error, got %v", args, err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%v changed absent installation", args)
		}
	}
	if err := run([]string{"install", "--check", "--dir", dir, "--repo", "github.com/owner-name/repo.Name_2", "--author", "alice-1", "--reviewer", "bob", "--acceptance-pr", "1"}, nil, &bytes.Buffer{}); err != nil {
		t.Fatalf("valid boundary options rejected: %v", err)
	}
	for _, option := range []string{"--project", "--repo", "--author", "--reviewer", "--admin", "--acceptance-pr"} {
		err := run([]string{"install", "--standalone", "--dir", dir, option, ""}, nil, &bytes.Buffer{})
		var ce cliError
		if !errors.As(err, &ce) || ce.code != 2 {
			t.Fatalf("%s empty: %v", option, err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s empty changed installation", option)
		}
	}
	if err := run(append(append([]string{"install"}, base...), "--user-only"), nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"uninstall", "--standalone", "--dir", dir, "--unknown"},
		{"uninstall", "--standalone", "--dir", dir, "--user-only"},
		{"update", "--standalone", "--dir", dir, "--project", dir},
		{"uninstall", "--standalone", "--dir", dir, "--help"},
	} {
		err := run(args, nil, &bytes.Buffer{})
		if args[len(args)-1] == "--help" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatalf("%v succeeded", args)
		}
		after, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%v damaged installation: %v", args, err)
		}
	}
	for _, option := range []string{"--project", "--repo", "--author", "--reviewer", "--admin", "--acceptance-pr"} {
		err := run([]string{"install", "--standalone", "--dir", dir, option, ""}, nil, &bytes.Buffer{})
		var ce cliError
		if !errors.As(err, &ce) || ce.code != 2 {
			t.Fatalf("%s empty existing: %v", option, err)
		}
		after, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
		if err != nil || !bytes.Equal(before, after) {
			t.Fatalf("%s empty damaged installation: %v", option, err)
		}
	}
	if err := run([]string{"uninstall", "--standalone", "--dir", dir}, nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestHelpRecognitionRespectsValuesAndNativeArguments(t *testing.T) {
	for _, args := range [][]string{{"doctor", "--scope", "user", "--help"}, {"user", "list", "--json", "-h"}, {"project", "status", "--project", ".", "--help"}, {"help", "--help"}} {
		var out bytes.Buffer
		if err := run(args, nil, &out); err != nil || out.Len() == 0 {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{{"install", "--dir", "--help"}, {"user", "add", "--token", "--help"}, {"gh", "author", "--", "auth", "--help"}, {"git", "author", "--", "--help"}, {"unknown", "--help"}} {
		if commandHelpRequested(args) {
			t.Fatalf("help mistaken for value/native argument: %v", args)
		}
	}
}
