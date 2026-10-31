package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if name == "gh" || name == "git" {
		helper(name)
		return
	}
	os.Exit(m.Run())
}
func helper(name string) {
	args := os.Args[1:]
	if name == "gh" && len(args) > 0 && args[0] == "api" {
		for _, arg := range args {
			if strings.HasPrefix(arg, "http://") {
				c := exec.Command(os.Getenv("REAL_GH"), args...)
				c.Stdout = os.Stdout
				c.Stderr = os.Stderr
				if e := c.Run(); e != nil {
					os.Exit(1)
				}
				return
			}
		}
	}
	if name == "git" {
		if strings.Join(args, " ") == "remote get-url origin" {
			fmt.Println(envDefault("TEST_ORIGIN", "https://github.com/fixture/repo.git"))
			return
		}
		c := exec.Command(os.Getenv("REAL_GIT"), args...)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if e := c.Run(); e != nil {
			var ee *exec.ExitError
			if errors.As(e, &ee) {
				os.Exit(ee.ExitCode())
			}
			os.Exit(1)
		}
		return
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("gh version 2.102.0")
		return
	}
	if len(args) >= 2 && args[0] == "auth" && args[1] == "token" {
		fmt.Println("admin")
		return
	}
	if len(args) >= 3 && args[0] == "issue" && args[1] == "create" && args[2] == "--help" {
		fmt.Println("--parent --blocked-by")
		return
	}
	if len(args) >= 2 && args[0] == "api" && args[1] == "user" {
		fmt.Println(envDefault("TEST_ACTOR", "author"))
		return
	}
	if len(args) >= 2 && args[0] == "pr" && args[1] == "view" {
		b, _ := os.ReadFile(os.Getenv("PR_DATA"))
		fmt.Print(string(b))
		return
	}
	if len(args) >= 2 && args[0] == "repo" && args[1] == "view" {
		fmt.Println(`{"url":"https://github.com/fixture/repo","defaultBranchRef":{"name":"master"}}`)
		return
	}
	b, _ := json.Marshal(map[string]any{"args": args, "token": os.Getenv("GH_TOKEN"), "otherToken": os.Getenv("GITHUB_TOKEN")})
	os.WriteFile(os.Getenv("CALL_DATA"), b, 0o600)
	if os.Getenv("TEST_GH_EXIT") == "7" {
		os.Exit(7)
	}
}
func fixture(t *testing.T) (string, string) {
	if gh, e := exec.LookPath("gh"); e == nil {
		t.Setenv("REAL_GH", gh)
	}
	t.Helper()
	r := t.TempDir()
	if p, e := filepath.EvalSymlinks(r); e == nil {
		r = p
	}
	repo := filepath.Join(r, "repo")
	os.Mkdir(repo, 0o755)
	cwd, _ := os.Getwd()
	if e := os.Chdir(repo); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { os.Chdir(cwd) })
	real, e := exec.LookPath("git")
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("REAL_GIT", real)
	bin := filepath.Join(r, "bin")
	os.Mkdir(bin, 0o755)
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(exe)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"gh", "git"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if e = os.WriteFile(filepath.Join(bin, name), data, 0o755); e != nil {
			t.Fatal(e)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CALL_DATA", filepath.Join(r, "call.json"))
	t.Setenv("PR_DATA", filepath.Join(r, "pr.json"))
	gitCmd(t, "init", "-b", "master")
	gitCmd(t, "config", "user.name", "Test")
	gitCmd(t, "config", "user.email", "test@example.com")
	os.WriteFile("seed", []byte("seed"), 0o644)
	gitCmd(t, "add", ".")
	gitCmd(t, "commit", "-m", "seed")
	return r, repo
}
func gitCmd(t *testing.T, a ...string) string {
	t.Helper()
	s, e := output(nil, "git", a...)
	if e != nil {
		t.Fatal(a, e)
	}
	return s
}
func TestQueue(t *testing.T) {
	parse := func(s string) []issue {
		var a []issue
		if e := json.Unmarshal([]byte(s), &a); e != nil {
			t.Fatal(e)
		}
		return a
	}
	item := func(n int, owner string, children, blockers int) string {
		a := "[]"
		if owner != "" {
			a = `[{"login":"` + owner + `"}]`
		}
		nodes := "[]"
		if blockers != 0 {
			nodes = `[{}]`
		}
		return fmt.Sprintf(`{"number":%d,"assignees":%s,"subIssuesSummary":{"total":%d},"blockedBy":{"totalCount":%d,"nodes":%s}}`, n, a, children, blockers, nodes)
	}
	a := parse("[" + strings.Join([]string{item(1, "", 2, 0), item(2, "other", 0, 0), item(3, "", 0, 1), item(5, "", 0, 0), item(4, "", 0, 0)}, ",") + "]")
	n, action, e := selectIssue(a, "author")
	if e != nil || n != 4 || action != "claim" {
		t.Fatal(n, action, e)
	}
	n, action, e = selectIssue(parse("["+item(7, "author", 0, 0)+"]"), "author")
	if e != nil || n != 7 || action != "resume" {
		t.Fatal(n, action, e)
	}
	for _, s := range []string{`[{"number":1}]`, "[" + item(1, "author", 0, 0) + "," + item(2, "author", 0, 0) + "]", `[{"number":1,"assignees":[],"subIssuesSummary":{"total":0},"blockedBy":{"totalCount":2,"nodes":[]}}]`} {
		if _, _, e = selectIssue(parse(s), "author"); e == nil {
			t.Fatal("accepted invalid", s)
		}
	}
	_, _, e = selectIssue(nil, "author")
	var ce cliError
	if !errors.As(e, &ce) || ce.code != 3 {
		t.Fatal(e)
	}
	if _, _, e = selectIssue(make([]issue, 1000), "author"); e == nil {
		t.Fatal("truncation accepted")
	}
}
func TestHelpAndJSON(t *testing.T) {
	for _, cmd := range []string{"version", "gh", "git", "doctor", "project", "user", "init", "next", "tidy", "install", "update", "uninstall"} {
		for _, a := range [][]string{{"help", cmd}, {cmd, "--help"}} {
			var b bytes.Buffer
			if e := run(a, strings.NewReader(""), &b); e != nil || b.Len() == 0 {
				t.Fatal(a, e)
			}
		}
	}
	var b bytes.Buffer
	e := run([]string{"next", "--author", "a", "--json"}, strings.NewReader(`[{"number":2,"assignees":[],"blockedBy":{"totalCount":0,"nodes":[]},"subIssuesSummary":{"total":0}}]`), &b)
	if e != nil || !strings.Contains(b.String(), `"issue":2`) {
		t.Fatal(b.String(), e)
	}
	for _, a := range [][]string{{"bad"}, {"next"}, {"tidy", "-1"}, {"install", "--bin-dir", "/tmp"}} {
		if e := run(a, strings.NewReader(""), &b); e == nil {
			t.Fatal(a)
		}
	}
}
func TestRoles(t *testing.T) {
	r, _ := fixture(t)
	t.Setenv("GHSHIP_AUTHOR", "author")
	t.Setenv("GHSHIP_DISPATCHER", "dispatcher")
	token := filepath.Join(r, "token")
	os.WriteFile(token, []byte("fixture-token"), 0o600)
	t.Setenv("GHSHIP_AUTHOR_PAT_FILE", token)
	var b bytes.Buffer
	if e := github([]string{"author", "--", "issue", "create", "--title", "literal $()"}, strings.NewReader(""), &b); e != nil {
		t.Fatal(e)
	}
	call, _ := os.ReadFile(os.Getenv("CALL_DATA"))
	if !strings.Contains(string(call), "literal $()") || !strings.Contains(string(call), "fixture-token") {
		t.Fatal(string(call))
	}
	t.Setenv("TEST_ACTOR", "wrong")
	if e := github([]string{"author", "--", "issue", "list"}, nil, &b); e == nil {
		t.Fatal("wrong role accepted")
	}
	t.Setenv("TEST_ACTOR", "author")
	inside, _ := filepath.Abs("token")
	os.WriteFile(inside, []byte("fixture-token"), 0o600)
	t.Setenv("GHSHIP_AUTHOR_PAT_FILE", inside)
	if e := github([]string{"author", "--", "issue", "list"}, nil, &b); e == nil {
		t.Fatal("workspace credential accepted")
	}
	t.Setenv("TEST_ACTOR", "dispatcher")
	t.Setenv("GH_TOKEN", "pollution")
	t.Setenv("GITHUB_TOKEN", "pollution")
	if e := github([]string{"dispatcher", "--", "issue", "list"}, nil, &b); e != nil {
		t.Fatal(e)
	}
	call, _ = os.ReadFile(os.Getenv("CALL_DATA"))
	if strings.Contains(string(call), "pollution") {
		t.Fatal(string(call))
	}
	t.Setenv("TEST_GH_EXIT", "7")
	e := github([]string{"dispatcher", "--", "issue", "list"}, nil, &b)
	var ce cliError
	if !errors.As(e, &ce) || ce.code != 7 {
		t.Fatal(e)
	}
}
func merged(t *testing.T, r string) {
	remote := filepath.Join(r, "origin.git")
	gitCmd(t, "init", "--bare", remote)
	gitCmd(t, "remote", "add", "origin", remote)
	gitCmd(t, "push", "-u", "origin", "master")
	gitCmd(t, "checkout", "-b", "codex/1-task")
	os.WriteFile("feature", []byte("feature"), 0o644)
	gitCmd(t, "add", ".")
	gitCmd(t, "commit", "-m", "feature")
	head := gitCmd(t, "rev-parse", "HEAD")
	gitCmd(t, "checkout", "master")
	gitCmd(t, "merge", "--squash", "codex/1-task")
	gitCmd(t, "commit", "-m", "squash")
	merge := gitCmd(t, "rev-parse", "HEAD")
	gitCmd(t, "push", "origin", "master")
	data, _ := json.Marshal(map[string]any{"state": "MERGED", "baseRefName": "master", "headRefName": "codex/1-task", "headRefOid": head, "mergeCommit": map[string]string{"oid": merge}, "isCrossRepository": false})
	os.WriteFile(os.Getenv("PR_DATA"), data, 0o600)
}
func TestTidySquashAndRepeat(t *testing.T) {
	r, _ := fixture(t)
	merged(t, r)
	gitCmd(t, "checkout", "codex/1-task")
	var b bytes.Buffer
	for i := 0; i < 2; i++ {
		if e := tidy([]string{"1", "--json"}, &b); e != nil {
			t.Fatal(e)
		}
	}
	if gitCmd(t, "branch", "--show-current") != "master" {
		t.Fatal("default branch not used")
	}
	if strings.Contains(gitCmd(t, "branch", "--list"), "codex/1-task") {
		t.Fatal("branch retained")
	}
	if _, e := os.Stat(os.Getenv("CALL_DATA")); !os.IsNotExist(e) {
		t.Fatal("platform mutated")
	}
}
func TestTidyPreservesWork(t *testing.T) {
	r, _ := fixture(t)
	merged(t, r)
	os.WriteFile("dirty", []byte("work"), 0o644)
	if e := tidy([]string{"1"}, &bytes.Buffer{}); e == nil {
		t.Fatal("dirty accepted")
	}
	os.Remove("dirty")
	gitCmd(t, "checkout", "codex/1-task")
	os.WriteFile("extra", []byte("work"), 0o644)
	gitCmd(t, "add", ".")
	gitCmd(t, "commit", "-m", "extra")
	if e := tidy([]string{"1"}, &bytes.Buffer{}); e == nil {
		t.Fatal("unique commit deleted")
	}
	if gitCmd(t, "branch", "--show-current") != "codex/1-task" {
		t.Fatal("switched despite extra work")
	}
}

