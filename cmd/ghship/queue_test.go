package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"
)

func testPage[T any](total int, nodes []T, more bool, cursor string) connection[T] {
	p := connection[T]{Total: &total, Nodes: nodes}
	p.Page = &struct {
		More   *bool  `json:"hasNextPage"`
		Cursor string `json:"endCursor"`
	}{&more, cursor}
	return p
}

// Test producer simulates native gh output; production delegates pagination to gh.
func readPages[T any](fetch func(string) (connection[T], error)) ([]T, error) {
	all := []T{}
	v := pageVerifier{}
	cursor := ""
	for {
		page, e := fetch(cursor)
		if e != nil {
			return nil, e
		}
		if e = verifyPage(&v, page); e != nil {
			return nil, e
		}
		all = append(all, page.Nodes...)
		if v.ended {
			return all, nil
		}
		cursor = page.Page.Cursor
	}
}
func TestNativePaginationBeyondThousand(t *testing.T) {
	calls := 0
	items, err := readPages(func(cursor string) (connection[int], error) {
		start := 0
		if cursor != "" {
			var e error
			start, e = strconv.Atoi(cursor)
			if e != nil {
				return connection[int]{}, e
			}
		}
		end := start + 100
		if end > 12037 {
			end = 12037
		}
		nodes := []int{}
		for i := start; i < end; i++ {
			nodes = append(nodes, i)
		}
		calls++
		return testPage(12037, nodes, end < 12037, strconv.Itoa(end)), nil
	})
	if err != nil || len(items) != 12037 || calls != 121 || items[12036] != 12036 {
		t.Fatal(len(items), calls, err)
	}
	// The fully read queue must bypass stdin's conservative truncation guard.
	native := make([]issue, 0, len(items))
	for _, n := range items {
		var row issue
		if e := json.Unmarshal([]byte(fmt.Sprintf(`{"number":%d,"assignees":[],"blockedBy":{"totalCount":0,"nodes":[]},"subIssuesSummary":{"total":0}}`, n+1)), &row); e != nil {
			t.Fatal(e)
		}
		native = append(native, row)
	}
	n, action, e := selectCompleteIssues(native, "author")
	if e != nil || n != 1 || action != "claim" {
		t.Fatal(n, action, e)
	}
}
func TestPaginationRejectsPartialResults(t *testing.T) {
	for _, mode := range []string{"network", "count", "cursor", "missing", "short"} {
		t.Run(mode, func(t *testing.T) {
			items, e := readPages(func(cursor string) (connection[int], error) {
				if cursor == "" {
					if mode == "missing" {
						return connection[int]{}, nil
					}
					if mode == "short" {
						return testPage(3, []int{1}, false, ""), nil
					}
					return testPage(3, []int{1}, true, "same"), nil
				}
				switch mode {
				case "network":
					return connection[int]{}, errors.New("offline")
				case "count":
					return testPage(4, []int{2}, false, ""), nil
				default:
					return testPage(3, []int{2}, true, "same"), nil
				}
			})
			if e == nil || items != nil {
				t.Fatal("partial results escaped", items, e)
			}
		})
	}
}
