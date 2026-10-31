// ghship is a small, stateless CLI for an agent-led GitHub delivery pipeline.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	assets "github.com/ghship/cli"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var version = assets.Version

const help = `ghship — native GitHub delivery pipeline helpers

Usage:
  ghship help [COMMAND]
  ghship version
  ghship install|update|uninstall [--standalone] [--dir PATH] [--user-only] [--json]
  ghship user add|update|delete|list [OPTIONS]
  ghship init [OPTIONS]
  ghship project install|status|update|uninstall|skill [OPTIONS]
  ghship doctor [--scope user|project] [--json]
  ghship git ROLE -- GIT_ARGUMENTS...
  ghship gh ROLE -- GH_ARGUMENTS...
  ghship next --milestone NUMBER [--author LOGIN] [--json]
  ghship next --author LOGIN [--json] < authorized-issues.json
  ghship tidy PR [--json]

Commands:
  version                   Print the installed product version.
  install/update/uninstall  Manage the bundled skill and CLI lifecycle.
  user                      Store and validate role credentials outside Git.
  init                      Bind this project to verified identities and governance.
  project                   Project installation, status, update, uninstall, skill.
  doctor                    Check required tools and native GitHub capabilities.
  git, gh                   Verify the role identity, then invoke native Git/gh unchanged.
  next                      Read-only selection from a milestone or complete Issue JSON.
  tidy                      Local-only maintenance after a merged PR; never closes Issues.

Project workflows and next milestone mode require Git/gh. User lifecycle and
next stdin mode do not. Use doctor for actual dependency checks.
COMMAND: version, install, update, uninstall, user, init, project, doctor,
git, gh, next, tidy. Command --help and user/project SUBCOMMAND --help are supported.
Exit: 0 scope complete, 1 failure, 2 usage, 3 next no slice, 4 setup/entry pending.
gh preserves the native gh exit code after identity verification.
JSON results on stdout; diagnostics on stderr. gh uses native --json.
Environment: GHSHIP_AUTHOR, GHSHIP_REVIEWER, GHSHIP_DISPATCHER;
GHSHIP_AUTHOR_PAT_FILE, GHSHIP_REVIEWER_PAT_FILE override external credential paths.
Default accounts: current project role binding (run init first).
No daemon, persisted queue or background continuation; one in-progress marker only.
See references/pipeline.md for document-to-product orchestration.
`
const ghHelp = `ghship gh ROLE -- GH_ARGUMENTS...
ROLE author|reviewer|dispatcher, separator -- required. Current project init binding
selects the expected identity; no default hardcoded accounts.
Run inside the target Git repository, including with explicit role overrides.
Stored author/reviewer credentials are outside Git, Unix protected files or
Windows current-user DPAPI.
Dispatcher uses local gh login. Ambient token variables are removed.
Explicit GHSHIP_AUTHOR/REVIEWER/DISPATCHER and external PAT_FILE overrides are supported
for controlled integration; verify login before invoking actual native gh command.
Arguments, streams and gh exit codes preserved. Credentials never printed.
Each role is used only by that role's authorized executor; account switching is not
independent acceptance. Git authenticated operations: ghship git author -- push ...
Examples:
 ghship gh author -- issue create --title "Task" --body-file issue.md
 ghship gh reviewer -- pr review 12 --approve --body-file review.md
 ghship gh dispatcher -- pr merge 12 --squash
`
const nextHelp = `ghship next --milestone NUMBER [--author LOGIN] [--json]
Or: ghship next --author LOGIN [--json] < authorized-issues.json
Milestone mode reads the current origin and bound author; automatically paginates
all Open Issues and blocker connections with no arbitrary total Issue limit.
Only after every page succeeds does it select; partial/error/changed queries fail.
Input: complete Open Issue list within the authorized milestone/parent scope.
Required native fields: number, assignees, blockedBy, subIssuesSummary.
stdin mode rejects missing fields, incomplete blocker nodes and 1000+ lists
(possible truncation); use milestone mode for automatic native pagination.
Resumes one uniquely assigned leaf; multiple owned leaves fail. Otherwise chooses
lowest-numbered unassigned leaf with no blockers. Parent Issues are not selected.
Owned tasks still require the caller to check dependencies before development.
Caller checks scope, pagination, acceptance criteria, workspace and active PRs;
then assigns with gh and reads back. Single dispatcher only: selection is not a lock.
Plain output: Issue number. JSON: {"issue":N,"action":"resume|claim"}.
Milestone JSON additionally includes its scope and complete=true after all pages succeed.
Native pagination calls default to a 10m budget; GHSHIP_QUERY_TIMEOUT accepts a
positive Go duration. Ctrl-C cancels; failure never returns a partial selection.
No candidate: exit 3 with explanation on stderr; never means product acceptance passed.
Example:
  gh issue list --milestone 1.0 --state open --limit 1000 \
    --json number,assignees,blockedBy,subIssuesSummary | ghship next --author developer --json
`
const tidyHelp = `ghship tidy PR [--json]
Requires a clean target repository and gh login. Reads PR and repository metadata;
requires a same-repository PR merged into the default branch, and matching origin.
Fetches default branch, verifies merge ancestry, fast-forwards local default branch,
compares its SHA with remote, then deletes only a local branch equal to the PR head.
Squash merges are supported; unique local commits, forks, dirty trees or divergence
are preserved and reported. Repeating after deletion is safe.
No remote branch deletion, Issue close, label write, test rerun or evidence comment.
Local maintenance failure does not change completed task status.
JSON: {"pr":N,"branch":NAME,"mainSHA":SHA,"cleaned":true}.
`

