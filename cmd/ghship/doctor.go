package main

import (
	"errors"
	"flag"
	"io"
	"os/exec"
	"runtime"
	"strings"
)

const doctorHelp = `ghship doctor [--scope user|project] [--json]
project (default): checks Git, gh and native Issue parent/dependency capabilities.
user: checks supported OS/architecture; npm distribution also needs Node/npm.
No software is installed. Missing/unusable tools return 4 with official install URLs.
Business-specific tools must be declared by the project, never guessed.
`

func doctor(args []string, out io.Writer) error {
	f := flag.NewFlagSet("doctor", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	scope := f.String("scope", "project", "scope")
	j := f.Bool("json", false, "JSON")
	if e := f.Parse(args); e != nil || f.NArg() != 0 || (*scope != "user" && *scope != "project") {
		return usage("doctor --scope must be user or project")
	}
	checks := []map[string]any{}
	missing := []string{}
	next := []string{}
	platform := (runtime.GOOS == "darwin" || runtime.GOOS == "linux" || runtime.GOOS == "windows") && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64")
	checks = append(checks, map[string]any{"tool": "platform", "available": platform, "value": runtime.GOOS + "/" + runtime.GOARCH})
	if !platform {
		missing = append(missing, "supported platform")
	}
	if *scope == "project" {
		for _, tool := range []struct{ name, url string }{{"git", "https://git-scm.com/downloads"}, {"gh", "https://cli.github.com/"}} {
			p, e := exec.LookPath(tool.name)
			available := e == nil
			value := ""
			if available {
				value, e = output(nil, tool.name, "--version")
				available = e == nil && strings.TrimSpace(value) != ""
			}
			checks = append(checks, map[string]any{"tool": tool.name, "available": available, "path": p, "version": value, "installURL": tool.url})
			if !available {
				missing = append(missing, tool.name)
				next = append(next, "Install "+tool.name+": "+tool.url)
			}
		}
		if _, e := exec.LookPath("gh"); e == nil {
			help, e := output(cleanGHEnv(), "gh", "issue", "create", "--help")
			native := e == nil && strings.Contains(help, "--parent") && strings.Contains(help, "--blocked-by")
			checks = append(checks, map[string]any{"tool": "gh native Issue relationships", "available": native})
			if !native {
				missing = append(missing, "gh native parent/dependency support")
				next = append(next, "Upgrade gh: https://cli.github.com/")
			}
		}
	}
	result := map[string]any{"scope": *scope, "checks": checks, "ready": len(missing) == 0, "missing": missing, "next": next}
	if e := emitResult(out, *j, result); e != nil {
		return e
	}
	if len(missing) > 0 {
		return cliError{4, errPrerequisites}
	}
	return nil
}

var errPrerequisites = errors.New("required tools unavailable; install or upgrade them, then rerun ghship doctor")
