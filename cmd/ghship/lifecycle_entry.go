package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
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
			if i+1 == len(args) {
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
			initArgs = append(initArgs, s)
		}
	}
	if !standalone && !skillOnly && (action == "update" || action == "uninstall") {
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
		if !standalone && m.Binary != nil {
			return errors.New("standalone installation exists; cannot overwrite with npm")
		}
	} else {
		if _, e = os.Lstat(dir); e == nil {
			return errors.New("unmanaged skill directory exists; preserved")
		} else if !os.IsNotExist(e) {
			return e
		}
	}
	if check {
		return emitResult(out, true, map[string]any{"managed": managed, "version": m.Version, "targetVersion": version})
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
