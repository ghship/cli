package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	assets "github.com/ghship/cli"
)

const projectHelp = `ghship project install|status|update|uninstall|skill [OPTIONS]
install: same options as init; matching, unmodified managed home skill is required. Adds project instructions,
managed AGENTS reference and local role binding. Governance changes require a PR.
status: --project PATH --json. Reports local installation facts, not invented readiness.
update: --project PATH --json. Requires matching, unmodified managed home skill;
updates only owned, unchanged installation content.
uninstall: --project PATH [--binding-only] --json. Removes owned project instructions
and binding, preserves home skill, users, customization, GitHub history and rules.
Unknown files or edited managed content are preserved, with a conflict error.
skill: --project PATH --show | --create [--mode supplement|override]
       [--overrides testing,acceptance] [--json]
Project customization: .ghship/skill.md, owned by the user, always preserved.
A default supplement augments global workflow. override replaces only explicitly
listed planning/development/testing/acceptance/delivery areas; CLI and platform
permissions cannot be rewritten by prose. Unknown/ambiguous formats fail.
Local project file changes must be reviewed/committed; exit 0 never means remote merge.
Examples:
 ghship project status --project /work/app --json
 ghship project skill --create --mode override --overrides testing,acceptance
 ghship project update --project /work/app
 ghship project uninstall --project /work/app
`
const agentStart = "<!-- ghship:begin -->"
const agentEnd = "<!-- ghship:end -->"

func agentBlock() string {
	return agentStart + "\n## ghship project integration\n\nUse the ghship skill at the current user's home default `.agents/skills/ghship/SKILL.md`,\nincluding its `references/pipeline.md`, `references/workflow.md` and `references/cli.md`.\nRead this repository's `.ghship/skill.md` if present, then `.ghship/README.md`.\nApply current user instructions and applicable AGENTS constraints first; project\nsupplement/explicit scoped overrides then global defaults. Never invent results.\nCLI help and actual platform permissions remain authoritative software facts.\nKeep project rules outside these markers or in `.ghship/skill.md`; editing inside\nthem makes `ghship project update` preserve the file and report a conflict.\n" + agentEnd
}
func projectReadme() string {
	return "# ghship project integration\n\nIntegration template version: " + version + "\n\nShared skill: `~/.agents/skills/ghship/SKILL.md`. Each user's own home applies.\nProject customization: `.ghship/skill.md` (user-owned; preserved by update/uninstall).\nRun `ghship project skill --show --json` to inspect its declared scope.\nRun `ghship init --json` for actual repository readiness and missing steps.\nRun `ghship user list --json` for token status; valid does not prove repository access.\n\nProject role bindings and credentials are stored outside Git in user configuration.\nWorkflow/CI/CODEOWNERS changes require a real reviewed integration PR.\nInstallation alone does not prove tests pass, native governance is configured,\nor the product is delivered. Use actual CI and independent review evidence.\n\nMaintenance: `ghship project update`, `ghship project uninstall`.\nThese create local changes requiring review/commit; remote rules/history stay.\n"
}

type projectManifest struct {
	SchemaVersion int    `json:"schemaVersion"`
	Version       string `json:"version"`
	ReadmeHash    string `json:"readmeHash"`
	BlockHash     string `json:"blockHash"`
	CreatedAgents bool   `json:"createdAgents"`
}

func projectRoot(path string) (string, error) {
	root, e := gitAt(path, "rev-parse", "--show-toplevel")
	if e != nil {
		return "", e
	}
	return filepath.EvalSymlinks(root)
}
func loadProjectManifest(root string) (projectManifest, error) {
	var m projectManifest
	p := filepath.Join(root, ".ghship", "install.json")
	if e := noLinks(p); e != nil {
		return m, e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return m, e
	}
	if e = json.Unmarshal(b, &m); e != nil {
		return m, errors.New("invalid project installation manifest")
	}
	if m.SchemaVersion != 1 || len(m.ReadmeHash) != 64 || len(m.BlockHash) != 64 {
		return m, errors.New("invalid project installation ownership")
	}
	return m, nil
}

