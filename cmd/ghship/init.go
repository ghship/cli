package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/term"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const initHelp = `ghship init [--project PATH] [--repo HOST/OWNER/REPO]
  [--author LOGIN] [--reviewer LOGIN] [--admin LOGIN]
  [--configure] [--non-interactive] [--json]
  [--confirm-auto-close] [--acceptance-pr NUMBER] [--remove-binding]
Binds one physical project directory to verified GitHub identities outside Git.
Uses Git/gh. Admin is the expected local gh login, ambient token variables removed.
--configure explicitly authorizes collaborator invitations/acceptance (write minimum)
and enabling native branch deletion in this repository. It does not alter rulesets.
--confirm-auto-close records your UI verification of the native auto-close setting.
--acceptance-pr identifies a merged, independently approved integration PR with
successful actual CI; static workflow presence alone cannot prove readiness.
Missing governance returns 4 with next steps. No rules bypass, fake CI or task database.
Accounts: user add first, or enter them in interactive init. Tokens remain external.
--remove-binding can remove a missing directory's saved binding by its original path.
Exit: 0 ready/removed, 1 operation failed, 2 missing/invalid input, 4 setup pending.
Example:
 ghship init --project /work/app --repo github.com/team/app --author dev \
   --reviewer acceptor --admin owner --configure --non-interactive --json
`

type projectBinding struct {
	SchemaVersion      int    `json:"schemaVersion"`
	Root               string `json:"root"`
	Repo               string `json:"repo"`
	Author             string `json:"author"`
	Reviewer           string `json:"reviewer"`
	Admin              string `json:"admin"`
	AutoCloseConfirmed bool   `json:"autoCloseConfirmed"`
	AcceptancePR       int    `json:"acceptancePR,omitempty"`
}

func bindingPath(root string) (string, error) {
	base, e := configRoot()
	return filepath.Join(base, "projects", digest([]byte(root))+".json"), e
}
func readBinding(root string) (projectBinding, error) {
	var p projectBinding
	name, e := bindingPath(root)
	if e != nil {
		return p, e
	}
	if e = noLinks(name); e != nil {
		return p, e
	}
	b, e := os.ReadFile(name)
	if e != nil {
		return p, e
	}
	if e = json.Unmarshal(b, &p); e != nil {
		return p, e
	}
	if p.SchemaVersion != 1 || p.Root != root || len(strings.Split(p.Repo, "/")) != 3 {
		return p, errors.New("invalid project binding")
	}
	return p, nil
}
func accountReferences(host, user string) ([]string, error) {
	result := []string{}
	base, e := configRoot()
	if e != nil {
		return nil, e
	}
	dir := filepath.Join(base, "projects")
	if e = noLinks(dir); e != nil {
		return nil, e
	}
	entries, e := os.ReadDir(dir)
	if os.IsNotExist(e) {
		return result, nil
	}
	if e != nil {
		return nil, e
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if e = noLinks(path); e != nil {
			return nil, e
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		var p projectBinding
		if e = json.Unmarshal(b, &p); e != nil {
			return nil, errors.New("invalid project binding; preserved")
		}
		if strings.HasPrefix(p.Repo, host+"/") && (p.Author == user || p.Reviewer == user) {
			result = append(result, p.Root)
		}
	}
	return result, nil
}
func gitAt(root string, args ...string) (string, error) {
	return output(nil, "git", append([]string{"-C", root}, args...)...)
}
func parseOrigin(raw string) (string, error) {
	s := strings.TrimSuffix(strings.TrimSpace(raw), ".git")
	if strings.HasPrefix(s, "https://") {
		s = strings.TrimPrefix(s, "https://")
	} else if strings.HasPrefix(s, "git@") {
		s = strings.Replace(strings.TrimPrefix(s, "git@"), ":", "/", 1)
	} else {
		return "", errors.New("origin must be GitHub HTTPS or git@host SSH URL")
	}
	parts := strings.Split(s, "/")
	if len(parts) != 3 || !validHost.MatchString(parts[0]) || parts[1] == "" || parts[2] == "" || strings.ContainsAny(s, "?#@") {
		return "", errors.New("invalid repository origin")
	}
	return strings.ToLower(s), nil
}

type invitation struct {
	ID         int `json:"id"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Invitee struct {
		Login string `json:"login"`
	} `json:"invitee"`
}

