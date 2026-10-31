package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	assets "github.com/ghship/cli"
)

const installHelp = `ghship install|update|uninstall [--dir PATH] [--standalone] [--bin-dir PATH]
  [--user-only] [--json]
Npm: npx @ghship/cli@VERSION install|update|uninstall coordinates the exact-version
CLI package and home skill. install enters init unless --user-only.
install accepts the init options shown by ghship help init and forwards them.
Registry commands require the chosen version to be published; downloaded binaries
use --standalone, downloaded npm tarballs use the package's local npm exec entry.
Default skill: ~/.agents/skills/ghship; repeat --dir for custom installations.
Direct download: --standalone also manages the executable; default Unix bin
~/.local/bin, Windows %LOCALAPPDATA%/Programs/ghship. No sudo or PATH edits.
Npm and standalone installations cannot overwrite one another.
Updates/uninstalls require managed unchanged files, preserve users/projects,
refuse symlinks, unknown content and downgrades; stage and recover on failure.
Windows standalone update/uninstall: run from a separate downloaded executable.
Npm update/uninstall use an external npx entry; installed Go CLI returns 4.
Install is idempotent for managed installations; unknown destinations preserved.
Coordinator flags --skill-only and --check are for bundled-skill operations:
--check validates ownership without writing; neither implies project readiness.
Examples:
 npx @ghship/cli@latest install --user-only
 npx @ghship/cli@latest update
 npx @ghship/cli@latest uninstall
 ./download/ghship install --standalone --user-only
 ./new-download/ghship update --standalone
 ./download/ghship uninstall --standalone
JSON: owned skill/binary results or initialization completed/missing/next.
Exit 0 requested scope complete; 1 failure; 2 usage; 4 initialization/entry pending.
`

