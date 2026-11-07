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
			dir := installDir(t)
			for _, jsonMode := range []bool{false, true} {
				args := []string{action, "--dir", dir}
				if jsonMode {
					args = append(args, "--json")
				}
				var out bytes.Buffer
				err := run(args, nil, &out)
				var ce cliError
				if !errors.As(err, &ce) || ce.code != 4 || !strings.Contains(err.Error(), "npx @ghship/cli@latest "+action) {
					t.Fatalf("%v: %v", args, err)
				}
				if jsonMode {
					var v struct {
						Ready bool     `json:"ready"`
						Next  []string `json:"next"`
					}
					if e := json.Unmarshal(out.Bytes(), &v); e != nil || v.Ready || len(v.Next) != 1 || v.Next[0] != "npx @ghship/cli@latest "+action {
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
