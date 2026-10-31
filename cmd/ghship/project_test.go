package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectUpdateRequiresValidInstalledSkill(t *testing.T) {
	for _, scenario := range []string{"missing", "invalid manifest", "modified skill", "missing reference", "omitted manifest entry", "wrong version"} {
		t.Run(scenario, func(t *testing.T) {
			_, root := fixture(t)
			home := setupAuthHome(t)
			if err := lifecycle("install", nil, &bytes.Buffer{}); err != nil {
				t.Fatal(err)
			}
			if err := installProjectFiles(root, false); err != nil {
				t.Fatal(err)
			}
			manifestPath := filepath.Join(root, ".ghship", "install.json")
			m, err := loadProjectManifest(root)
			if err != nil {
				t.Fatal(err)
			}
			m.Version = "0.9.0"
			b, _ := json.Marshal(m)
			if err = os.WriteFile(manifestPath, b, 0644); err != nil {
				t.Fatal(err)
			}
			paths := []string{filepath.Join(root, "AGENTS.md"), filepath.Join(root, ".ghship", "README.md"), manifestPath}
			before := make([][]byte, len(paths))
			for i, p := range paths {
				before[i], err = os.ReadFile(p)
				if err != nil {
					t.Fatal(err)
				}
			}
			dir := filepath.Join(home, ".agents", "skills", "ghship")
			switch scenario {
			case "missing":
				err = os.RemoveAll(dir)
			case "invalid manifest":
				err = os.WriteFile(filepath.Join(dir, ".ghship.json"), []byte("invalid"), 0644)
			case "modified skill":
				err = os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("modified"), 0644)
			case "missing reference":
				err = os.Remove(filepath.Join(dir, "references", "cli.md"))
			case "omitted manifest entry", "wrong version":
				mp := filepath.Join(dir, ".ghship.json")
				data, e := os.ReadFile(mp)
				if e != nil {
					t.Fatal(e)
				}
				var skill manifest
				if e = json.Unmarshal(data, &skill); e != nil {
					t.Fatal(e)
				}
				if scenario == "omitted manifest entry" {
					delete(skill.Files, "references/cli.md")
					if e = os.Remove(filepath.Join(dir, "references", "cli.md")); e != nil {
						t.Fatal(e)
					}
				} else {
					skill.Version = "9.0.0"
				}
				data, e = json.Marshal(skill)
				if e != nil {
					t.Fatal(e)
				}
				err = os.WriteFile(mp, data, 0644)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = project([]string{"update", "--project", root}, nil, &bytes.Buffer{}); err == nil {
				t.Fatal("updated project without valid installed skill")
			}
			for i, p := range paths {
				after, e := os.ReadFile(p)
				if e != nil || !bytes.Equal(before[i], after) {
					t.Fatalf("project changed: %s %v", p, e)
				}
			}
		})
	}
}

func TestProjectUninstallPreservesUnknownManifestAndBinding(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	if err := lifecycle("install", nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := installProjectFiles(root, false); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, ".ghship", "install.json")
	m, err := loadProjectManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	m.SchemaVersion = 99
	b, _ := json.Marshal(m)
	if err = os.WriteFile(p, b, 0644); err != nil {
		t.Fatal(err)
	}
	binding, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(binding), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(binding, []byte("preserve binding"), 0600); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(root, "AGENTS.md"), filepath.Join(root, ".ghship", "README.md"), p, binding}
	before := make([][]byte, len(paths))
	for i, path := range paths {
		before[i], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"uninstall", "--project", root}, {"uninstall", "--project", root, "--binding-only"}} {
		if err = project(args, nil, &bytes.Buffer{}); err == nil {
			t.Fatalf("uninstalled unknown project or binding: %v", args)
		}
	}
	if err = initialize([]string{"--project", root, "--remove-binding"}, nil, &bytes.Buffer{}); err == nil {
		t.Fatal("removed binding with unknown manifest")
	}
	for i, path := range paths {
		after, e := os.ReadFile(path)
		if e != nil || !bytes.Equal(before[i], after) {
			t.Fatalf("unknown data changed: %s %v", path, e)
		}
	}
}

