package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestRESTPaginationAndUntrustedNext(t *testing.T) {
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `[3]`)
			return
		}
		w.Header().Set("Link", "<"+apiBaseOverride+"/items?page=2>; rel=\"next\"")
		fmt.Fprint(w, `[1,2]`)
	})
	rows, e := listREST[int](githubAPI{"github.com", "test"}, "items?per_page=100")
	if e != nil || len(rows) != 3 || rows[2] != 3 {
		t.Fatal(rows, e)
	}

}
func TestRESTPartialFailureAndCancellation(t *testing.T) {
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Link", "<"+apiBaseOverride+"/items?page=2>; rel=\"next\"")
		fmt.Fprint(w, `[1]`)
	})
	if rows, e := listREST[int](githubAPI{"github.com", "test"}, "items"); e == nil || rows != nil {
		t.Fatal("partial result escaped", rows, e)
	}
	old := operationContext
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	operationContext = ctx
	t.Cleanup(func() { operationContext = old })
	if _, e := (githubAPI{"github.com", "test"}).call("GET", "items", nil, new([]int)); e == nil {
		t.Fatal("cancelled query succeeded")
	}
}
func TestIntegrationDecisionOnLaterPages(t *testing.T) {
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		later := r.URL.Query().Get("page") == "2"
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/12"):
			fmt.Fprint(w, `{"merged":true,"base":{"ref":"main","repo":{"full_name":"team/repo"}},"head":{"sha":"sha"},"user":{"login":"author"}}`)
		case strings.HasSuffix(r.URL.Path, "/reviews"):
			if later {
				fmt.Fprint(w, `[{"state":"APPROVED","commit_id":"sha","user":{"login":"reviewer"}}]`)
			} else {
				w.Header().Set("Link", "<"+apiBaseOverride+r.URL.Path+"?page=2>; rel=\"next\"")
				fmt.Fprint(w, `[{"state":"COMMENTED","user":{"login":"other"}}]`)
			}
		case strings.HasSuffix(r.URL.Path, "/check-runs"):
			if later {
				fmt.Fprint(w, `{"total_count":2,"check_runs":[{"name":"real-ci","conclusion":"success"}]}`)
			} else {
				w.Header().Set("Link", "<"+apiBaseOverride+r.URL.Path+"?page=2>; rel=\"next\"")
				fmt.Fprint(w, `{"total_count":2,"check_runs":[{"name":"other","conclusion":"success"}]}`)
			}
		default:
			w.WriteHeader(404)
		}
	})
	if e := verifyIntegration(githubAPI{"github.com", "test"}, "team/repo", 12, "reviewer", "main", []string{"real-ci"}); e != nil {
		t.Fatal(e)
	}
}
func TestGraphMissingHasNextPageFails(t *testing.T) {
	var page connection[int]
	if e := json.Unmarshal([]byte(`{"totalCount":0,"nodes":[],"pageInfo":{}}`), &page); e != nil {
		t.Fatal(e)
	}
	if rows, e := readPages(func(string) (connection[int], error) { return page, nil }); e == nil || rows != nil {
		t.Fatal("missing cursor flag accepted", rows, e)
	}
}
