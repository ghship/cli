package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedAccountLifecycleAndBinding(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			w.WriteHeader(500)
			return
		}
		synthetic := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if synthetic == "invalid" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprintf(w, `{"login":%q}`, "Mona-Cat_Octo")
	})
	var out bytes.Buffer
	add := []string{"add", "--user", "Mona-Cat_Octo", "--token-stdin", "--non-interactive", "--json"}
	if err := users(add, tokenJSONReader("synthetic-old"), &out); err != nil {
		t.Fatalf("add: %v", err)
	}
	file, err := accountPath("github.com", "mona-cat_octo")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	update := []string{"update", "--user", "MONA-CAT_OCTO", "--token-stdin", "--non-interactive", "--json"}
	out.Reset()
	if err = users(update, tokenJSONReader("invalid"), &out); err == nil {
		t.Fatal("invalid token accepted")
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed update changed account: %v", err)
	}
	out.Reset()
	if err = users(update, tokenJSONReader("synthetic-new"), &out); err != nil {
		t.Fatalf("update: %v", err)
	}
	out.Reset()
	if err = users([]string{"list", "--user", "MONA-CAT_OCTO", "--json"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Login  string `json:"login"`
		Status string `json:"status"`
	}
	if err = json.Unmarshal(out.Bytes(), &rows); err != nil || len(rows) != 1 || rows[0].Login != "mona-cat_octo" || rows[0].Status != "valid" {
		t.Fatalf("list did not find managed user: %v", err)
	}
	binding := projectBinding{SchemaVersion: 1, Root: root, Repo: "github.com/fixture/repo", Author: "mona-cat_octo", Reviewer: "reviewer_octo", Admin: "octo_admin"}
	b, _ := json.Marshal(binding)
	bp, err := bindingPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = atomicConfig(bp, b); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = users([]string{"delete", "--user", "mona-cat_octo", "--json"}, nil, &out); err == nil {
		t.Fatal("referenced account deleted")
	}
	if err = initialize([]string{"--project", root, "--remove-binding"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err = users([]string{"delete", "--user", "Mona-Cat_Octo", "--json"}, nil, &out); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err = os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("account remains: %v", err)
	}
}

func TestManagedUsersPassInitializationPreflight(t *testing.T) {
	dir := installDir(t)
	var out bytes.Buffer
	err := run([]string{"install", "--check", "--dir", dir, "--author", "mona-cat_octo", "--reviewer", "reviewer_octo", "--admin", "octo_admin", "--repo", "github.com/mona-cat_octo/repo", "--json"}, nil, &out)
	if err != nil {
		t.Fatalf("managed identities rejected before setup: %v", err)
	}
}

func TestManagedLoginPathValidationAndLegacyPreservation(t *testing.T) {
	setupAuthHome(t)
	for _, login := range []string{"../escape", "a/b", "a\\\\b", "mona_cat/../x", "mona_cat!"} {
		if _, err := accountPath("github.com", login); err == nil {
			t.Errorf("unsafe path accepted: %q", login)
		}
	}
	// Existing ordinary-name records remain visible even when GitHub would reject
	// their spelling; changing validation must not silently hide user data.
	name, err := accountPath("github.com", "legacy-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(name, []byte("invalid fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err = users([]string{"list", "--json"}, nil, &out); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Login  string `json:"login"`
		Status string `json:"status"`
	}
	if err = json.Unmarshal(out.Bytes(), &rows); err != nil || len(rows) != 1 || rows[0].Login != "legacy-" || rows[0].Status != "unavailable" {
		t.Fatalf("legacy account hidden: %v", err)
	}
}

func TestManagedUsersInitializeAndRoleBindings(t *testing.T) {
	_, root := fixture(t)
	setupAuthHome(t)
	gitCmd(t, "remote", "add", "origin", "https://github.com/fixture/repo.git")
	if err := lifecycle("install", nil, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			login := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if login == "admin" {
				login = "octo_admin"
			}
			fmt.Fprintf(w, `{"login":%q}`, login)
		case "/repos/fixture/repo":
			fmt.Fprint(w, `{"full_name":"fixture/repo","default_branch":"master","has_issues":true,"delete_branch_on_merge":true,"permissions":{"admin":true}}`)
		case "/repos/fixture/repo/collaborators/mona-cat_octo/permission", "/repos/fixture/repo/collaborators/reviewer_octo/permission":
			fmt.Fprint(w, `{"permission":"write"}`)
		case "/repos/fixture/repo/rules/branches/master":
			fmt.Fprint(w, `[]`)
		default:
			w.WriteHeader(500)
		}
	})
	for _, login := range []string{"mona-cat_octo", "reviewer_octo"} {
		if err := users([]string{"add", "--user", login, "--token-stdin", "--non-interactive", "--json"}, tokenJSONReader(login), &bytes.Buffer{}); err != nil {
			t.Fatalf("add managed account: %v", err)
		}
	}
	args := []string{"--project", root, "--repo", "github.com/fixture/repo", "--author", "Mona-Cat_Octo", "--reviewer", "Reviewer_Octo", "--admin", "Octo_Admin", "--non-interactive", "--json"}
	for _, prefix := range [][]string{{"init"}, {"project", "install"}} {
		var out bytes.Buffer
		err := run(append(append([]string{}, prefix...), args...), nil, &out)
		var ce cliError
		if !errors.As(err, &ce) || ce.code != 4 {
			t.Fatalf("setup should be pending governance: %v", err)
		}
		binding, e := readBinding(root)
		if e != nil || binding.Author != "mona-cat_octo" || binding.Reviewer != "reviewer_octo" || binding.Admin != "Octo_Admin" {
			t.Fatalf("managed binding unavailable: %v", e)
		}
	}
	for _, role := range []struct{ role, actor string }{{"author", "MONA-CAT_OCTO"}, {"reviewer", "REVIEWER_OCTO"}, {"dispatcher", "OCTO_ADMIN"}} {
		t.Setenv("TEST_ACTOR", role.actor)
		if _, err := roleEnvironment(role.role); err != nil {
			t.Fatalf("%s role: %v", role.role, err)
		}
	}
	var out bytes.Buffer
	if err := project([]string{"status", "--project", root, "--json"}, nil, &out); err != nil {
		t.Fatalf("status rejected managed binding: %v", err)
	}
}