type cliError struct {
	code int
	err  error
}

func (e cliError) Error() string { return e.err.Error() }
func usage(s string) error       { return cliError{2, errors.New(s)} }
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	operationContext = ctx
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		var ce cliError
		if errors.As(err, &ce) {
			os.Exit(ce.code)
		}
		os.Exit(1)
	}
}
func run(a []string, in io.Reader, out io.Writer) error {
	if len(a) == 3 && (a[2] == "--help" || a[2] == "-h") {
		if (a[0] == "user" && (a[1] == "add" || a[1] == "update" || a[1] == "delete" || a[1] == "list")) || (a[0] == "project" && (a[1] == "install" || a[1] == "status" || a[1] == "update" || a[1] == "uninstall" || a[1] == "skill")) {
			return run([]string{"help", a[0]}, in, out)
		}
	}
	if len(a) == 2 && (a[1] == "--help" || a[1] == "-h") {
		return run([]string{"help", a[0]}, in, out)
	}
	if len(a) == 0 {
		fmt.Fprint(out, help)
		return nil
	}
	switch a[0] {
	case "git":
		return roleGit(a[1:], in, out)
	case "doctor":
		return doctor(a[1:], out)
	case "project":
		return project(a[1:], in, out)
	case "user":
		return users(a[1:], in, out)
	case "init":
		return initialize(a[1:], in, out)
	case "help", "--help", "-h":
		if len(a) == 1 {
			fmt.Fprint(out, help)
			return nil
		}
		if len(a) != 2 {
			return usage("help accepts one command")
		}
		switch a[1] {
		case "version":
			fmt.Fprintln(out, "ghship version\nPrint the installed product version. No arguments.")
		case "install", "update", "uninstall":
			fmt.Fprint(out, installHelp)
		case "git":
			fmt.Fprint(out, gitHelp)
		case "doctor":
			fmt.Fprint(out, doctorHelp)
		case "project":
			fmt.Fprint(out, projectHelp)
		case "user":
			fmt.Fprint(out, userHelp)
		case "init":
			fmt.Fprint(out, initHelp)
		case "gh":
			fmt.Fprint(out, ghHelp)
		case "next":
			fmt.Fprint(out, nextHelp)
		case "tidy":
			fmt.Fprint(out, tidyHelp)
		default:
			return usage("unknown help command")
		}
		return nil
	case "install", "update", "uninstall":
		return lifecycleEntry(a[0], a[1:], in, out)
	case "version", "--version":
		if len(a) != 1 {
			return usage("version accepts no arguments")
		}
		fmt.Fprintln(out, version)
		return nil
	case "gh":
		return github(a[1:], in, out)
	case "next":
		return next(a[1:], in, out)
	case "tidy":
		return tidy(a[1:], out)
	default:
		return usage("unknown command; use ghship help")
	}
}
func envDefault(k, v string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return v
}
func output(env []string, name string, a ...string) (string, error) {
	ctx, stop := context.WithTimeout(operationContext, 30*time.Second)
	defer stop()
	c := exec.CommandContext(ctx, name, a...)
	if env != nil {
		c.Env = env
	}
	b, err := c.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("%s failed: %s", name, strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("%s failed: %w", name, err)
	}
	return strings.TrimSpace(string(b)), nil
}
func roleEnvironment(role string) ([]string, error) {
	var expected, file string
	switch role {
	case "author":
		expected = os.Getenv("GHSHIP_AUTHOR")
		file = os.Getenv("GHSHIP_AUTHOR_PAT_FILE")
	case "reviewer":
		expected = os.Getenv("GHSHIP_REVIEWER")
		file = os.Getenv("GHSHIP_REVIEWER_PAT_FILE")
	case "dispatcher":
		expected = os.Getenv("GHSHIP_DISPATCHER")
	default:
		return nil, usage("unknown role")
	}
	env := cleanGHEnv()
	host := "github.com"
	if expected == "" {
		root, e := output(nil, "git", "rev-parse", "--show-toplevel")
		if e != nil {
			return nil, e
		}
		root, e = filepath.EvalSymlinks(root)
		if e != nil {
			return nil, e
		}
		binding, e := readBinding(root)
		if e != nil {
			return nil, errors.New("project role binding unavailable; run ghship init")
		}
		origin, e := output(nil, "git", "remote", "get-url", "origin")
		if e != nil {
			return nil, e
		}
		repo, e := parseOrigin(origin)
		if e != nil {
			return nil, e
		}
		if repo != binding.Repo {
			return nil, errors.New("origin differs from project binding")
		}
		host = strings.Split(binding.Repo, "/")[0]
		switch role {
		case "author":
			expected = binding.Author
		case "reviewer":
			expected = binding.Reviewer
		case "dispatcher":
			expected = binding.Admin
		}
		if role != "dispatcher" {
			api, e := accountAPI(host, expected)
			if e != nil {
				return nil, e
			}
			env = append(env, githubTokenEnv(host)+"="+api.token)
		}
	} else if role != "dispatcher" && file == "" {
		return nil, errors.New("explicit role requires external token-file environment setting")
	}
	env = append(env, "GH_HOST="+host)
	if file != "" {
		root, e := output(nil, "git", "rev-parse", "--show-toplevel")
		if e != nil {
			return nil, e
		}
		root, e = filepath.EvalSymlinks(root)
		if e != nil {
			return nil, e
		}
		file, e = filepath.EvalSymlinks(file)
		if e != nil {
			return nil, errors.New("credential unavailable")
		}
		file, e = filepath.Abs(file)
		if e != nil {
			return nil, e
		}
		rel, e := filepath.Rel(root, file)
		if e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, errors.New("credential must be outside repository")
		}
		st, e := os.Stat(file)
		if e != nil || !st.Mode().IsRegular() {
			return nil, errors.New("credential must be regular file")
		}
		b, e := os.ReadFile(file)
		if e != nil {
			return nil, errors.New("credential unreadable")
		}
		token := strings.TrimSpace(string(b))
		if token == "" {
			return nil, errors.New("empty credential")
		}
		env = append(env, githubTokenEnv(host)+"="+token)
	}
	actor, err := output(env, "gh", "api", "user", "--hostname", host, "--jq", ".login")
	if err != nil {
		return nil, err
	}
	if actor != expected {
		return nil, fmt.Errorf("wrong identity: expected %s, got %s", expected, actor)
	}
	return env, nil
}
func github(a []string, in io.Reader, out io.Writer) error {
	if len(a) < 3 || a[1] != "--" {
		return usage("use ghship gh ROLE -- GH_ARGUMENTS...")
	}
	env, err := roleEnvironment(a[0])
	if err != nil {
		return err
	}
	c := exec.CommandContext(operationContext, "gh", a[2:]...)
	c.Env = env
	c.Stdin = in
	c.Stdout = out
	c.Stderr = os.Stderr
	if e := c.Run(); e != nil {
		var ee *exec.ExitError
		if errors.As(e, &ee) {
			return cliError{ee.ExitCode(), errors.New("gh command failed")}
		}
		return e
	}
	return nil
}

