package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

var operationContext = context.Background()

// GitHub CLI owns transport and pagination. Go consumes one native page at a time.
func ghAPIPages(env []string, args []string, visit func(json.RawMessage) error) error {
	budget := 10 * time.Minute
	if value := os.Getenv("GHSHIP_QUERY_TIMEOUT"); value != "" {
		var e error
		budget, e = time.ParseDuration(value)
		if e != nil || budget <= 0 {
			return usage("GHSHIP_QUERY_TIMEOUT must be a positive duration, for example 10m")
		}
	}
	ctx, stop := context.WithTimeout(operationContext, budget)
	defer stop()
	args = append([]string{"api", "--paginate"}, args...)
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Env = env
	pipe, e := cmd.StdoutPipe()
	if e != nil {
		return e
	}
	// Native diagnostics can include request details; expose a sanitized failure.
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		return errors.New("GitHub CLI unavailable; install gh")
	}
	decoder := json.NewDecoder(pipe)
	for {
		var raw json.RawMessage
		e = decoder.Decode(&raw)
		if e == io.EOF {
			break
		}
		if e != nil {
			cmd.Process.Kill()
			cmd.Wait()
			return errors.New("invalid native GitHub page")
		}
		if e = visit(raw); e != nil {
			cmd.Process.Kill()
			cmd.Wait()
			return e
		}
	}
	if e = cmd.Wait(); e != nil {
		return errors.New("GitHub paginated query failed or cancelled; no complete result")
	}
	return nil
}
func restPages[T any](g githubAPI, path string, decode func(json.RawMessage) ([]T, *int, error)) ([]T, error) {
	env := append(cleanGHEnv(), "GH_HOST="+g.host, githubTokenEnv(g.host)+"="+g.token)
	endpoint := path
	if apiBaseOverride != "" {
		endpoint = apiBaseOverride + "/" + strings.TrimPrefix(path, "/")
	}
	all := []T{}
	total := -1
	pages := 0
	e := ghAPIPages(env, []string{endpoint, "--hostname", g.host}, func(raw json.RawMessage) error {
		rows, count, e := decode(raw)
		if e != nil || rows == nil {
			return errors.New("invalid REST list response")
		}
		if count != nil {
			if *count < 0 || (total >= 0 && total != *count) {
				return errors.New("REST list changed during pagination")
			}
			total = *count
		}
		all = append(all, rows...)
		pages++
		return nil
	})
	if e != nil {
		return nil, e
	}
	if pages == 0 || (total >= 0 && total != len(all)) {
		return nil, errors.New("REST list incomplete")
	}
	return all, nil
}
func listREST[T any](g githubAPI, path string) ([]T, error) {
	return restPages(g, path, func(raw json.RawMessage) ([]T, *int, error) {
		var rows []T
		e := json.Unmarshal(raw, &rows)
		return rows, nil, e
	})
}