type binaryRecord struct {
	Path     string `json:"path"`
	Hash     string `json:"sha256"`
	Platform string `json:"platform"`
}
type manifest struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
	Binary  *binaryRecord     `json:"binary,omitempty"`
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func noLinks(path string) error {
	for p := path; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if e == nil && st.Mode()&os.ModeSymlink != 0 {
			return errors.New("path contains symlink; preserved")
		}
		if filepath.Dir(p) == p {
			return nil
		}
	}
}
func validateManaged(dir string, m manifest) error {
	if len(m.Files) == 0 {
		return errors.New("invalid installation manifest")
	}
	dirs := map[string]bool{".": true}
	for name, hash := range m.Files {
		if !fs.ValidPath(name) || name == ".ghship.json" || len(hash) != 64 {
			return errors.New("unsafe manifest")
		}
		for p := filepath.Dir(filepath.FromSlash(name)); p != "."; p = filepath.Dir(p) {
			dirs[filepath.ToSlash(p)] = true
		}
	}
	e := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink in installation; preserved")
		}
		if d.IsDir() {
			if !dirs[rel] {
				return fmt.Errorf("custom directory %s; preserved", rel)
			}
			return nil
		}
		if rel == ".ghship.json" {
			return nil
		}
		hash, ok := m.Files[rel]
		if !ok {
			return fmt.Errorf("custom file %s; preserved", rel)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		if digest(b) != hash {
			return fmt.Errorf("modified file %s; preserved", rel)
		}
		return nil
	})
	if e != nil {
		return e
	}
	for name, hash := range m.Files {
		b, e := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if e != nil || digest(b) != hash {
			return errors.New("managed file missing or modified; preserved")
		}
	}
	if m.Binary != nil {
		if !filepath.IsAbs(m.Binary.Path) || m.Binary.Platform != runtime.GOOS+"/"+runtime.GOARCH {
			return errors.New("invalid standalone platform or path")
		}
		if e = noLinks(m.Binary.Path); e != nil {
			return e
		}
		b, e := os.ReadFile(m.Binary.Path)
		if e != nil || digest(b) != m.Binary.Hash {
			return errors.New("installed executable missing or modified; preserved")
		}
	}
	return nil
}
func newerOrEqual(new, old string) bool {
	split := func(v string) []int {
		p := strings.Split(v, ".")
		if len(p) != 3 {
			return nil
		}
		n := []int{}
		for _, s := range p {
			x, e := strconv.Atoi(s)
			if e != nil || x < 0 {
				return nil
			}
			n = append(n, x)
		}
		return n
	}
	a, b := split(new), split(old)
	if a == nil || b == nil {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return true
}

type replacement struct {
	dest, stage, backup string
	expected            *bool
}

func commitReplacements(items []replacement) error {
	rollback := func(end int) error {
		var errs []error
		for i := end; i >= 0; i-- {
			x := items[i]
			if x.stage != "" {
				if e := os.RemoveAll(x.dest); e != nil && !os.IsNotExist(e) {
					errs = append(errs, e)
				}
			}
			if x.backup != "" {
				if e := os.Rename(x.backup, x.dest); e != nil {
					errs = append(errs, fmt.Errorf("recover at %s: %w", x.backup, e))
				}
			}
		}
		return errors.Join(errs...)
	}
	for i := range items {
		x := &items[i]
		if _, e := os.Lstat(x.dest); e == nil {
			if x.expected != nil && !*x.expected {
				return errors.Join(errors.New("destination appeared during staging; preserved"), rollback(i-1))
			}
			backup, e := os.MkdirTemp(filepath.Dir(x.dest), ".ghship-backup-")
			if e != nil {
				return errors.Join(e, rollback(i-1))
			}
			if e = os.Remove(backup); e != nil {
				return errors.Join(e, rollback(i-1))
			}
			if e = os.Rename(x.dest, backup); e != nil {
				return errors.Join(e, rollback(i-1))
			}
			x.backup = backup
		} else if !os.IsNotExist(e) {
			return errors.Join(e, rollback(i-1))
		} else if x.expected != nil && *x.expected {
			return errors.Join(errors.New("installation disappeared during staging"), rollback(i-1))
		}
		if x.stage != "" {
			if e := os.Rename(x.stage, x.dest); e != nil {
				return errors.Join(e, rollback(i))
			}
		}
	}
	for _, x := range items {
		if x.backup != "" {
			if e := os.RemoveAll(x.backup); e != nil {
				return fmt.Errorf("operation completed; old data retained at %s: %w", x.backup, e)
			}
		}
	}
	return nil
}
func lifecycle(action string, a []string, out io.Writer) error {
	home, e := os.UserHomeDir()
	if e != nil {
		return e
	}
	dir := filepath.Join(home, ".agents", "skills", "ghship")
	binDir := ""
	standalone, asJSON := false, false
	for i := 0; i < len(a); i++ {
		switch a[i] {
		case "--dir", "--bin-dir":
			key := a[i]
			i++
			if i == len(a) || a[i] == "" {
				return usage(key + " requires path")
			}
			if key == "--dir" {
				dir = a[i]
			} else {
				binDir = a[i]
			}
		case "--standalone":
			standalone = true
		case "--json":
			asJSON = true
		default:
			return usage("unknown lifecycle option")
		}
	}
	if binDir != "" && !standalone {
		return usage("--bin-dir requires --standalone")
	}
	dir, e = filepath.Abs(dir)
	if e != nil {
		return e
	}
	if e = noLinks(dir); e != nil {
		return e
	}
	exists := false
	if _, e = os.Lstat(dir); e == nil {
		exists = true
	} else if !os.IsNotExist(e) {
		return e
	}
	if action == "install" && exists {
		return errors.New("destination exists; use update for managed installation")
	}
	if action != "install" && !exists {
		return errors.New("no managed installation")
	}
	var old manifest
	if exists {
		b, e := os.ReadFile(filepath.Join(dir, ".ghship.json"))
		if e != nil {
			return errors.New("unmanaged installation; preserved")
		}
		if e = json.Unmarshal(b, &old); e != nil {
			return errors.New("invalid manifest")
		}
		if e = validateManaged(dir, old); e != nil {
			return e
		}
		if standalone != (old.Binary != nil) {
			return errors.New("installation channel differs; uninstall with the original channel first")
		}
		if action == "update" && !newerOrEqual(version, old.Version) {
			return errors.New("downgrade refused")
		}
	}
	var binaryPath string
	if standalone {
		if exists {
			binaryPath = old.Binary.Path
			if binDir != "" {
				p, e := filepath.Abs(binDir)
				if e != nil {
					return e
				}
				if p != filepath.Dir(binaryPath) {
					return errors.New("bin directory differs from manifest")
				}
			}
		} else {
			if binDir == "" {
				binDir = filepath.Join(home, ".local", "bin")
				if runtime.GOOS == "windows" {
					binDir = filepath.Join(envDefault("LOCALAPPDATA", filepath.Join(home, ".local")), "Programs", "ghship")
				}
			}
			binDir, e = filepath.Abs(binDir)
			if e != nil {
				return e
			}
			name := "ghship"
			if runtime.GOOS == "windows" {
				name += ".exe"
			}
			binaryPath = filepath.Join(binDir, name)
			if _, err := os.Lstat(binaryPath); err == nil {
				return errors.New("binary destination already exists; preserved")
			} else if !os.IsNotExist(err) {
				return err
			}
		}
		if e = noLinks(binaryPath); e != nil {
			return e
		}
		rel, _ := filepath.Rel(dir, binaryPath)
		if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return errors.New("binary must be outside skill directory")
		}
		exe, e := os.Executable()
		if e != nil {
			return e
		}
		exe, e = filepath.EvalSymlinks(exe)
		if e != nil {
			return e
		}
		if runtime.GOOS == "windows" && strings.EqualFold(exe, binaryPath) {
			return errors.New("run update/uninstall from a separate downloaded executable on Windows")
		}
	}
	items := []replacement{{dest: dir, expected: &exists}}
	if standalone {
		items = append(items, replacement{dest: binaryPath, expected: &exists})
	}
	if action != "uninstall" {
		parent := filepath.Dir(dir)
		if e = os.MkdirAll(parent, 0o755); e != nil {
			return e
		}
		stage, e := os.MkdirTemp(parent, ".ghship-stage-")
		if e != nil {
			return e
		}
		defer os.RemoveAll(stage)
		items[0].stage = stage
		current := manifest{Version: version, Files: map[string]string{}}
		e = fs.WalkDir(assets.Skill, ".", func(name string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			b, e := assets.Skill.ReadFile(name)
			if e != nil {
				return e
			}
			p := filepath.Join(stage, filepath.FromSlash(name))
			if e = os.MkdirAll(filepath.Dir(p), 0o755); e != nil {
				return e
			}
			if e = os.WriteFile(p, b, 0o644); e != nil {
				return e
			}
			current.Files[name] = digest(b)
			return nil
		})
		if e != nil {
			return e
		}
		if standalone {
			if e = os.MkdirAll(filepath.Dir(binaryPath), 0o755); e != nil {
				return e
			}
			exe, e := os.Executable()
			if e != nil {
				return e
			}
			b, e := os.ReadFile(exe)
			if e != nil {
				return e
			}
			f, e := os.CreateTemp(filepath.Dir(binaryPath), ".ghship-stage-")
			if e != nil {
				return e
			}
			path := f.Name()
			defer os.Remove(path)
			if _, e = f.Write(b); e != nil {
				f.Close()
				return e
			}
			if e = f.Close(); e != nil {
				return e
			}
			if e = os.Chmod(path, 0o755); e != nil {
				return e
			}
			items[1].stage = path
			current.Binary = &binaryRecord{Path: binaryPath, Hash: digest(b), Platform: runtime.GOOS + "/" + runtime.GOARCH}
		}
		b, e := json.MarshalIndent(current, "", "  ")
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(stage, ".ghship.json"), b, 0o644); e != nil {
			return e
		}
	}
	// Revalidate owned files after staging, before any replacement.
	if exists {
		if e = validateManaged(dir, old); e != nil {
			return e
		}
	}
	if e = commitReplacements(items); e != nil {
		return e
	}
	result := map[string]string{"action": action, "directory": dir, "version": version}
	if standalone {
		result["binary"] = binaryPath
		result["pathHint"] = filepath.Dir(binaryPath)
	}
	if asJSON {
		return json.NewEncoder(out).Encode(result)
	}
	fmt.Fprintf(out, "%s: %s (%s)\n", action, dir, version)
	if standalone {
		fmt.Fprintf(out, "CLI: %s; add %s to PATH if needed\n", binaryPath, filepath.Dir(binaryPath))
	}
	return nil
}
