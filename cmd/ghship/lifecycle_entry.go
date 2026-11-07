package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func lifecycleEntry(action string, args []string, in io.Reader, out io.Writer) error {
	skillOnly, userOnly, check := false, false, false
	life := []string{}
	initArgs := []string{}
	dir := ""
	standalone := false
	for i := 0; i < len(args); i++ {
		s := args[i]
		switch s {
		case "--skill-only":
			skillOnly = true
		case "--user-only":
			userOnly = true
		case "--check":
			check = true
		case "--standalone":
			standalone = true
			life = append(life, s)
		case "--dir", "--bin-dir":
			if i+1 == len(args) || args[i+1] == "" {
				return usage(s + " requires a path")
			}
			life = append(life, s, args[i+1])
			if s == "--dir" {
				dir = args[i+1]
			}
			i++
		case "--json":
			life = append(life, s)
			initArgs = append(initArgs, s)
		default:
			name := strings.TrimLeft(strings.SplitN(s, "=", 2)[0], "-")
			valueFlag := map[string]bool{"project": true, "repo": true, "author": true, "reviewer": true, "admin": true, "acceptance-pr": true}[name]
			initArgs = append(initArgs, s)
			if valueFlag && !strings.Contains(s, "=") && i+1 < len(args) {
				i++
				initArgs = append(initArgs, args[i])
			}
		}
	}
	if err := validateLifecycleArguments(action, initArgs, skillOnly, userOnly, standalone, check, life); err != nil {
		return err
	}
	if !standalone && !skillOnly && action == "uninstall" {
		if _, e := io.WriteString(out, "Use npx @ghship/cli@latest "+action+" to manage npm CLI and skill together.\n"); e != nil {
			return e
		}
		return cliError{4, errors.New("external npm lifecycle entry required")}
	}
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	if dir == "" {
		dir = filepath.Join(home, ".agents", "skills", "ghship")
	}
	if e = noLinks(dir); e != nil {
		return e
	}
	manifestPath := filepath.Join(dir, ".ghship.json")
	b, e := os.ReadFile(manifestPath)
	managed := e == nil
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	var m manifest
	if managed {
		if e = json.Unmarshal(b, &m); e != nil {
			return e
		}
		if e = validateManaged(dir, m); e != nil {
			return e
		}
		if !standalone && !skillOnly && action == "update" && !check && m.Binary != nil {
			for _, arg := range initArgs {
				if arg != "--json" {
					return usage("unknown update option")
				}
			}
			return standaloneDirectUpdate(m, dir, life, out)
		}
		if !standalone && m.Binary != nil && !check {
			return errors.New("standalone installation exists; cannot overwrite with npm")
		}
	} else {
		if _, e = os.Lstat(dir); e == nil {
			return errors.New("unmanaged skill directory exists; preserved")
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	if !standalone && !skillOnly && action == "update" {
		if _, e = io.WriteString(out, "Use npx @ghship/cli@latest update to manage npm CLI and skill together.\n"); e != nil {
			return e
		}
		return cliError{4, errors.New("external npm lifecycle entry required")}
	}
	if check {
		return emitResult(out, true, map[string]any{"managed": managed, "standalone": managed && m.Binary != nil, "version": m.Version, "targetVersion": version})
	}
	if action == "uninstall" && !managed {
		return emitResult(out, true, map[string]any{"action": "uninstall", "alreadyAbsent": true})
	}
	actual := action
	if action == "install" && managed {
		actual = "update"
	}
	jsonResult := false
	for _, a := range life {
		if a == "--json" {
			jsonResult = true
		}
	}
	if action == "install" && !skillOnly && !userOnly && jsonResult {
		var installed, initialized bytes.Buffer
		if e = lifecycle(actual, life, &installed); e != nil {
			return e
		}
		initErr := initialize(initArgs, in, &initialized)
		result := map[string]any{}
		if json.Unmarshal(initialized.Bytes(), &result) != nil {
			result = map[string]any{"ready": false, "missing": []string{}, "next": []string{"ghship init --json"}}
			if initErr != nil {
				result["missing"] = []string{initErr.Error()}
			}
		}
		result["userInstalled"] = true
		var installation any
		json.Unmarshal(installed.Bytes(), &installation)
		result["installation"] = installation
		if e = emitResult(out, true, result); e != nil {
			return e
		}
		return initErr
	}
	if e = lifecycle(actual, life, out); e != nil {
		return e
	}
	if action != "install" || skillOnly || userOnly {
		return nil
	}
	return initialize(initArgs, in, out)
}

// Validate all arguments before reading or changing installation state.
func validateLifecycleArguments(action string, initArgs []string, skillOnly, userOnly, standalone, check bool, life []string) error {
	if skillOnly && standalone {
		return usage("--skill-only cannot be combined with --standalone")
	}
	for i := 0; i < len(life); i++ {
		if life[i] == "--bin-dir" && !standalone {
			return usage("--bin-dir requires --standalone")
		}
		if life[i] == "--dir" || life[i] == "--bin-dir" {
			i++
		}
	}
	f := flag.NewFlagSet("init", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	for _, name := range []string{"project", "repo", "author", "reviewer", "admin"} {
		f.String(name, "", "")
	}
	f.Int("acceptance-pr", 0, "")
	for _, name := range []string{"configure", "non-interactive", "confirm-auto-close", "remove-binding", "json"} {
		f.Bool(name, false, "")
	}
	if err := f.Parse(initArgs); err != nil || f.NArg() != 0 {
		return usage("invalid lifecycle arguments; use ghship help install")
	}
	invalid := false
	f.Visit(func(value *flag.Flag) {
		if value.Name != "json" && (action != "install" || skillOnly || userOnly || check) {
			invalid = true
		}
	})
	if invalid {
		return usage("init options require install without --user-only, --skill-only or --check")
	}
	return nil
}
