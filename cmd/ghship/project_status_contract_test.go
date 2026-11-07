package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProjectStatusDistinguishesMissingAndCorruptRecords(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	query := func() (map[string]any, error) {
		var out bytes.Buffer
		err := project([]string{"status", "--project", root, "--json"}, nil, &out)
		var value map[string]any
		if err == nil {
			if e := json.Unmarshal(out.Bytes(), &value); e != nil {
				t.Fatal(e)
			}
		} else if out.Len() != 0 {
			t.Fatalf("error emitted misleading status %q", out.String())
		}
		return value, err
	}
	for i := 0; i < 2; i++ {
		value, err := query()
		if err != nil || value["installed"] != false || value["bound"] != false {
			t.Fatalf("missing: %v %v", value, err)
		}
	}
	manifest := filepath.Join(root, ".ghship", "install.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0755); err != nil {
		t.Fatal(err)
	}
	bad := []byte("{invalid")
	if err := os.WriteFile(manifest, bad, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := query(); err == nil {
			t.Error("corrupt manifest reported as missing")
		}
		got, err := os.ReadFile(manifest)
		if err != nil || !bytes.Equal(got, bad) {
			t.Fatal("manifest changed", err)
		}
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	binding, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(binding), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(binding, bad, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = query(); err == nil {
			t.Error("corrupt binding reported as missing")
		}
		got, e := os.ReadFile(binding)
		if e != nil || !bytes.Equal(got, bad) {
			t.Fatal("binding changed", e)
		}
	}
}

func TestProjectStatusRejectsSymlinkedRecords(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege varies")
	}
	_, root := fixture(t)
	setupAuthHome(t)
	manifest := filepath.Join(root, ".ghship", "install.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(filepath.Dir(root), "sentinel")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, manifest); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
		t.Fatalf("symlink reported as state: %s %v", out.String(), err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "keep" {
		t.Fatal("target changed", err)
	}
	if err = os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	binding, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(binding), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, binding); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
		t.Fatalf("symlinked binding reported as state: %s %v", out.String(), err)
	}
	got, err = os.ReadFile(target)
	if err != nil || string(got) != "keep" {
		t.Fatal("binding target changed", err)
	}
}

func TestProjectStatusReportsManagedFileChangesAndUnknowns(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	if err := lifecycle("install", nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := installProjectFiles(root, false); err != nil {
		t.Fatal(err)
	}
	binding := projectBinding{SchemaVersion: 1, Root: root, Repo: "github.com/fixture/repo", Author: "author", Reviewer: "reviewer", Admin: "admin"}
	data, _ := json.Marshal(binding)
	name, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = atomicConfig(name, data); err != nil {
		t.Fatal(err)
	}
	query := func() (map[string]any, error) {
		var out bytes.Buffer
		err := project([]string{"status", "--project", root, "--json"}, nil, &out)
		var value map[string]any
		if err == nil {
			if e := json.Unmarshal(out.Bytes(), &value); e != nil {
				t.Fatal(e)
			}
		} else if out.Len() != 0 {
			t.Fatalf("unknown state emitted output %q", out.String())
		}
		return value, err
	}
	value, err := query()
	if err != nil || value["installed"] != true || value["bound"] != true || value["managedFilesUnmodified"] != true {
		t.Fatalf("valid: %v %v", value, err)
	}
	readme := filepath.Join(root, ".ghship", "README.md")
	before, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(readme, []byte("user changed"), 0644); err != nil {
		t.Fatal(err)
	}
	value, err = query()
	if err != nil || value["installed"] != true || value["bound"] != true || value["managedFilesUnmodified"] != false {
		t.Fatalf("modified: %v %v", value, err)
	}
	if err = os.Remove(readme); err != nil {
		t.Fatal(err)
	}
	value, err = query()
	if err != nil || value["managedFilesUnmodified"] != false {
		t.Fatalf("missing managed file: %v %v", value, err)
	}
	if runtime.GOOS != "windows" {
		target := filepath.Join(filepath.Dir(root), "sentinel")
		if err = os.WriteFile(target, before, 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(target, readme); err != nil {
			t.Fatal(err)
		}
		if _, err = query(); err == nil {
			t.Fatal("symlinked owned file reported known status")
		}
		got, e := os.ReadFile(target)
		if e != nil || !bytes.Equal(got, before) {
			t.Fatal("symlink target changed", e)
		}
	}
}

func TestProjectStatusRejectsUnreadableManagedFile(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("POSIX permission test needs an unprivileged user")
	}
	_, root := fixture(t)
	setupAuthHome(t)
	if err := lifecycle("install", nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := installProjectFiles(root, false); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(root, ".ghship", "README.md")
	if err := os.Chmod(readme, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readme, 0644) })
	var out bytes.Buffer
	if err := project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
		t.Fatalf("unreadable file reported known status: %q %v", out.String(), err)
	}
}

