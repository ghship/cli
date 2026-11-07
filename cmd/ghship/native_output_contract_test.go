package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeGhOutputIsUnfilteredAndHelpExplainsIt(t *testing.T) {
	base, _ := fixture(t)
	setupAuthHome(t)
	token := filepath.Join(base, "synthetic-credential")
	if err := os.WriteFile(token, []byte("synthetic-credential"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHSHIP_AUTHOR", "author")
	t.Setenv("GHSHIP_AUTHOR_PAT_FILE", token)
	t.Setenv("TEST_ACTOR", "author")
	var out bytes.Buffer
	if err := github([]string{"author", "--", "auth", "token"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	// The gh test double returns only a synthetic value; it is deliberately not logged.
	if strings.TrimSpace(out.String()) != "admin" {
		t.Fatal("native output was not preserved")
	}
	if !strings.Contains(ghHelp, "Native gh output is unfiltered") || !strings.Contains(gitHelp, "Native Git output is unfiltered") {
		t.Fatal("native output disclosure missing from help")
	}
}
