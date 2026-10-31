package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type connection[T any] struct {
	Total *int `json:"totalCount"`
	Nodes []T  `json:"nodes"`
	Page  *struct {
		More   *bool  `json:"hasNextPage"`
		Cursor string `json:"endCursor"`
	} `json:"pageInfo"`
}
type queueOwner struct {
	Login string `json:"login"`
}
type queueBlocker struct {
	ID     string  `json:"id"`
	Number int     `json:"number"`
	State  string  `json:"state"`
	Reason *string `json:"stateReason"`
}
type queueIssue struct {
	ID       string                   `json:"id"`
	Number   int                      `json:"number"`
	Owners   connection[queueOwner]   `json:"assignees"`
	Blockers connection[queueBlocker] `json:"blockedBy"`
	Children *struct {
		Total *int `json:"total"`
	} `json:"subIssuesSummary"`
}
type pageVerifier struct {
	total, count   int
	started, ended bool
	cursors        map[string]bool
}

func verifyPage[T any](v *pageVerifier, p connection[T]) error {
	if p.Total == nil || *p.Total < 0 || p.Nodes == nil || p.Page == nil || p.Page.More == nil || v.ended {
		return errors.New("incomplete native pagination fields")
	}
	if !v.started {
		v.total = *p.Total
		v.started = true
		v.cursors = map[string]bool{}
	}
	if v.total != *p.Total {
		return errors.New("query changed during pagination; retry")
	}
	v.count += len(p.Nodes)
	if v.count > v.total {
		return errors.New("pagination count exceeds native total")
	}
	if *p.Page.More {
		if len(p.Nodes) == 0 || p.Page.Cursor == "" || v.cursors[p.Page.Cursor] {
			return errors.New("pagination did not advance")
		}
		v.cursors[p.Page.Cursor] = true
	} else {
		v.ended = true
		if v.count != v.total {
			return errors.New("pagination incomplete")
		}
	}
	return nil
}
func extractGraph[T any](raw json.RawMessage, path []string) (connection[T], error) {
	var envelope struct {
		Errors []json.RawMessage `json:"errors"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Errors) > 0 {
		return connection[T]{}, errors.New("GraphQL query failed")
	}
	for _, key := range path {
		var object map[string]json.RawMessage
		if json.Unmarshal(raw, &object) != nil || len(object[key]) == 0 || string(object[key]) == "null" {
			return connection[T]{}, errors.New("native query scope unavailable")
		}
		raw = object[key]
	}
	var page connection[T]
	e := json.Unmarshal(raw, &page)
	return page, e
}
func graphPages[T any](env []string, args []string, path []string, visit func([]T) error) error {
	v := pageVerifier{}
	e := ghAPIPages(env, args, func(raw json.RawMessage) error {
		page, e := extractGraph[T](raw, path)
		if e != nil {
			return e
		}
		if e = verifyPage(&v, page); e != nil {
			return e
		}
		return visit(page.Nodes)
	})
	if e != nil {
		return e
	}
	if !v.ended {
		return errors.New("GraphQL pagination incomplete")
	}
	return nil
}
func completeNested[T any](env []string, initial connection[T], args []string, path []string) ([]T, error) {
	if initial.Total == nil || *initial.Total < 0 || initial.Nodes == nil {
		return nil, errors.New("incomplete nested connection")
	}
	if *initial.Total == len(initial.Nodes) {
		return initial.Nodes, nil
	}
	rows := []T{}
	e := graphPages[T](env, args, path, func(page []T) error { rows = append(rows, page...); return nil })
	if e != nil {
		return nil, e
	}
	if len(rows) != *initial.Total {
		return nil, errors.New("nested connection changed during pagination")
	}
	return rows, nil
}
func milestoneIssues(number int, author string) ([]issue, string, error) {
	origin, e := output(nil, "git", "remote", "get-url", "origin")
	if e != nil {
		return nil, "", e
	}
	repo, e := parseOrigin(origin)
	if e != nil {
		return nil, "", e
	}
	parts := strings.Split(repo, "/")
	env, e := roleEnvironment("author")
	if e != nil {
		return nil, "", e
	}
	for _, value := range env {
		if strings.HasPrefix(value, "GH_HOST=") && strings.TrimPrefix(value, "GH_HOST=") != parts[0] {
			return nil, "", errors.New("author credential host differs from origin")
		}
	}
	login, e := output(env, "gh", "api", "user", "--hostname", parts[0], "--jq", ".login")
	if e != nil {
		return nil, "", e
	}
	if author != "" && !strings.EqualFold(author, login) {
		return nil, "", errors.New("requested author differs from authenticated author")
	}
	args := func(q string, extra ...string) []string {
		return append([]string{"graphql", "--hostname", parts[0], "-f", "query=" + q, "-f", "owner=" + parts[1], "-f", "repo=" + parts[2]}, extra...)
	}
	// Only one pageInfo per query so gh advances the intended connection.
	q := `query($owner:String!,$repo:String!,$milestone:Int!,$endCursor:String){repository(owner:$owner,name:$repo){milestone(number:$milestone){issues(first:100,after:$endCursor,states:OPEN){totalCount pageInfo{hasNextPage endCursor} nodes{id number assignees(first:100){totalCount nodes{login}} blockedBy(first:100){totalCount nodes{id number state stateReason}} subIssuesSummary{total}}}}}}`
	items := []issue{}
	seen := map[string]bool{}
	e = graphPages[queueIssue](env, args(q, "-F", fmt.Sprintf("milestone=%d", number)), []string{"data", "repository", "milestone", "issues"}, func(rows []queueIssue) error {
		for _, row := range rows {
			if row.ID == "" || row.Number <= 0 || seen[row.ID] || row.Children == nil || row.Children.Total == nil || *row.Children.Total < 0 {
				return errors.New("duplicate or incomplete native Issue")
			}
			seen[row.ID] = true
			oq := `query($owner:String!,$repo:String!,$issue:Int!,$endCursor:String){repository(owner:$owner,name:$repo){issue(number:$issue){assignees(first:100,after:$endCursor){totalCount pageInfo{hasNextPage endCursor} nodes{login}}}}}`
			owners, e := completeNested(env, row.Owners, args(oq, "-F", fmt.Sprintf("issue=%d", row.Number)), []string{"data", "repository", "issue", "assignees"})
			if e != nil {
				return e
			}
			bq := `query($owner:String!,$repo:String!,$issue:Int!,$endCursor:String){repository(owner:$owner,name:$repo){issue(number:$issue){blockedBy(first:100,after:$endCursor){totalCount pageInfo{hasNextPage endCursor} nodes{id number state stateReason}}}}}`
			blockers, e := completeNested(env, row.Blockers, args(bq, "-F", fmt.Sprintf("issue=%d", row.Number)), []string{"data", "repository", "issue", "blockedBy"})
			if e != nil {
				return e
			}
			ownerMaps := []map[string]string{}
			ownerSeen := map[string]bool{}
			for _, owner := range owners {
				key := strings.ToLower(owner.Login)
				if key == "" || ownerSeen[key] {
					return errors.New("duplicate or missing assignee")
				}
				ownerSeen[key] = true
				ownerMaps = append(ownerMaps, map[string]string{"login": owner.Login})
			}
			open := []queueBlocker{}
			blockSeen := map[string]bool{}
			for _, block := range blockers {
				if block.ID == "" || block.Number <= 0 || blockSeen[block.ID] || (block.State != "OPEN" && block.State != "CLOSED") {
					return errors.New("duplicate or incomplete blocker")
				}
				blockSeen[block.ID] = true
				if block.State == "CLOSED" && (block.Reason == nil || *block.Reason != "COMPLETED") {
					return errors.New("closed dependency not completed; resolve acceptance before claiming")
				}
				if block.State == "OPEN" {
					open = append(open, block)
				}
			}
			raw, _ := json.Marshal(map[string]any{"number": row.Number, "assignees": ownerMaps, "blockedBy": map[string]any{"totalCount": len(open), "nodes": open}, "subIssuesSummary": map[string]any{"total": *row.Children.Total}})
			var item issue
			if e = json.Unmarshal(raw, &item); e != nil {
				return e
			}
			if *item.SubIssuesSummary.Total != 0 {
				continue
			}
			if ownerSeen[strings.ToLower(login)] {
				items = append(items, item)
			} else if len(ownerMaps) == 0 && len(open) == 0 {
				replaced := false
				for i, existing := range items {
					if len(*existing.Assignees) == 0 {
						if item.Number < existing.Number {
							items[i] = item
						}
						replaced = true
						break
					}
				}
				if !replaced {
					items = append(items, item)
				}
			}
			if len(items) > 3 {
				return errors.New("multiple assigned slices; resolve active work")
			}
		}
		return nil
	})
	if e != nil {
		return nil, "", e
	}
	return items, login, nil
}
