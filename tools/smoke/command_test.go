package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandProcess(t *testing.T) {
	mode := os.Getenv("GHSHIP_TEST_COMMAND_MODE")
	if mode == "" {
		return
	}
	switch mode {
	case "notice":
		fmt.Fprint(os.Stdout, `[{"filename":"candidate.tgz"}]`)
		fmt.Fprint(os.Stderr, "npm notice: update available\n")
	case "fail":
		fmt.Fprint(os.Stdout, "partial output")
		fmt.Fprint(os.Stderr, "npm error: cannot pack\n")
		os.Exit(7)
	case "invalid":
		fmt.Fprint(os.Stdout, "not JSON")
	}
	os.Exit(0)
}

func TestCommandStreams(t *testing.T) {
	run := func(mode string) ([]byte, error) {
		env := append(os.Environ(), "GHSHIP_TEST_COMMAND_MODE="+mode)
		return runCommand(env, os.Args[0], "-test.run=^TestCommandProcess$")
	}
	t.Run("success with stderr notice", func(t *testing.T) {
		out, err := run("notice")
		if err != nil {
			t.Fatal(err)
		}
		var entries []struct{ Filename string }
		if err = json.Unmarshal(out, &entries); err != nil || len(entries) != 1 || entries[0].Filename != "candidate.tgz" {
			t.Fatalf("stdout was not valid pack JSON: %q (%v)", out, err)
		}
	})
	t.Run("failure preserves diagnostics and exit", func(t *testing.T) {
		out, err := run("fail")
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 7 || !strings.Contains(string(out), "npm error: cannot pack") || !strings.Contains(err.Error(), "npm error: cannot pack") {
			t.Fatalf("missing failure details: output=%q error=%v", out, err)
		}
	})
	t.Run("invalid JSON remains invalid", func(t *testing.T) {
		out, err := run("invalid")
		if err != nil || json.Valid(out) {
			t.Fatalf("invalid output should not be accepted: %q (%v)", out, err)
		}
	})
	t.Run("missing executable reports start failure", func(t *testing.T) {
		_, err := runCommand(os.Environ(), filepath.Join(t.TempDir(), "missing-command"))
		if err == nil {
			t.Fatal("missing executable should fail")
		}
	})
}
