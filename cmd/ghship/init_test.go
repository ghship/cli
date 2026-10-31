package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodeownerUsesSelectedExactLogin(t *testing.T) {
	for _, tt := range []struct {
		content string
		want    bool
	}{
		{"# * @chosen-reviewer\n* @other\n", false},
		{"* @chosen-reviewer-old\n", false},
		{"* @other # @chosen-reviewer\n", false},
		{"* @Chosen-Reviewer @other\n", true},
		{"/src/ @chosen-reviewer\n", true},
	} {
		if got := hasCodeowner([]byte(tt.content), "chosen-reviewer"); got != tt.want {
			t.Fatalf("%q got %v want %v", tt.content, got, tt.want)
		}
	}
}

func TestInitNativeReadyAndMultipleProjects(t *testing.T) {
	_, root := fixture(t)
	home := setupAuthHome(t)
	gitCmd(t, "remote", "add", "origin", "https://github.com/fixture/repo.git")
	if e := lifecycle("install", nil, &bytes.Buffer{}); e != nil {
		t.Fatal(e)
	}
	weakRules := false
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			login := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			fmt.Fprintf(w, `{"login":%q}`, login)
		case "/repos/fixture/repo":
			fmt.Fprint(w, `{"full_name":"fixture/repo","default_branch":"master","has_issues":true,"delete_branch_on_merge":true,"permissions":{"admin":true}}`)
		case "/repos/fixture/repo/collaborators/developer/permission", "/repos/fixture/repo/collaborators/reviewer/permission":
			fmt.Fprint(w, `{"permission":"write"}`)
		case "/repos/fixture/repo/rules/branches/master":
			if weakRules {
				fmt.Fprint(w, `[{"type":"pull_request","parameters":{"required_approving_review_count":1,"require_code_owner_review":true}},{"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"real-ci"}]}}]`)
				return
			}
			fmt.Fprint(w, `[{"type":"deletion"},{"type":"non_fast_forward"},{"type":"pull_request","parameters":{"required_approving_review_count":1,"require_code_owner_review":true,"require_last_push_approval":true,"dismiss_stale_reviews_on_push":true,"required_review_thread_resolution":true}},{"type":"required_status_checks","parameters":{"strict_required_status_checks_policy":true,"required_status_checks":[{"context":"real-ci"}]}}]`)
		case "/repos/fixture/repo/pulls/12":
			fmt.Fprint(w, `{"merged":true,"base":{"ref":"master","repo":{"full_name":"fixture/repo"}},"head":{"sha":"tested-sha"},"user":{"login":"developer"}}`)
		case "/repos/fixture/repo/pulls/12/reviews":
			fmt.Fprint(w, `[{"state":"APPROVED","commit_id":"tested-sha","user":{"login":"reviewer"}}]`)
		case "/repos/fixture/repo/commits/tested-sha/check-runs":
			fmt.Fprint(w, `{"total_count":1,"check_runs":[{"name":"real-ci","conclusion":"success"}]}`)
		default:
			t.Errorf("unexpected endpoint %s", r.URL.Path)
			w.WriteHeader(500)
		}
	})
	for _, user := range []string{"developer", "reviewer"} {
		if e := users([]string{"add", "--user", user, "--token-stdin", "--non-interactive"}, tokenJSONReader(user), &bytes.Buffer{}); e != nil {
			t.Fatal(e)
		}
	}
	os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0755)
	os.WriteFile(filepath.Join(root, ".github", "CODEOWNERS"), []byte("* @reviewer\n"), 0644)
	os.WriteFile(filepath.Join(root, ".github", "workflows", "test.yml"), []byte("name: real-ci\n"), 0644)
	var out bytes.Buffer
	args := []string{"--project", root, "--repo", "github.com/fixture/repo", "--author", "developer", "--reviewer", "reviewer", "--admin", "admin", "--configure", "--confirm-auto-close", "--acceptance-pr", "12", "--non-interactive", "--json"}
	for i := 0; i < 2; i++ {
		out.Reset()
		if e := initialize(args, nil, &out); e != nil {
			t.Fatal(e, out.String())
		}
		var result map[string]any
		json.Unmarshal(out.Bytes(), &result)
		if result["ready"] != true {
			t.Fatal(out.String())
		}
	}
	// Exercise public dispatch across a continuous configured project lifecycle.
	dispatch := func(command []string) {
		t.Helper()
		out.Reset()
		if e := run(command, nil, &out); e != nil {
			t.Fatal(command, e, out.String())
		}
	}
	dispatch(append([]string{"init"}, args...))
	dispatch(append([]string{"project", "install"}, args...))
	dispatch([]string{"project", "status", "--project", root, "--json"})
	dispatch([]string{"project", "skill", "--project", root, "--create", "--mode", "override", "--overrides", "testing,acceptance", "--json"})
	dispatch([]string{"project", "skill", "--project", root, "--show", "--json"})
	customBefore, _ := os.ReadFile(filepath.Join(root, ".ghship", "skill.md"))
	dispatch([]string{"project", "update", "--project", root, "--json"})
	for _, scope := range []string{"user", "project"} {
		dispatch([]string{"doctor", "--scope", scope, "--json"})
	}
	dispatch([]string{"version"})
	if strings.TrimSpace(out.String()) != version {
		t.Fatal("version output incorrect")
	}
	for _, role := range []string{"author", "reviewer", "dispatcher"} {
		actor := map[string]string{"author": "developer", "reviewer": "reviewer", "dispatcher": "admin"}[role]
		t.Setenv("TEST_ACTOR", actor)
		dispatch([]string{"gh", role, "--", "api", "user", "--jq", ".login"})
		if strings.TrimSpace(out.String()) != actor {
			t.Fatal("role identity output incorrect")
		}
		dispatch([]string{"git", role, "--", "status", "--short"})
	}
	t.Setenv("TEST_ACTOR", "admin")
	dispatch([]string{"project", "uninstall", "--project", root, "--json"})
	after, _ := os.ReadFile(filepath.Join(root, ".ghship", "skill.md"))
	if !bytes.Equal(customBefore, after) {
		t.Fatal("dispatch lifecycle lost customization")
	}
	dispatch([]string{"project", "status", "--project", root, "--json"})
	var status map[string]any
	json.Unmarshal(out.Bytes(), &status)
	if status["installed"] != false || status["bound"] != false {
		t.Fatal("uninstall retained active installation", status)
	}
	dispatch(append([]string{"project", "install"}, args...))
	weakRules = true
	out.Reset()
	if e := initialize(args, nil, &out); e == nil {
		t.Fatal("weak protection reported ready")
	}
	var weakResult map[string]any
	json.Unmarshal(out.Bytes(), &weakResult)
	if weakResult["ready"] != false {
		t.Fatal(out.String())
	}
	refs, e := accountReferences("github.com", "developer")
	if e != nil || len(refs) != 1 || refs[0] != root {
		t.Fatal(refs, e)
	}
	if _, e := os.Stat(filepath.Join(home, ".agents", "skills", "ghship", "SKILL.md")); e != nil {
		t.Fatal("shared skill lost")
	}
	if e := initialize([]string{"--project", root, "--repo", "github.com/other/repo", "--non-interactive"}, nil, &out); e == nil {
		t.Fatal("wrong repository target accepted")
	}
}