func TestProjectLifecyclePreservesOtherContentAndCustomization(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	if err := lifecycle("install", nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	original := "# Existing project rules\nKeep this instruction.\n"
	if e := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(original), 0644); e != nil {
		t.Fatal(e)
	}
	if e := installProjectFiles(root, false); e != nil {
		t.Fatal(e)
	}
	if e := installProjectFiles(root, false); e != nil {
		t.Fatal("idempotent install", e)
	}
	var out bytes.Buffer
	if e := projectSkill([]string{"--project", root, "--create", "--mode", "override", "--overrides", "testing,acceptance", "--json"}, &out); e != nil {
		t.Fatal(e)
	}
	custom, _ := os.ReadFile(filepath.Join(root, ".ghship", "skill.md"))
	agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	agents = append(agents, []byte("\nMore project rules.\n")...)
	os.WriteFile(filepath.Join(root, "AGENTS.md"), agents, 0644)
	if e := project([]string{"update", "--project", root, "--json"}, nil, &out); e != nil {
		t.Fatal(e)
	}
	if e := project([]string{"uninstall", "--project", root, "--json"}, nil, &out); e != nil {
		t.Fatal(e)
	}
	remaining, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if !strings.Contains(string(remaining), original) || !strings.Contains(string(remaining), "More project rules") || strings.Contains(string(remaining), agentStart) {
		t.Fatal(string(remaining))
	}
	after, _ := os.ReadFile(filepath.Join(root, ".ghship", "skill.md"))
	if !bytes.Equal(custom, after) {
		t.Fatal("customization changed")
	}
	if _, e := os.Stat(filepath.Join(root, ".ghship", "README.md")); !os.IsNotExist(e) {
		t.Fatal("owned file remains")
	}
	if e := installProjectFiles(root, false); e != nil {
		t.Fatal("reinstall", e)
	}
}
func TestProjectUninstallRefusesEditedOwnership(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	if err := lifecycle("install", nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if e := installProjectFiles(root, false); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(root, "AGENTS.md")
	b, _ := os.ReadFile(p)
	b = []byte(strings.Replace(string(b), "Never invent results", "Do not alter this custom sentence", 1))
	os.WriteFile(p, b, 0644)
	var out bytes.Buffer
	if e := project([]string{"uninstall", "--project", root}, nil, &out); e == nil {
		t.Fatal("edited fragment deleted")
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(b, after) {
		t.Fatal("edited file lost")
	}
	if _, e := os.Stat(filepath.Join(root, ".ghship", "README.md")); e != nil {
		t.Fatal("partial removal")
	}
}
func TestManagedBlockReadsReferencesAndPreservesHandEdits(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	if err := lifecycle("install", nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if e := installProjectFiles(root, false); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(root, "AGENTS.md")
	agents, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	block, start, end, e := extractBlock(string(agents))
	if e != nil || block == "" || start < 0 || end <= start {
		t.Fatal(e, block)
	}
	for _, want := range []string{"references/pipeline.md", "references/workflow.md", "references/cli.md", "project update"} {
		if !strings.Contains(block, want) {
			t.Errorf("managed block omits %q", want)
		}
	}
	edited := []byte(strings.Replace(string(agents), "references/pipeline.md", "references/renamed.md", 1))
	if e = os.WriteFile(p, edited, 0644); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e = project([]string{"update", "--project", root, "--json"}, nil, &out); e == nil {
		t.Fatal("update accepted an edited managed block")
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(edited, after) {
		t.Fatal("edited AGENTS.md was rewritten")
	}
}
func TestProjectSkillStrictScope(t *testing.T) {
	_, root := fixture(t)
	var out bytes.Buffer
	if e := projectSkill([]string{"--show", "--project", root, "--json"}, &out); e != nil || !strings.Contains(out.String(), "global_defaults") {
		t.Fatal(e, out.String())
	}
	if e := projectSkill([]string{"--create", "--project", root, "--mode", "override"}, &out); e == nil {
		t.Fatal("ambiguous override accepted")
	}
	if e := projectSkill([]string{"--create", "--project", root, "--mode", "override", "--overrides", "permissions"}, &out); e == nil {
		t.Fatal("unknown scope accepted")
	}
}
