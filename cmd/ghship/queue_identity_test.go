package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func queueFixture(number int, owner string, blocked, children int) string {
	assignees := "[]"
	if owner != "" {
		assignees = fmt.Sprintf(`[{"login":%q}]`, owner)
	}
	nodes := "[]"
	if blocked > 0 {
		nodes = `[{}]`
	}
	return fmt.Sprintf(`{"number":%d,"assignees":%s,"blockedBy":{"totalCount":%d,"nodes":%s},"subIssuesSummary":{"total":%d}}`, number, assignees, blocked, nodes, children)
}

func TestNextRespectsCaseInsensitiveOwnership(t *testing.T) {
	for _, test := range []struct {
		name, input string
		wantIssue   int
		wantAction  string
		wantErr     bool
	}{
		{"resume", "[" + queueFixture(1, "", 0, 0) + "," + queueFixture(10, "Developer", 0, 0) + "]", 10, "resume", false},
		{"blocked", "[" + queueFixture(1, "", 0, 0) + "," + queueFixture(10, "Developer", 1, 0) + "]", 0, "", true},
		{"multiple", "[" + queueFixture(1, "", 0, 0) + "," + queueFixture(10, "Developer", 0, 0) + "," + queueFixture(11, "DEVELOPER", 0, 0) + "]", 0, "", true},
		{"other owner", "[" + queueFixture(1, "", 0, 0) + "," + queueFixture(10, "other", 0, 0) + "]", 1, "claim", false},
		{"parent", "[" + queueFixture(1, "", 0, 0) + "," + queueFixture(10, "Developer", 0, 2) + "]", 1, "claim", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out bytes.Buffer
			err := next([]string{"--author", "developer", "--json"}, strings.NewReader(test.input), &out)
			if test.wantErr {
				if err == nil || out.Len() != 0 {
					t.Fatalf("unsafe selection: %s %v", out.String(), err)
				}
				return
			}
			var got struct {
				Issue  int    `json:"issue"`
				Action string `json:"action"`
			}
			if err != nil || json.Unmarshal(out.Bytes(), &got) != nil || got.Issue != test.wantIssue || got.Action != test.wantAction {
				t.Fatalf("result %s: %v", out.String(), err)
			}
		})
	}
}

func TestNextRejectsInvalidAuthorBeforeSelection(t *testing.T) {
	input := "[" + queueFixture(1, "", 0, 0) + "]"
	for _, login := range []string{"", " ", "bad@user", "bad-", "bad--user"} {
		for _, mode := range [][]string{nil, {"--milestone", "1"}} {
			var out bytes.Buffer
			args := append(append([]string{}, mode...), "--author", login, "--json")
			err := next(args, strings.NewReader(input), &out)
			var ce cliError
			if !errors.As(err, &ce) || ce.code != 2 || out.Len() != 0 {
				t.Errorf("%q %v: output %q error %v", login, mode, out.String(), err)
			}
		}
	}
}

func TestCompleteQueueFindsCaseInsensitiveOwnerAfterThousand(t *testing.T) {
	items := make([]issue, 0, 1201)
	for n := 1; n <= 1201; n++ {
		owner := ""
		if n == 1201 {
			owner = "Developer"
		}
		var row issue
		if err := json.Unmarshal([]byte(queueFixture(n, owner, 0, 0)), &row); err != nil {
			t.Fatal(err)
		}
		items = append(items, row)
	}
	n, action, err := selectCompleteIssues(items, "developer")
	if err != nil || n != 1201 || action != "resume" {
		t.Fatalf("incomplete selection: %d %s %v", n, action, err)
	}
}

func TestNextEmptyQueueHasNoCandidateExit(t *testing.T) {
	var out bytes.Buffer
	err := next([]string{"--author", "developer", "--json"}, strings.NewReader("[]"), &out)
	var ce cliError
	if !errors.As(err, &ce) || ce.code != 3 || out.Len() != 0 {
		t.Fatalf("empty queue: %q %v", out.String(), err)
	}
}

func TestNextAcceptsEnterpriseManagedUsernames(t *testing.T) {
	for _, login := range []string{"mona-cat_octo", "octo_admin", "2abvd19d_admin"} {
		var out bytes.Buffer
		input := "[" + queueFixture(1, "", 0, 0) + "," + queueFixture(10, strings.ToUpper(login), 0, 0) + "]"
		if err := next([]string{"--author", login, "--json"}, strings.NewReader(input), &out); err != nil {
			t.Fatalf("%s: %v", login, err)
		}
		var got struct {
			Issue  int    `json:"issue"`
			Action string `json:"action"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.Issue != 10 || got.Action != "resume" {
			t.Fatalf("%s: %s %v", login, out.String(), err)
		}
	}
}

func TestMilestoneOriginUsesVerifiedProjectEnvironment(t *testing.T) {
	base, _ := fixture(t)
	setupAuthHome(t)
	token := filepath.Join(base, "synthetic")
	if err := os.WriteFile(token, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHSHIP_AUTHOR", "author")
	t.Setenv("GHSHIP_AUTHOR_PAT_FILE", token)
	t.Setenv("TEST_ACTOR", "author")
	t.Setenv("GIT_DIR", filepath.Join(base, "other-git"))
	t.Setenv("GIT_WORK_TREE", filepath.Join(base, "other-worktree"))
	t.Setenv("TEST_REDIRECTED_ORIGIN", "https://github.com/other/repo.git")
	// The mock gh intentionally returns no page; inspect only its query scope.
	_, _, _ = milestoneIssues(1, "author")
	data, err := os.ReadFile(os.Getenv("CALL_DATA"))
	if err != nil {
		t.Fatalf("query did not reach fixture: %v", err)
	}
	var captured struct {
		Args []string `json:"args"`
	}
	if err = json.Unmarshal(data, &captured); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(captured.Args, "owner=fixture") || slices.Contains(captured.Args, "owner=other") {
		t.Fatalf("wrong query owner: %v", captured.Args)
	}
}

func TestMilestoneFailsIfOriginChangesBeforeQuery(t *testing.T) {
	base, _ := fixture(t)
	setupAuthHome(t)
	token := filepath.Join(base, "synthetic")
	if err := os.WriteFile(token, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GHSHIP_AUTHOR", "author")
	t.Setenv("GHSHIP_AUTHOR_PAT_FILE", token)
	t.Setenv("TEST_ACTOR", "author")
	t.Setenv("TEST_ORIGIN_COUNTER", filepath.Join(base, "origin-count"))
	_, _, err := milestoneIssues(1, "author")
	if err == nil || !strings.Contains(err.Error(), "origin changed") {
		t.Fatalf("changed origin not rejected: %v", err)
	}
	if _, err = os.Stat(os.Getenv("CALL_DATA")); !os.IsNotExist(err) {
		t.Fatalf("query executed after origin changed: %v", err)
	}
}