// A project entry may only point at the exact installed public skill shipped
// with this CLI. Unknown, incomplete and modified installations are preserved.
func validateProjectSkill() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".agents", "skills", "ghship")
	if err = noLinks(filepath.Join(dir, ".ghship.json")); err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(dir, ".ghship.json"))
	if err != nil {
		return errors.New("GHShip skill unavailable; install the matching home skill first")
	}
	var m manifest
	if json.Unmarshal(b, &m) != nil || m.Version != version {
		return errors.New("GHShip skill manifest or version invalid; preserved")
	}
	if err = validateManaged(dir, m); err != nil {
		return fmt.Errorf("GHShip skill invalid; preserved: %w", err)
	}
	count := 0
	err = fs.WalkDir(assets.Skill, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		content, err := assets.Skill.ReadFile(name)
		if err != nil {
			return err
		}
		if m.Files[name] != digest(content) {
			return errors.New("GHShip skill does not match this CLI; preserved")
		}
		count++
		return nil
	})
	if err != nil {
		return err
	}
	if count != len(m.Files) {
		return errors.New("GHShip skill contents are incomplete or unknown; preserved")
	}
	return nil
}

func validateBindingRemoval(root string) error {
	_, err := loadProjectManifest(root)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func extractBlock(content string) (string, int, int, error) {
	a, b := strings.Index(content, agentStart), strings.Index(content, agentEnd)
	if a < 0 && b < 0 {
		return "", -1, -1, nil
	}
	if a < 0 || b < a || strings.Count(content, agentStart) != 1 || strings.Count(content, agentEnd) != 1 {
		return "", 0, 0, errors.New("ambiguous ghship AGENTS markers; preserved")
	}
	end := b + len(agentEnd)
	return content[a:end], a, end, nil
}
func validateProjectFiles(root string, m projectManifest) (string, error) {
	for _, rel := range []string{"AGENTS.md", ".ghship/README.md", ".ghship/install.json"} {
		if e := noLinks(filepath.Join(root, filepath.FromSlash(rel))); e != nil {
			return "", e
		}
	}
	b, e := os.ReadFile(filepath.Join(root, ".ghship", "README.md"))
	if e != nil {
		return "", e
	}
	if digest(b) != m.ReadmeHash {
		return "", errors.New("project instructions changed; preserved")
	}
	b, e = os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if e != nil {
		return "", e
	}
	block, _, _, e := extractBlock(string(b))
	if e != nil {
		return "", e
	}
	if block == "" || digest([]byte(block)) != m.BlockHash {
		return "", errors.New("managed AGENTS block changed or missing; preserved")
	}
	return string(b), nil
}
func projectChanges(root string, contents map[string]*string) error {
	changes := []replacement{}
	stages := []string{}
	defer func() {
		for _, s := range stages {
			os.Remove(s)
		}
	}()
	for _, rel := range []string{"AGENTS.md", ".ghship/README.md", ".ghship/install.json"} {
		value, ok := contents[rel]
		if !ok {
			continue
		}
		dest := filepath.Join(root, filepath.FromSlash(rel))
		if e := noLinks(dest); e != nil {
			return e
		}
		st, e := os.Lstat(dest)
		exists := e == nil
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if exists && !st.Mode().IsRegular() {
			return errors.New("project target is not a regular file")
		}
		item := replacement{dest: dest, expected: &exists}
		if value != nil {
			if e = os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
				return e
			}
			f, e := os.CreateTemp(filepath.Dir(dest), ".ghship-stage-")
			if e != nil {
				return e
			}
			item.stage = f.Name()
			stages = append(stages, f.Name())
			_, e = f.WriteString(*value)
			ce := f.Close()
			if e != nil {
				return e
			}
			if ce != nil {
				return ce
			}
			if e = os.Chmod(item.stage, 0644); e != nil {
				return e
			}
		}
		changes = append(changes, item)
	}
	return commitReplacements(changes)
}
func installProjectFiles(root string, update bool) error {
	m, e := loadProjectManifest(root)
	var agents string
	created := false
	if e == nil {
		if update && !newerOrEqual(version, m.Version) {
			return errors.New("project installation version cannot be downgraded")
		}
		agents, e = validateProjectFiles(root, m)
		if e != nil {
			return e
		}
		created = m.CreatedAgents
		if !update {
			if m.Version != version {
				return errors.New("project integration has a different version; use ghship project update")
			}
			return nil
		}
		block, start, end, e := extractBlock(agents)
		if e != nil {
			return e
		}
		_ = block
		agents = agents[:start] + agentBlock() + agents[end:]
	} else if !os.IsNotExist(e) {
		return e
	} else {
		if update {
			return errors.New("project not installed; use ghship project install")
		}
		for _, rel := range []string{"AGENTS.md", ".ghship/README.md", ".ghship/install.json"} {
			if e = noLinks(filepath.Join(root, filepath.FromSlash(rel))); e != nil {
				return e
			}
		}
		if _, e = os.Lstat(filepath.Join(root, ".ghship", "README.md")); e == nil {
			return errors.New("project instructions exist without ownership; preserved")
		} else if !os.IsNotExist(e) {
			return e
		}
		b, e := os.ReadFile(filepath.Join(root, "AGENTS.md"))
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		created = os.IsNotExist(e)
		agents = string(b)
		block, _, _, e := extractBlock(agents)
		if e != nil {
			return e
		}
		if block != "" {
			return errors.New("unowned AGENTS reference; preserved")
		}
		if agents != "" && !strings.HasSuffix(agents, "\n") {
			agents += "\n"
		}
		agents += "\n" + agentBlock() + "\n"
	}
	if e = validateProjectSkill(); e != nil {
		return e
	}
	readme := projectReadme()
	m = projectManifest{1, version, digest([]byte(readme)), digest([]byte(agentBlock())), created}
	data, _ := json.MarshalIndent(m, "", "  ")
	manifestText := string(data) + "\n"
	return projectChanges(root, map[string]*string{"AGENTS.md": &agents, ".ghship/README.md": &readme, ".ghship/install.json": &manifestText})
}
func project(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return usage("project requires install, status, update, uninstall or skill")
	}
	if args[0] == "install" {
		return initialize(args[1:], in, out)
	}
	if args[0] == "skill" {
		return projectSkill(args[1:], out)
	}
	f := flag.NewFlagSet("project", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	path := f.String("project", ".", "project")
	j := f.Bool("json", false, "JSON")
	only := f.Bool("binding-only", false, "only remove local binding")
	if e := f.Parse(args[1:]); e != nil || f.NArg() != 0 {
		return usage("invalid project arguments")
	}
	if args[0] == "uninstall" && *only {
		return initialize([]string{"--project", *path, "--remove-binding", "--json"}, nil, out)
	}
	root, e := projectRoot(*path)
	if e != nil {
		return e
	}
	m, me := loadProjectManifest(root)
	switch args[0] {
	case "status":
		_, be := readBinding(root)
		custom, ce := readProjectSkill(root)
		var owned bool
		if me == nil {
			_, e = validateProjectFiles(root, m)
			owned = e == nil
		}
		result := map[string]any{"project": root, "installed": me == nil, "managedFilesUnmodified": owned, "bound": be == nil, "ready": nil, "readiness": "not_checked", "next": []string{"ghship init --project " + root + " --non-interactive --json"}}
		if ce == nil {
			result["customization"] = custom
		} else if !os.IsNotExist(ce) {
			return ce
		}
		return emitResult(out, *j, result)
	case "update":
		if e = installProjectFiles(root, true); e != nil {
			return e
		}
	case "uninstall":
		if me != nil {
			if os.IsNotExist(me) {
				return initialize([]string{"--project", root, "--remove-binding", "--json"}, nil, out)
			}
			return me
		}
		agents, e := validateProjectFiles(root, m)
		if e != nil {
			return e
		}
		_, start, end, e := extractBlock(agents)
		if e != nil {
			return e
		}
		agents = agents[:start] + agents[end:]
		var agentContent *string = &agents
		if m.CreatedAgents && strings.TrimSpace(agents) == "" {
			agentContent = nil
		}
		if e = projectChanges(root, map[string]*string{"AGENTS.md": agentContent, ".ghship/README.md": nil, ".ghship/install.json": nil}); e != nil {
			return e
		}
		if e = initialize([]string{"--project", root, "--remove-binding"}, nil, io.Discard); e != nil {
			return e
		}
	default:
		return usage("unknown project command")
	}
	return emitResult(out, *j, map[string]any{"action": args[0], "project": root, "localChangesPendingReview": true, "customizationPreserved": true, "platformRulesChanged": false})
}