func permission(api githubAPI, repo, user string) (string, error) {
	var p struct {
		Permission string `json:"permission"`
	}
	_, e := api.call("GET", "repos/"+repo+"/collaborators/"+escaped(user)+"/permission", nil, &p)
	return p.Permission, e
}
func writable(p string) bool { return p == "write" || p == "maintain" || p == "admin" }
func ensureCollaborator(admin, member githubAPI, repo, user string, configure bool) error {
	p, e := permission(admin, repo, user)
	if e == nil && writable(p) {
		return nil
	}
	if e != nil && !strings.Contains(e.Error(), "HTTP 404") {
		return e
	}
	if !configure {
		return fmt.Errorf("%s needs write permission; rerun init --configure", user)
	}
	if e = member.identity(user); e != nil {
		return e
	}
	pending := 0
	invites, e := listREST[invitation](admin, "repos/"+repo+"/invitations?per_page=100")
	if e != nil {
		return e
	}
	for _, v := range invites {
		if strings.EqualFold(v.Invitee.Login, user) {
			pending = v.ID
		}
	}
	if pending == 0 {
		var v invitation
		_, e = admin.call("PUT", "repos/"+repo+"/collaborators/"+escaped(user), map[string]string{"permission": "push"}, &v)
		if e != nil {
			return e
		}
		pending = v.ID
	}
	p, e = permission(admin, repo, user)
	if e == nil && writable(p) {
		return nil
	}
	invites, e = listREST[invitation](member, "user/repository_invitations?per_page=100")
	if e != nil {
		return e
	}
	for _, v := range invites {
		if strings.EqualFold(v.Repository.FullName, repo) && strings.EqualFold(v.Invitee.Login, user) && (pending == 0 || pending == v.ID) {
			_, e = member.call("PATCH", fmt.Sprintf("user/repository_invitations/%d", v.ID), nil, nil)
			if e != nil {
				return e
			}
			break
		}
	}
	p, e = permission(admin, repo, user)
	if e != nil {
		return e
	}
	if !writable(p) {
		return fmt.Errorf("%s invitation/access is still pending; rerun init", user)
	}
	return nil
}