func TestIntegrationRequiresDefaultBaseAndNamedRequiredCI(t *testing.T) {
	wrongBase, wrongCheck := false, false
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/team/repo/pulls/12":
			base := "main"
			if wrongBase {
				base = "other"
			}
			fmt.Fprintf(w, `{"merged":true,"base":{"ref":%q,"repo":{"full_name":"team/repo"}},"head":{"sha":"head-sha"},"user":{"login":"developer"}}`, base)
		case "/repos/team/repo/pulls/12/reviews":
			fmt.Fprint(w, `[{"state":"APPROVED","commit_id":"head-sha","user":{"login":"reviewer"}}]`)
		case "/repos/team/repo/commits/head-sha/check-runs":
			name := "real-ci"
			if wrongCheck {
				name = "unrelated"
			}
			fmt.Fprintf(w, `{"total_count":1,"check_runs":[{"name":%q,"conclusion":"success"}]}`, name)
		default:
			w.WriteHeader(500)
		}
	})
	api := githubAPI{"github.com", "synthetic"}
	if e := verifyIntegration(api, "team/repo", 12, "reviewer", "main", []string{"real-ci"}); e != nil {
		t.Fatal(e)
	}
	wrongBase = true
	if e := verifyIntegration(api, "team/repo", 12, "reviewer", "main", []string{"real-ci"}); e == nil {
		t.Fatal("other branch accepted")
	}
	wrongBase = false
	wrongCheck = true
	if e := verifyIntegration(api, "team/repo", 12, "reviewer", "main", []string{"real-ci"}); e == nil {
		t.Fatal("unrelated CI accepted")
	}
}