type issue struct {
	Number    int `json:"number"`
	Assignees *[]struct {
		Login string `json:"login"`
	} `json:"assignees"`
	BlockedBy *struct {
		TotalCount *int               `json:"totalCount"`
		Nodes      *[]json.RawMessage `json:"nodes"`
	} `json:"blockedBy"`
	SubIssuesSummary *struct {
		Total *int `json:"total"`
	} `json:"subIssuesSummary"`
}

func selectIssue(items []issue, author string) (int, string, error) {
	if len(items) >= 1000 {
		return 0, "", errors.New("possible query truncation; paginate before selecting")
	}
	return selectCompleteIssues(items, author)
}
func selectCompleteIssues(items []issue, author string) (int, string, error) {
	owned, ready := []int{}, []int{}
	for _, x := range items {
		if x.Number <= 0 || x.Assignees == nil || x.BlockedBy == nil || x.SubIssuesSummary == nil || x.BlockedBy.TotalCount == nil || x.BlockedBy.Nodes == nil || x.SubIssuesSummary.Total == nil {
			return 0, "", errors.New("missing native Issue fields")
		}
		if *x.BlockedBy.TotalCount != len(*x.BlockedBy.Nodes) {
			return 0, "", errors.New("incomplete blocker query")
		}
		if *x.SubIssuesSummary.Total < 0 || *x.BlockedBy.TotalCount < 0 {
			return 0, "", errors.New("invalid native counts")
		}
		if *x.SubIssuesSummary.Total != 0 {
			continue
		}
		for _, p := range *x.Assignees {
			if p.Login == author {
				if *x.BlockedBy.TotalCount != 0 {
					return 0, "", errors.New("assigned slice blocked; preserve and park it before claiming dependency repair")
				}
				owned = append(owned, x.Number)
			}
		}
		if len(*x.Assignees) == 0 && *x.BlockedBy.TotalCount == 0 {
			ready = append(ready, x.Number)
		}
	}
	if len(owned) > 1 {
		return 0, "", errors.New("multiple assigned slices; resolve active work")
	}
	if len(owned) == 1 {
		return owned[0], "resume", nil
	}
	if len(ready) == 0 {
		return 0, "", cliError{3, errors.New("no eligible slice; inspect dependencies, ownership and product acceptance")}
	}
	sort.Ints(ready)
	return ready[0], "claim", nil
}
func next(a []string, in io.Reader, out io.Writer) error {
	author := ""
	asJSON := false
	milestone := 0
	for i := 0; i < len(a); i++ {
		switch a[i] {
		case "--author":
			i++
			if i == len(a) {
				return usage("--author requires LOGIN")
			}
			author = a[i]
		case "--json":
			asJSON = true
		case "--milestone":
			i++
			if i == len(a) {
				return usage("--milestone requires a positive number")
			}
			var err error
			milestone, err = strconv.Atoi(a[i])
			if err != nil || milestone <= 0 {
				return usage("--milestone must be positive")
			}
		default:
			return usage("unknown next option")
		}
	}
	if milestone != 0 {
		items, login, err := milestoneIssues(milestone, author)
		if err != nil {
			return err
		}
		n, action, err := selectCompleteIssues(items, login)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(out).Encode(map[string]any{"issue": n, "action": action, "query": map[string]any{"milestone": milestone, "complete": true}})
		}
		_, err = fmt.Fprintln(out, n)
		return err
	}
	if author == "" {
		return usage("next requires --author LOGIN")
	}
	var items []issue
	d := json.NewDecoder(in)
	if err := d.Decode(&items); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("expected one JSON array")
	}
	if items == nil {
		return errors.New("expected JSON array, not null")
	}
	n, action, err := selectIssue(items, author)
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(out).Encode(map[string]any{"issue": n, "action": action})
	}
	fmt.Fprintln(out, n)
	return nil
}
func tidy(a []string, out io.Writer) error {
	if len(a) < 1 || len(a) > 2 {
		return usage("use ghship tidy PR [--json]")
	}
	n, err := strconv.Atoi(a[0])
	if err != nil || n <= 0 {
		return usage("PR must be positive integer")
	}
	asJSON := len(a) == 2
	if asJSON && a[1] != "--json" {
		return usage("unknown tidy option")
	}
	git := func(a ...string) (string, error) { return output(nil, "git", a...) }
	dirty, err := git("status", "--porcelain")
	if err != nil {
		return err
	}
	if dirty != "" {
		return errors.New("dirty workspace; preserve local work")
	}
	data, err := output(nil, "gh", "pr", "view", a[0], "--json", "state,baseRefName,headRefName,headRefOid,mergeCommit,isCrossRepository")
	if err != nil {
		return err
	}
	var pr struct {
		State, BaseRefName, HeadRefName, HeadRefOid string
		MergeCommit                                 struct{ OID string }
		IsCrossRepository                           bool
	}
	if err = json.Unmarshal([]byte(data), &pr); err != nil {
		return err
	}
	if pr.State != "MERGED" || pr.IsCrossRepository {
		return errors.New("requires merged same-repository PR")
	}
	data, err = output(nil, "gh", "repo", "view", "--json", "url,defaultBranchRef")
	if err != nil {
		return err
	}
	var repo struct {
		URL              string
		DefaultBranchRef struct{ Name string }
	}
	if err = json.Unmarshal([]byte(data), &repo); err != nil {
		return err
	}
	base, branch := repo.DefaultBranchRef.Name, pr.HeadRefName
	if base == "" || base != pr.BaseRefName || branch == "" || branch == base || pr.MergeCommit.OID == "" || pr.HeadRefOid == "" {
		return errors.New("unexpected PR branches or missing SHAs")
	}
	origin, err := git("remote", "get-url", "origin")
	if err != nil {
		return err
	}
	canonical := strings.TrimSuffix(repo.URL, ".git")
	ssh := ""
	// HTTPS and standard SCP SSH remotes identify the same GitHub repository.
	rest := strings.TrimPrefix(canonical, "https://")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		return errors.New("unsupported repository URL")
	}
	ssh = "git@" + parts[0] + ":" + parts[1] + ".git"
	if origin != canonical && origin != canonical+".git" && origin != ssh {
		return errors.New("origin differs from GitHub repository")
	}
	if _, err = git("fetch", "origin", base); err != nil {
		return err
	}
	if _, err = git("merge-base", "--is-ancestor", pr.MergeCommit.OID, "origin/"+base); err != nil {
		return errors.New("remote default branch lacks merge commit")
	}
	ref := "refs/heads/" + branch
	present, err := localHead(ref)
	if err != nil {
		return err
	}
	if present != "" && present != pr.HeadRefOid {
		return errors.New("local branch has additional work; preserved")
	}
	if _, err = git("checkout", base); err != nil {
		return err
	}
	if _, err = git("merge", "--ff-only", "origin/"+base); err != nil {
		return err
	}
	head, err := git("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	remote, err := git("ls-remote", "--exit-code", "origin", "refs/heads/"+base)
	if err != nil {
		return err
	}
	fields := strings.Fields(remote)
	if len(fields) != 2 || fields[0] != head {
		return errors.New("default branch advanced; retry")
	}
	// Recheck immediately before deletion: preserve unexpected local updates.
	present, err = localHead(ref)
	if err != nil {
		return err
	}
	if present != "" {
		if present != pr.HeadRefOid {
			return errors.New("local branch changed; preserved")
		}
		if _, err = git("branch", "-D", "--", branch); err != nil {
			return err
		}
	}
	if asJSON {
		return json.NewEncoder(out).Encode(map[string]any{"pr": n, "branch": base, "mainSHA": head, "cleaned": true})
	}
	fmt.Fprintln(out, "Local default branch synchronized; merged local branch cleaned")
	return nil
}
func localHead(ref string) (string, error) {
	c := exec.Command("git", "show-ref", "--verify", "--quiet", ref)
	if e := c.Run(); e != nil {
		var ee *exec.ExitError
		if errors.As(e, &ee) && ee.ExitCode() == 1 {
			return "", nil
		}
		return "", e
	}
	return output(nil, "git", "rev-parse", ref)
}
