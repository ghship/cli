package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRefusalProcess(t *testing.T) {
	if os.Getenv("GHSHIP_TEST_REFUSAL_HELPER") != "1" {
		return
	}
	fmt.Fprintln(os.Stderr, os.Getenv("GHSHIP_TEST_REFUSAL_MESSAGE"))
	if os.Getenv("GHSHIP_TEST_REFUSAL_EXIT") == "2" {
		os.Exit(2)
	}
	os.Exit(1)
}

func runRefusalProcess(t *testing.T, message, code string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRefusalProcess$")
	cmd.Env = append(os.Environ(), "GHSHIP_TEST_REFUSAL_HELPER=1", "GHSHIP_TEST_REFUSAL_MESSAGE="+message, "GHSHIP_TEST_REFUSAL_EXIT="+code)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	return output.Bytes(), err
}

func TestRefusalRequiresExpectedReasonAndExit(t *testing.T) {
	output, err := runRefusalProcess(t, "downgrade refused", "1")
	if e := expectRefusal(output, err, 1, "downgrade refused"); e != nil {
		t.Fatal(e)
	}
	output, err = runRefusalProcess(t, "unrelated network error", "1")
	if e := expectRefusal(output, err, 1, "downgrade refused"); e == nil {
		t.Fatal("unrelated error accepted as a refusal")
	}
	output, err = runRefusalProcess(t, "downgrade refused", "2")
	if e := expectRefusal(output, err, 1, "downgrade refused"); e == nil {
		t.Fatal("wrong exit status accepted as a refusal")
	}
	if e := expectRefusal(nil, errors.New("executable unavailable"), 1, "downgrade refused"); e == nil {
		t.Fatal("startup error accepted as a refusal")
	}
	if e := expectRefusal([]byte("downgrade refused"), nil, 1, "downgrade refused"); e == nil {
		t.Fatal("success accepted as a refusal")
	}
}

func TestSnapshotDetectsChanges(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "owned")
	if e := os.WriteFile(p, []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	before, e := snapshotTrees(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = unchangedTrees(before, root); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(p, []byte("modified"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = unchangedTrees(before, root); e == nil {
		t.Fatal("modified content not detected")
	}
	if e = os.WriteFile(p, []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "unexpected"), []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = unchangedTrees(before, root); e == nil {
		t.Fatal("unexpected file not detected")
	}
}