func TestBlockedAssignedSlice(t *testing.T) {
	var items []issue
	json.Unmarshal([]byte(`[{"number":1,"assignees":[{"login":"author"}],"blockedBy":{"totalCount":1,"nodes":[{}]},"subIssuesSummary":{"total":0}}]`), &items)
	if _, _, e := selectIssue(items, "author"); e == nil {
		t.Fatal("blocked task resumed without parking")
	}
}

func TestEnterpriseRoleEnvironment(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	t.Setenv("GHSHIP_AUTHOR", "")
	t.Setenv("TEST_ORIGIN", "git@git.example.com:fixture/repo.git")
	t.Setenv("GH_TOKEN", "ambient")
	t.Setenv("GH_ENTERPRISE_TOKEN", "ambient-enterprise")
	t.Setenv("GH_HOST", "unrelated.example.com")
	token, e := protectSecret([]byte("selected"))
	if e != nil {
		t.Fatal(e)
	}
	p, _ := accountPath("git.example.com", "author")
	b, _ := json.Marshal(account{1, "git.example.com", "author", token})
	if e = atomicConfig(p, b); e != nil {
		t.Fatal(e)
	}
	binding := projectBinding{SchemaVersion: 1, Root: root, Repo: "git.example.com/fixture/repo", Author: "author", Reviewer: "reviewer", Admin: "admin"}
	b, _ = json.Marshal(binding)
	p, _ = bindingPath(root)
	if e = atomicConfig(p, b); e != nil {
		t.Fatal(e)
	}
	env, e := roleEnvironment("author")
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, v := range env {
		if v == "GH_ENTERPRISE_TOKEN=selected" {
			found = true
		}
		if strings.HasPrefix(v, "GH_TOKEN=") || strings.Contains(v, "ambient") || v == "GH_HOST=unrelated.example.com" {
			t.Fatal("wrong ambient credential", v)
		}
	}
	if !found {
		t.Fatal("enterprise token not selected")
	}
}
func TestRoleGitRewritesSSHWithoutGlobalConfiguration(t *testing.T) {
	_, _ = fixture(t)
	t.Setenv("GHSHIP_AUTHOR", "author")
	p := filepath.Join(t.TempDir(), "token")
	os.WriteFile(p, []byte("synthetic"), 0600)
	t.Setenv("GHSHIP_AUTHOR_PAT_FILE", p)
	var out bytes.Buffer
	if e := roleGit([]string{"author", "--", "config", "--get-regexp", "^url\\."}, nil, &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "https://github.com/.insteadof git@github.com:") {
		t.Fatal(out.String())
	}
	var direct bytes.Buffer
	c := exec.Command("git", "config", "--local", "--get-regexp", "^url\\.")
	c.Stdout = &direct
	_ = c.Run()
	if direct.Len() != 0 {
		t.Fatal("wrote local URL config")
	}
}

func TestPublicSubcommandHelp(t *testing.T) {
	for family, commands := range map[string][]string{"user": {"add", "update", "delete", "list"}, "project": {"install", "status", "update", "uninstall", "skill"}} {
		for _, command := range commands {
			for _, flag := range []string{"--help", "-h"} {
				var out bytes.Buffer
				if e := run([]string{family, command, flag}, nil, &out); e != nil || out.Len() == 0 {
					t.Fatal(family, command, flag, e)
				}
			}
		}
	}
}