func TestManagedLoginBoundaries(t *testing.T) {
	for _, login := range []string{"user_abc", "mona-cat_octo", "octo_admin", "2abvd19d_admin", strings.Repeat("a", 30) + "_12345678"} {
		if !validGitHubLogin(login) || !validStoredLogin(login) {
			t.Errorf("valid login rejected: %q", login)
		}
	}
	for _, login := range []string{"user_ab", "user_123456789", strings.Repeat("a", 31) + "_12345678", "-user_octo", "user-_octo", "user--name_octo", "user_octo_x", "user_../x", "user_åbc"} {
		if validGitHubLogin(login) || validStoredLogin(login) {
			t.Errorf("invalid managed login accepted: %q", login)
		}
	}
}

func TestAccountListRejectsExplicitEmptyFilters(t *testing.T) {
	setupAuthHome(t)
	name, err := accountPath("github.com", "mona-cat_octo")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	before := []byte("invalid fixture")
	if err = os.WriteFile(name, before, 0600); err != nil {
		t.Fatal(err)
	}
	for _, option := range []string{"--host", "--user"} {
		var out bytes.Buffer
		err = users([]string{"list", option, "", "--json"}, nil, &out)
		var ce cliError
		if !errors.As(err, &ce) || ce.code != 2 || out.Len() != 0 {
			t.Errorf("%s empty filter returned unfiltered results", option)
		}
		after, e := os.ReadFile(name)
		if e != nil || !bytes.Equal(before, after) {
			t.Fatalf("account changed: %v", e)
		}
	}
}

func TestLifecycleCheckIsReadOnlyForEveryAction(t *testing.T) {
	dir := installDir(t)
	for _, action := range []string{"install", "update", "uninstall"} {
		var out bytes.Buffer
		err := run([]string{action, "--check", "--dir", dir, "--json"}, nil, &out)
		var state struct {
			Managed bool `json:"managed"`
		}
		if err != nil || json.Unmarshal(out.Bytes(), &state) != nil || state.Managed {
			t.Errorf("%s check: %q %v", action, out.String(), err)
		}
		if _, err = os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s check changed directory: %v", action, err)
		}
	}
}

func TestAccountMutationRejectsInvalidNewNamesButPreservesLegacy(t *testing.T) {
	setupAuthHome(t)
	calls := 0
	testAPI(t, func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{"login":"bad--user"}`) })
	for _, login := range []string{"bad--user", "legacy-"} {
		var out bytes.Buffer
		err := users([]string{"add", "--user", login, "--token-stdin", "--non-interactive", "--json"}, tokenJSONReader("synthetic"), &out)
		var ce cliError
		if !errors.As(err, &ce) || ce.code != 2 || out.Len() != 0 {
			t.Errorf("invalid new login accepted: %q", login)
		}
	}
	if calls != 0 {
		t.Fatal("invalid account triggered identity request")
	}
	file, err := accountPath("github.com", "legacy-")
	if err != nil {
		t.Fatal(err)
	}
	protected, err := protectSecret([]byte("synthetic"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(account{SchemaVersion: 1, Host: "github.com", Login: "legacy-", Secret: protected})
	if err = atomicConfig(file, raw); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = users([]string{"update", "--user", "legacy-", "--token-stdin", "--non-interactive", "--json"}, tokenJSONReader("synthetic-new"), &out)
	var ce cliError
	if !errors.As(err, &ce) || ce.code != 2 {
		t.Errorf("invalid legacy update accepted: %v", err)
	}
	after, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatalf("legacy account changed: %v", err)
	}
	if calls != 0 {
		t.Fatal("invalid update triggered identity request")
	}
	out.Reset()
	if err = users([]string{"delete", "--user", "legacy-", "--json"}, nil, &out); err != nil {
		t.Fatalf("legacy deletion rejected: %v", err)
	}
	if _, err = os.Stat(file); !os.IsNotExist(err) {
		t.Fatalf("legacy account remains: %v", err)
	}
}