func TestProjectStatusRejectsUnreadableRecords(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("POSIX permission test needs an unprivileged user")
	}
	for _, kind := range []string{"manifest", "binding"} {
		t.Run(kind, func(t *testing.T) {
			_, root := fixture(t)
			setupAuthHome(t)
			record := filepath.Join(root, ".ghship", "install.json")
			if kind == "binding" {
				var err error
				record, err = bindingPath(root)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(filepath.Dir(record), 0700); err != nil {
				t.Fatal(err)
			}
			before := []byte(`{"synthetic":true}`)
			if err := os.WriteFile(record, before, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(record, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(record, 0600) })
			var out bytes.Buffer
			if err := project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
				t.Fatalf("unreadable %s reported status: %q %v", kind, out.String(), err)
			}
			if err := os.Chmod(record, 0600); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(record)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("%s changed: %v", kind, err)
			}
		})
	}
}

func TestProjectStatusRejectsSemanticallyCorruptRecords(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	manifest := filepath.Join(root, ".ghship", "install.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0755); err != nil {
		t.Fatal(err)
	}
	for _, record := range []projectManifest{
		{SchemaVersion: 1, Version: "1.0.0", ReadmeHash: strings.Repeat("z", 64), BlockHash: strings.Repeat("z", 64)},
		{SchemaVersion: 1, ReadmeHash: strings.Repeat("a", 64), BlockHash: strings.Repeat("b", 64)},
		{SchemaVersion: 1, Version: "not-a-version", ReadmeHash: strings.Repeat("a", 64), BlockHash: strings.Repeat("b", 64)},
	} {
		before, _ := json.Marshal(record)
		if err := os.WriteFile(manifest, before, 0600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
			t.Errorf("invalid manifest reported valid: %q %v", out.String(), err)
		}
		after, err := os.ReadFile(manifest)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("manifest changed", err)
		}
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	binding, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(binding), 0700); err != nil {
		t.Fatal(err)
	}
	for _, record := range []projectBinding{
		{SchemaVersion: 1, Root: root, Repo: "a/b/c"},
		{SchemaVersion: 1, Root: root, Repo: "github.com/fixture/repo", Author: "bad@user", Reviewer: "reviewer", Admin: "admin"},
		{SchemaVersion: 1, Root: root, Repo: "github.com/fixture/repo", Author: "developer", Reviewer: "Developer", Admin: "admin"},
	} {
		before, _ := json.Marshal(record)
		if err = os.WriteFile(binding, before, 0600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err = project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
			t.Errorf("invalid binding reported valid: %q %v", out.String(), err)
		}
		after, e := os.ReadFile(binding)
		if e != nil || !bytes.Equal(before, after) {
			t.Fatal("binding changed", e)
		}
	}
}

