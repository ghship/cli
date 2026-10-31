package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func installDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if p, e := filepath.EvalSymlinks(d); e == nil {
		d = p
	}
	return filepath.Join(d, "skill")
}
func TestSkillLifecycle(t *testing.T) {
	d := installDir(t)
	var b bytes.Buffer
	for _, action := range []string{"install", "update", "uninstall"} {
		if e := lifecycle(action, []string{"--dir", d, "--json"}, &b); e != nil {
			t.Fatal(action, e)
		}
	}
	if _, e := os.Stat(d); !os.IsNotExist(e) {
		t.Fatal("skill remains")
	}
}
func TestManagedPreservation(t *testing.T) {
	for _, kind := range []string{"modified", "custom", "empty-directory", "missing", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			d := installDir(t)
			if e := lifecycle("install", []string{"--dir", d}, &bytes.Buffer{}); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "modified":
				os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("custom"), 0o644)
			case "custom":
				os.WriteFile(filepath.Join(d, "mine"), []byte("custom"), 0o644)
			case "empty-directory":
				os.Mkdir(filepath.Join(d, "mine"), 0o755)
			case "missing":
				os.Remove(filepath.Join(d, "SKILL.md"))
			case "symlink":
				if runtime.GOOS == "windows" {
					t.Skip("Windows symlink privilege varies")
				}
				os.Symlink("SKILL.md", filepath.Join(d, "mine"))
			}
			for _, action := range []string{"update", "uninstall"} {
				if e := lifecycle(action, []string{"--dir", d}, &bytes.Buffer{}); e == nil {
					t.Fatal("modified installation destroyed")
				}
			}
			if _, e := os.Stat(d); e != nil {
				t.Fatal("installation lost")
			}
		})
	}
}
func TestStandaloneLifecycle(t *testing.T) {
	d := installDir(t)
	bin := filepath.Join(filepath.Dir(d), "custom-bin")
	var b bytes.Buffer
	if e := lifecycle("install", []string{"--dir", d, "--bin-dir", bin, "--standalone", "--json"}, &b); e != nil {
		t.Fatal(e)
	}
	if e := lifecycle("update", []string{"--dir", d}, &b); e == nil {
		t.Fatal("npm channel overwrote standalone")
	}
	for _, action := range []string{"update", "uninstall"} {
		if e := lifecycle(action, []string{"--dir", d, "--standalone", "--json"}, &b); e != nil {
			t.Fatal(action, e)
		}
	}
	name := "ghship"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if _, e := os.Stat(filepath.Join(bin, name)); !os.IsNotExist(e) {
		t.Fatal("binary remains")
	}
}
func TestReplacementRollback(t *testing.T) {
	d := installDir(t)
	os.MkdirAll(d, 0o755)
	one := filepath.Join(d, "one")
	os.WriteFile(one, []byte("old"), 0o600)
	stage := filepath.Join(d, "stage")
	os.WriteFile(stage, []byte("new"), 0o600)
	e := commitReplacements([]replacement{{dest: one, stage: stage}, {dest: filepath.Join(d, "two"), stage: filepath.Join(d, "missing")}})
	if e == nil {
		t.Fatal("expected failure")
	}
	b, _ := os.ReadFile(one)
	if string(b) != "old" {
		t.Fatal("rollback lost old data")
	}
}
func TestVersionGuard(t *testing.T) {
	for _, pair := range [][2]string{{"1.0.0", "1.0.0"}, {"1.1.0", "1.0.9"}} {
		if !newerOrEqual(pair[0], pair[1]) {
			t.Fatal(pair)
		}
	}
	if newerOrEqual("1.0.0", "2.0.0") || newerOrEqual("1.0.0", "bad") {
		t.Fatal("downgrade accepted")
	}
}

func TestVersionUpdate(t *testing.T) {
	saved := version
	defer func() { version = saved }()
	version = "1.0.0"
	d := installDir(t)
	var b bytes.Buffer
	if e := lifecycle("install", []string{"--dir", d}, &b); e != nil {
		t.Fatal(e)
	}
	version = "1.0.1"
	if e := lifecycle("update", []string{"--dir", d}, &b); e != nil {
		t.Fatal(e)
	}
	version = "1.0.0"
	if e := lifecycle("update", []string{"--dir", d}, &b); e == nil {
		t.Fatal("downgrade allowed")
	}
	if e := lifecycle("uninstall", []string{"--dir", d}, &b); e != nil {
		t.Fatal(e)
	}
}

func TestUnexpectedReplacementPreserved(t *testing.T) {
	d := installDir(t)
	os.MkdirAll(d, 0o755)
	file := filepath.Join(d, "user-file")
	os.WriteFile(file, []byte("user"), 0o600)
	stage := filepath.Join(d, "stage")
	os.WriteFile(stage, []byte("new"), 0o600)
	expected := false
	if e := commitReplacements([]replacement{{dest: file, stage: stage, expected: &expected}}); e == nil {
		t.Fatal("unexpected file overwritten")
	}
	b, _ := os.ReadFile(file)
	if string(b) != "user" {
		t.Fatal("user file lost")
	}
}

func TestStandaloneInitializationSingleJSON(t *testing.T) {
	home := setupAuthHome(t)
	cwd, _ := os.Getwd()
	os.Chdir(home)
	t.Cleanup(func() { os.Chdir(cwd) })
	var out bytes.Buffer
	e := lifecycleEntry("install", []string{"--standalone", "--non-interactive", "--json"}, nil, &out)
	if e == nil {
		t.Fatal("non-project reported initialized")
	}
	var result map[string]any
	if e := json.Unmarshal(out.Bytes(), &result); e != nil {
		t.Fatal("multiple/invalid JSON", e, out.String())
	}
	if result["userInstalled"] != true || result["ready"] != false {
		t.Fatal(result)
	}
}