type projectSkillRecord struct {
	Path      string   `json:"path"`
	Mode      string   `json:"mode"`
	Overrides []string `json:"overrides"`
	Content   string   `json:"content"`
}

var skillScopes = map[string]bool{"planning": true, "development": true, "testing": true, "acceptance": true, "delivery": true}

func readProjectSkill(root string) (projectSkillRecord, error) {
	p := filepath.Join(root, ".ghship", "skill.md")
	r := projectSkillRecord{Path: p, Overrides: []string{}}
	if e := noLinks(p); e != nil {
		return r, e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return r, e
	}
	r.Content = string(b)
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return r, errors.New("project skill needs explicit mode frontmatter")
	}
	split := strings.Index(text[4:], "\n---\n")
	if split < 0 {
		return r, errors.New("invalid project skill frontmatter")
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(text[4:4+split], "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return r, errors.New("invalid project skill field")
		}
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if seen[key] {
			return r, errors.New("duplicate project skill field")
		}
		seen[key] = true
		switch key {
		case "mode":
			r.Mode = value
		case "overrides":
			if !strings.HasPrefix(value, "[") || !strings.HasSuffix(value, "]") {
				return r, errors.New("overrides must be an explicit inline list")
			}
			for _, s := range strings.Split(strings.Trim(value, "[]"), ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					if !skillScopes[s] {
						return r, errors.New("unknown skill override scope")
					}
					for _, existing := range r.Overrides {
						if existing == s {
							return r, errors.New("duplicate skill override scope")
						}
					}
					r.Overrides = append(r.Overrides, s)
				}
			}
		default:
			return r, errors.New("unknown project skill metadata field")
		}
	}
	if r.Mode != "supplement" && r.Mode != "override" {
		return r, errors.New("mode must be supplement or override")
	}
	if r.Mode == "override" && len(r.Overrides) == 0 {
		return r, errors.New("override needs explicit scopes")
	}
	if r.Mode == "supplement" && len(r.Overrides) > 0 {
		return r, errors.New("supplement cannot declare overrides")
	}
	return r, nil
}
func projectSkill(args []string, out io.Writer) error {
	f := flag.NewFlagSet("project skill", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	path := f.String("project", ".", "project")
	show := f.Bool("show", false, "show effective local customization")
	create := f.Bool("create", false, "create user-owned template")
	mode := f.String("mode", "supplement", "mode")
	scopes := f.String("overrides", "", "comma-separated scopes")
	j := f.Bool("json", false, "JSON")
	if e := f.Parse(args); e != nil || f.NArg() != 0 || *show == *create {
		return usage("choose project skill --show or --create")
	}
	root, e := projectRoot(*path)
	if e != nil {
		return e
	}
	if *create {
		if *mode != "supplement" && *mode != "override" {
			return usage("invalid skill mode")
		}
		list := []string{}
		if *scopes != "" {
			for _, scope := range strings.Split(*scopes, ",") {
				if !skillScopes[scope] {
					return usage("unknown override scope")
				}
				for _, existing := range list {
					if existing == scope {
						return usage("duplicate override scope")
					}
				}
				list = append(list, scope)
			}
		}
		if (*mode == "override" && len(list) == 0) || (*mode == "supplement" && len(list) > 0) {
			return usage("override requires scopes; supplement has none")
		}
		p := filepath.Join(root, ".ghship", "skill.md")
		if e = noLinks(p); e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(p), 0755); e != nil {
			return e
		}
		file, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if e != nil {
			return errors.New("project customization already exists or cannot be created; preserved")
		}
		_, e = fmt.Fprintf(file, "---\nmode: %s\noverrides: [%s]\n---\n\n# Project workflow\n\nDescribe actual documentation, test commands, acceptance and delivery requirements.\nReplace this instructional text; no tests or permissions are implied by this template.\n", *mode, strings.Join(list, ", "))
		ce := file.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
	}
	r, e := readProjectSkill(root)
	if os.IsNotExist(e) && *show {
		return emitResult(out, *j, map[string]any{"project": root, "mode": "global_defaults", "source": nil, "overrides": []string{}})
	}
	if e != nil {
		return e
	}
	return emitResult(out, *j, r)
}
