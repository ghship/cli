package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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
		var out bytes.Buffer
		err := next([]string{"--author", login, "--json"}, strings.NewReader(input), &out)
		var ce cliError
		if !errors.As(err, &ce) || ce.code != 2 || out.Len() != 0 {
			t.Errorf("%q: output %q error %v", login, out.String(), err)
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