func TestProjectStatusAcceptsHistoricalAndManagedUserRecords(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	manifest := filepath.Join(root, ".ghship", "install.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0755); err != nil {
		t.Fatal(err)
	}
	record := projectManifest{SchemaVersion: 1, Version: "1.0.0", ReadmeHash: strings.Repeat("a", 64), BlockHash: strings.Repeat("b", 64)}
	data, _ := json.Marshal(record)
	if err := os.WriteFile(manifest, data, 0600); err != nil {
		t.Fatal(err)
	}
	binding := projectBinding{SchemaVersion: 1, Root: root, Repo: "github.com/fixture/repo", Author: "mona-cat_octo", Reviewer: "reviewer_octo", Admin: "octo_admin"}
	data, _ = json.Marshal(binding)
	name, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = atomicConfig(name, data); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = project([]string{"status", "--project", root, "--json"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(out.Bytes(), &result); err != nil || result["installed"] != true || result["bound"] != true || result["managedFilesUnmodified"] != false {
		t.Fatalf("historical/managed record: %s %v", out.String(), err)
	}
}

func TestProjectStatusRejectsDuplicateRecordFields(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	manifest := filepath.Join(root, ".ghship", "install.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(fmt.Sprintf(`{"schemaVersion":1,"version":"1.0.0","version":"1.0.0","readmeHash":%q,"blockHash":%q,"createdAgents":false}`, strings.Repeat("a", 64), strings.Repeat("b", 64)))
	if err := os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
		t.Errorf("duplicate manifest field accepted: %q %v", out.String(), err)
	}
	after, err := os.ReadFile(manifest)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("manifest changed", err)
	}
	if err = os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	binding, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(binding), 0700); err != nil {
		t.Fatal(err)
	}
	raw = []byte(fmt.Sprintf(`{"schemaVersion":1,"root":%q,"repo":"github.com/fixture/repo","author":"author","author":"author","reviewer":"reviewer","admin":"admin"}`, root))
	if err = os.WriteFile(binding, raw, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
		t.Errorf("duplicate binding field accepted: %q %v", out.String(), err)
	}
	after, err = os.ReadFile(binding)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("binding changed", err)
	}
}

func TestProjectStatusRejectsMissingRequiredRecordFields(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	manifest := filepath.Join(root, ".ghship", "install.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(fmt.Sprintf(`{"schemaVersion":1,"version":"1.0.0","readmeHash":%q,"blockHash":%q}`, strings.Repeat("a", 64), strings.Repeat("b", 64)))
	if err := os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
		t.Errorf("missing manifest field accepted: %q %v", out.String(), err)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	binding, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(binding), 0700); err != nil {
		t.Fatal(err)
	}
	raw = []byte(fmt.Sprintf(`{"schemaVersion":1,"root":%q,"repo":"github.com/fixture/repo","author":"author","reviewer":"reviewer","admin":"admin"}`, root))
	if err = os.WriteFile(binding, raw, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
		t.Errorf("missing binding field accepted: %q %v", out.String(), err)
	}
	after, err := os.ReadFile(binding)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("binding changed", err)
	}
}

func TestProjectStatusRejectsNullRequiredBooleans(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	manifest := filepath.Join(root, ".ghship", "install.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0755); err != nil {
		t.Fatal(err)
	}
	raw := []byte(fmt.Sprintf(`{"schemaVersion":1,"version":"1.0.0","readmeHash":%q,"blockHash":%q,"createdAgents":null}`, strings.Repeat("a", 64), strings.Repeat("b", 64)))
	if err := os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
		t.Errorf("null manifest field accepted: %q %v", out.String(), err)
	}
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	binding, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(binding), 0700); err != nil {
		t.Fatal(err)
	}
	raw = []byte(fmt.Sprintf(`{"schemaVersion":1,"root":%q,"repo":"github.com/fixture/repo","author":"author","reviewer":"reviewer","admin":"admin","autoCloseConfirmed":null}`, root))
	if err = os.WriteFile(binding, raw, 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = project([]string{"status", "--project", root, "--json"}, nil, &out); err == nil || out.Len() != 0 {
		t.Errorf("null binding field accepted: %q %v", out.String(), err)
	}
}
