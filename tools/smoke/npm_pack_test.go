package main

import "testing"

func TestParseNpmPack(t *testing.T) {
	for _, input := range []string{`[{"name":"@ghship/cli","version":"1.0.4","filename":"file.tgz"}]`, `{"@ghship/cli":{"name":"@ghship/cli","version":"1.0.4","filename":"file.tgz"}}`} {
		got, err := parseNpmPack([]byte(input), "@ghship/cli")
		if err != nil || got.Filename != "file.tgz" {
			t.Fatalf("valid pack: %+v %v", got, err)
		}
	}
	for _, input := range []string{`[]`, `[{"name":"@ghship/cli"},{"name":"@ghship/cli"}]`, `{"@wrong":{"name":"@ghship/cli"}}`} {
		if _, err := parseNpmPack([]byte(input), "@ghship/cli"); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
}
