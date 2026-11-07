package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestExternalLifecycleEntryJSONAndDiagnostics(t *testing.T) {
	for _, action := range []string{"update", "uninstall"} {
		t.Run(action, func(t *testing.T) {
			dir := installDir(t) + " path ' $()"
			for _, jsonMode := range []bool{false, true} {
				args := []string{action, "--dir", dir}
				if jsonMode {
					args = append(args, "--json")
				}
				var out bytes.Buffer
				err := run(args, nil, &out)
				var ce cliError
				if !errors.As(err, &ce) || ce.code != 4 || !strings.Contains(err.Error(), "npx @ghship/cli@latest "+action) || !strings.Contains(err.Error(), "original installation directory") {
					t.Fatalf("%v: %v", args, err)
				}
				if strings.Contains(err.Error(), dir) {
					t.Fatal("untrusted path was interpolated in shell guidance")
				}
				if jsonMode {
					var v struct {
						Ready   bool     `json:"ready"`
						Next    []string `json:"next"`
						Command struct {
							Program string   `json:"program"`
							Args    []string `json:"args"`
						} `json:"command"`
					}
					if e := json.Unmarshal(out.Bytes(), &v); e != nil || v.Ready || len(v.Next) != 1 || !strings.Contains(v.Next[0], "original installation directory") || v.Command.Program != "npx" || len(v.Command.Args) != 5 || v.Command.Args[2] != "--dir" || v.Command.Args[3] != dir || v.Command.Args[4] != "--json" {
						t.Fatalf("invalid JSON %q: %v", out.String(), e)
					}
				} else if out.Len() != 0 {
					t.Fatalf("diagnostics on stdout: %q", out.String())
				}
				if _, e := os.Stat(dir); !os.IsNotExist(e) {
					t.Fatalf("directory changed: %v", e)
				}
			}
		})
	}
	var out bytes.Buffer
	_ = externalLifecycleEntry("update", []string{"--dir", "--json"}, &out)
	if out.Len() != 0 {
		t.Fatalf("path value mistaken for --json: %s", out.String())
	}
}