func hasCodeowner(content []byte, login string) bool {
	for _, line := range strings.Split(string(content), "\n") {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		for _, owner := range fields[1:] {
			if strings.EqualFold(owner, "@"+login) {
				return true
			}
		}
	}
	return false
}
func initialize(args []string, in io.Reader, out io.Writer) error {
	f := flag.NewFlagSet("init", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	path := f.String("project", ".", "project path")
	repoFlag := f.String("repo", "", "host/owner/repo")
	author := f.String("author", "", "developer username")
	reviewer := f.String("reviewer", "", "acceptance username")
	adminUser := f.String("admin", "", "expected local gh login")
	configure := f.Bool("configure", false, "configure members and deletion")
	non := f.Bool("non-interactive", false, "no prompts")
	j := f.Bool("json", false, "JSON result")
	confirm := f.Bool("confirm-auto-close", false, "user UI confirmation")
	acceptance := f.Int("acceptance-pr", 0, "integration PR")
	remove := f.Bool("remove-binding", false, "remove only project binding")
	if e := f.Parse(args); e != nil || f.NArg() != 0 {
		return usage("invalid init arguments; use ghship help init")
	}
	root, e := filepath.Abs(*path)
	if e != nil {
		return e
	}
	if real, e := filepath.EvalSymlinks(root); e == nil {
		root = real
	}
	if *remove {
		if e = validateBindingRemoval(root); e != nil {
			return e
		}
		name, e := bindingPath(root)
		if e != nil {
			return e
		}
		if e = noLinks(name); e != nil {
			return e
		}
		if e = os.Remove(name); e != nil && !os.IsNotExist(e) {
			return e
		}
		return emitResult(out, *j, map[string]any{"removedBinding": root})
	}
	pending := func(missing []string, next []string, code int) error {
		if e := emitResult(out, *j, map[string]any{"project": root, "ready": false, "completed": []string{}, "missing": missing, "next": next}); e != nil {
			return e
		}
		return cliError{code, errors.New("project setup pending; see missing and next")}
	}
	for _, tool := range []string{"git", "gh"} {
		if _, e = exec.LookPath(tool); e != nil {
			return pending([]string{tool}, []string{"Install " + tool + " (Git: https://git-scm.com/downloads; gh: https://cli.github.com/), then rerun ghship init"}, 4)
		}
	}
	if err := doctor([]string{"--scope", "project"}, io.Discard); err != nil {
		return pending([]string{"Git/gh prerequisite or native feature unavailable"}, []string{"ghship doctor --scope project --json; install or upgrade missing tools, then rerun init"}, 4)
	}
	top, e := gitAt(root, "rev-parse", "--show-toplevel")
	if e != nil {
		return pending([]string{"Git project"}, []string{"ghship init --project /path/to/project"}, 4)
	}
	root, e = filepath.EvalSymlinks(top)
	if e != nil {
		return e
	}
	origin, e := gitAt(root, "remote", "get-url", "origin")
	if e != nil {
		return pending([]string{"origin remote"}, []string{"Set the intended GitHub origin, then rerun init"}, 4)
	}
	repo, e := parseOrigin(origin)
	if e != nil {
		return e
	}
	if *repoFlag != "" && !strings.EqualFold(*repoFlag, repo) {
		return errors.New("requested repository does not match physical project origin")
	}
	parts := strings.Split(repo, "/")
	host, full := parts[0], strings.Join(parts[1:], "/")
	old, oldErr := readBinding(root)
	if oldErr == nil {
		if old.Repo != repo {
			return errors.New("saved project repository differs from origin; remove binding explicitly")
		}
		if *author == "" {
			*author = old.Author
		}
		if *reviewer == "" {
			*reviewer = old.Reviewer
		}
		if *adminUser == "" {
			*adminUser = old.Admin
		}
		if old.AutoCloseConfirmed {
			*confirm = true
		}
		if *acceptance == 0 {
			*acceptance = old.AcceptancePR
		}
	} else if !os.IsNotExist(oldErr) {
		return oldErr
	}
	if !*non && term.IsTerminal(int(os.Stdin.Fd())) {
		reader := bufio.NewReader(in)
		ask := func(label string, value *string) error {
			if *value != "" {
				return nil
			}
			fmt.Fprint(os.Stderr, label+": ")
			s, e := reader.ReadString('\n')
			*value = strings.TrimSpace(s)
			return e
		}
		for _, v := range []struct {
			label string
			value *string
		}{{"Developer GitHub username", author}, {"Acceptance GitHub username", reviewer}, {"Expected administrator GitHub username", adminUser}} {
			if e = ask(v.label, v.value); e != nil {
				return pending([]string{"input cancelled"}, []string{"ghship init"}, 4)
			}
		}
		if !*configure {
			var choice string
			if e = ask("Configure collaborators and branch deletion for "+repo+"? [yes/no]", &choice); e != nil {
				return pending([]string{"input cancelled"}, []string{"ghship init"}, 4)
			}
			*configure = choice == "yes"
		}
		for _, user := range []string{*author, *reviewer} {
			if _, e = readAccount(host, user); e != nil {
				if e = accountMutation([]string{"set", "--host", host, "--user", user}, in, io.Discard); e != nil {
					return e
				}
			}
		}
	}
	missing := []string{}
	for _, v := range []struct{ name, value string }{{"author", *author}, {"reviewer", *reviewer}, {"admin", *adminUser}} {
		if v.value == "" {
			missing = append(missing, v.name)
		} else if !validLogin.MatchString(v.value) {
			return usage("invalid " + v.name + " username")
		}
	}
	if len(missing) > 0 {
		return pending(missing, []string{"Provide --author, --reviewer and --admin; use user add first"}, 2)
	}
	*author = strings.ToLower(*author)
	*reviewer = strings.ToLower(*reviewer)
	if *author == *reviewer {
		return usage("developer and acceptance usernames must differ")
	}
	if err := validateProjectSkill(); err != nil {
		return pending([]string{"User-level skill installation"}, []string{"ghship help install: install the home skill using your distribution channel with --user-only"}, 4)
	}
	if err := installProjectFiles(root, false); err != nil {
		return err
	}
	// gh auth token is captured only in memory, never included in output/errors.
	token, e := output(cleanGHEnv(), "gh", "auth", "token", "--hostname", host)
	if e != nil {
		return errors.New("administrator gh login unavailable; gh auth login --hostname " + host)
	}
	admin := githubAPI{host, token}
	if e = admin.identity(*adminUser); e != nil {
		return e
	}
	var meta struct {
		FullName       string `json:"full_name"`
		DefaultBranch  string `json:"default_branch"`
		HasIssues      bool   `json:"has_issues"`
		DeleteBranches bool   `json:"delete_branch_on_merge"`
		Permissions    struct {
			Admin bool `json:"admin"`
		} `json:"permissions"`
	}
	_, e = admin.call("GET", "repos/"+full, nil, &meta)
	if e != nil {
		return e
	}
	if !strings.EqualFold(meta.FullName, full) || meta.DefaultBranch == "" {
		return errors.New("repository metadata mismatch")
	}
	if *configure && !meta.Permissions.Admin {
		return errors.New("repository administrator permission is required to configure")
	}
	for _, user := range []string{*author, *reviewer} {
		member, e := accountAPI(host, user)
		if e != nil {
			return e
		}
		if e = member.identity(user); e != nil {
			return e
		}
		if e = ensureCollaborator(admin, member, full, user, *configure); e != nil {
			return e
		}
	}
	if !meta.DeleteBranches && *configure {
		_, e = admin.call("PATCH", "repos/"+full, map[string]bool{"delete_branch_on_merge": true}, nil)
		if e != nil {
			return e
		}
		_, e = admin.call("GET", "repos/"+full, nil, &meta)
		if e != nil {
			return e
		}
	}
	binding := projectBinding{1, root, repo, *author, *reviewer, *adminUser, *confirm, *acceptance}
	b, _ := json.MarshalIndent(binding, "", "  ")
	name, e := bindingPath(root)
	if e != nil {
		return e
	}
	if e = atomicConfig(name, b); e != nil {
		return e
	}
	missing = []string{}
	next := []string{}
	codeowners, ownerErr := os.ReadFile(filepath.Join(root, ".github", "CODEOWNERS"))
	if ownerErr != nil || !hasCodeowner(codeowners, *reviewer) {
		missing = append(missing, "Project CODEOWNERS must include independent acceptance owner")
		next = append(next, "Integration PR: review existing .github/CODEOWNERS coverage and configure selected acceptance user, for example: * @"+*reviewer)
	}
	workflows, workflowErr := os.ReadDir(filepath.Join(root, ".github", "workflows"))
	hasWorkflow := false
	for _, w := range workflows {
		if !w.IsDir() && (strings.HasSuffix(w.Name(), ".yml") || strings.HasSuffix(w.Name(), ".yaml")) {
			hasWorkflow = true
		}
	}
	if workflowErr != nil || !hasWorkflow {
		missing = append(missing, "Actual project CI workflow")
	}
	if !meta.HasIssues {
		missing = append(missing, "Repository Issues disabled")
	}
	if !meta.DeleteBranches {
		missing = append(missing, "Native automatic branch deletion")
	}
	if !*confirm {
		missing = append(missing, "Native Issue auto-close UI confirmation")
		next = append(next, "Enable Auto-close issues with merged linked pull requests in https://"+repo+"/settings; rerun init --confirm-auto-close")
	}
	type branchRule struct {
		Type       string `json:"type"`
		Parameters struct {
			RequiredApprovals int  `json:"required_approving_review_count"`
			CodeOwners        bool `json:"require_code_owner_review"`
			LastPush          bool `json:"require_last_push_approval"`
			StaleReviews      bool `json:"dismiss_stale_reviews_on_push"`
			ResolveThreads    bool `json:"required_review_thread_resolution"`
			Strict            bool `json:"strict_required_status_checks_policy"`
			Checks            []struct {
				Context string `json:"context"`
			} `json:"required_status_checks"`
		} `json:"parameters"`
	}
	rules, e := listREST[branchRule](admin, "repos/"+full+"/rules/branches/"+escaped(meta.DefaultBranch)+"?per_page=100")
	if e != nil {
		return e
	}
	protected, checks := false, false
	requiredContexts := []string{}
	forbidDelete, forbidForce := false, false
	for _, rule := range rules {
		if rule.Type == "pull_request" && rule.Parameters.RequiredApprovals >= 1 && rule.Parameters.CodeOwners && rule.Parameters.LastPush && rule.Parameters.StaleReviews && rule.Parameters.ResolveThreads {
			protected = true
		}
		if rule.Type == "required_status_checks" && rule.Parameters.Strict && len(rule.Parameters.Checks) > 0 {
			checks = true
			for _, c := range rule.Parameters.Checks {
				if c.Context != "" {
					requiredContexts = append(requiredContexts, c.Context)
				}
			}
		}
		if rule.Type == "deletion" {
			forbidDelete = true
		}
		if rule.Type == "non_fast_forward" {
			forbidForce = true
		}
	}
	if !forbidDelete || !forbidForce {
		missing = append(missing, "Default branch deletion/force-push protection")
	}
	if !protected {
		missing = append(missing, "Default branch independent latest-push/code-owner approval, stale dismissal and discussion resolution")
	}
	if !checks {
		missing = append(missing, "Actual strict required CI checks")
	}
	if *acceptance <= 0 {
		missing = append(missing, "Independently approved, merged project integration PR")
		next = append(next, "Agent: design real project CI and CODEOWNERS in an integration PR; independently review, merge, configure applicable rules, rerun init --acceptance-pr N")
	} else {
		if e = verifyIntegration(admin, full, *acceptance, *reviewer, meta.DefaultBranch, requiredContexts); e != nil {
			missing = append(missing, e.Error())
		}
	}
	if len(missing) > 0 {
		next = append(next, "ghship init --project "+root+" --non-interactive --json")
	}
	result := map[string]any{"project": root, "repo": repo, "ready": len(missing) == 0, "completed": []string{"Verified role identities", "Repository access", "Project binding"}, "missing": missing, "next": next}
	if e = emitResult(out, *j, result); e != nil {
		return e
	}
	if len(missing) > 0 {
		return cliError{4, errors.New("project setup pending; see missing and next")}
	}
	return nil
}
func verifyIntegration(api githubAPI, repo string, n int, reviewer, defaultBranch string, requiredContexts []string) error {
	var p struct {
		Merged bool `json:"merged"`
		Base   struct {
			Ref  string `json:"ref"`
			Repo struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
		User struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	_, e := api.call("GET", fmt.Sprintf("repos/%s/pulls/%d", repo, n), nil, &p)
	if e != nil {
		return e
	}
	if !p.Merged || p.Head.SHA == "" || p.Base.Ref != defaultBranch || !strings.EqualFold(p.Base.Repo.FullName, repo) {
		return errors.New("integration PR is not merged into this repository default branch")
	}
	type integrationReview struct {
		State    string `json:"state"`
		CommitID string `json:"commit_id"`
		User     struct {
			Login string `json:"login"`
		} `json:"user"`
	}
	reviews, e := listREST[integrationReview](api, fmt.Sprintf("repos/%s/pulls/%d/reviews?per_page=100", repo, n))
	if e != nil {
		return e
	}
	approved := false
	for _, r := range reviews {
		if strings.EqualFold(r.User.Login, reviewer) && !strings.EqualFold(p.User.Login, reviewer) && r.CommitID == p.Head.SHA && (r.State == "APPROVED" || r.State == "CHANGES_REQUESTED") {
			approved = r.State == "APPROVED"
		}
	}
	if !approved {
		return errors.New("integration PR lacks latest-head independent approval")
	}
	type integrationCheck struct {
		Name       string `json:"name"`
		Conclusion string `json:"conclusion"`
	}
	checks, e := restPages(api, "repos/"+repo+"/commits/"+p.Head.SHA+"/check-runs?per_page=100", func(raw json.RawMessage) ([]integrationCheck, *int, error) {
		var page struct {
			Total *int               `json:"total_count"`
			Runs  []integrationCheck `json:"check_runs"`
		}
		e := json.Unmarshal(raw, &page)
		if page.Total == nil {
			return nil, nil, errors.New("missing check count")
		}
		return page.Runs, page.Total, e
	})
	if e != nil {
		return e
	}
	if len(checks) == 0 {
		return errors.New("integration CI absent")
	}
	if len(requiredContexts) == 0 {
		return errors.New("required CI contexts are unavailable")
	}
	successful := map[string]bool{}
	for _, r := range checks {
		if r.Name != "" && r.Conclusion == "success" {
			successful[r.Name] = true
		}
	}
	for _, name := range requiredContexts {
		if !successful[name] {
			return fmt.Errorf("required integration CI %s is absent or not successful", name)
		}
	}

	return nil
}
