package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const userHelp = `ghship user add|update|delete|list [--host github.com] [--user LOGIN] [--json]
add/update: --token TOKEN | --token-file PATH | --token-stdin (choose one).
Without a source, interactive input is hidden. --token-file and --token-stdin read JSON containing
exactly one string field: {"token":"TOKEN"}. Unknown/duplicate fields, trailing
content and invalid JSON are rejected. Token text has no internal whitespace.
Maximum file/input size is 16 KiB. Input files are not deleted.
--non-interactive prevents prompts. --token can appear in shell history/process
arguments; prefer stdin/files for automation. Tokens never print.
add refuses existing identities. update requires an existing identity, validates
new token before atomic replacement. delete refuses identities used by projects.
list validates tokens online, showing status, checkedAt, expiry and project references.
Only account host directories and canonical LOGIN.json entries are listed; unrelated
regular files are ignored and preserved. Host symlinks are refused.
Statuses: valid, invalid (401), identity_mismatch, unavailable, unknown (network/403).
Missing expiry is unknown, never interpreted as no expiration. Validity does not
prove permission for every repository. Neither tokens nor secret response bodies print.
Default storage: Unix ~/.config/ghship/accounts/HOST/LOGIN.json (directories 0700,
files 0600; permission protection, not encryption). Owned configuration directories
without group/other write access are tightened automatically; symlinks, foreign
ownership and shared writable directories are refused. Windows
%APPDATA%\ghship\accounts\HOST\LOGIN.json (current-user DPAPI).
All input methods use this same store; projects reference identities.
Import JSON contains only token; managed account JSON is not an import file.
JSON example: {"token":"github_pat_REPLACE_WITH_YOUR_TOKEN"}
Examples:
 ghship user add --user developer --token TOKEN --non-interactive
 ghship user add --user developer --token-file /secure/dev.json --non-interactive
 ghship user update --user developer --token-stdin --non-interactive < /secure/dev.json
 ghship user list --json
 ghship init --remove-binding --project /work/product
 ghship user delete --user developer
`

type identityObservation struct {
	Login      string `json:"login"`
	Expiration string `json:"-"`
}

func expiryUTC(raw string) *string {
	for _, format := range []string{time.RFC3339, "2006-01-02 15:04:05 MST"} {
		if parsed, e := time.Parse(format, raw); e == nil {
			s := parsed.UTC().Format(time.RFC3339)
			return &s
		}
	}
	return nil
}
func users(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return usage("user requires add, update, delete or list")
	}
	action := args[0]
	if action == "list" {
		return listUsers(args[1:], out)
	}
	if action != "add" && action != "update" && action != "delete" {
		return usage("unknown user command; use ghship help user")
	}
	if action == "delete" {
		action = "remove"
	}
	return accountMutation(append([]string{action}, args[1:]...), in, out)
}
func listUsers(args []string, out io.Writer) error {
	f := flag.NewFlagSet("user list", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	host := f.String("host", "", "optional host filter")
	user := f.String("user", "", "optional login filter")
	j := f.Bool("json", false, "JSON")
	if e := f.Parse(args); e != nil || f.NArg() != 0 {
		return usage("invalid user list arguments")
	}
	*host = strings.ToLower(*host)
	*user = strings.ToLower(*user)
	if *host != "" && !validHost.MatchString(*host) {
		return usage("invalid host")
	}
	if *user != "" && !validLogin.MatchString(*user) {
		return usage("invalid username")
	}
	base, e := configRoot()
	if e != nil {
		return e
	}
	dir := filepath.Join(base, "accounts")
	if e = noLinks(dir); e != nil {
		return e
	}
	hosts, e := os.ReadDir(dir)
	if e != nil && !os.IsNotExist(e) {
		return e
	}
	result := []map[string]any{}
	for _, h := range hosts {
		if h.Type()&os.ModeSymlink != 0 {
			return errors.New("invalid account directory")
		}
		if !h.IsDir() {
			if h.Type().IsRegular() {
				continue
			}
			return errors.New("invalid account directory")
		}
		if *host != "" && h.Name() != *host {
			continue
		}
		if !validHost.MatchString(h.Name()) {
			return errors.New("invalid account host directory")
		}
		entries, e := os.ReadDir(filepath.Join(dir, h.Name()))
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			login := strings.TrimSuffix(entry.Name(), ".json")
			if !validLogin.MatchString(login) || login != strings.ToLower(login) {
				continue
			}
			if *user != "" && login != *user {
				continue
			}
			row := map[string]any{"host": h.Name(), "login": login, "checkedAt": time.Now().UTC().Format(time.RFC3339), "status": "unavailable", "expiresAt": nil}
			refs, e := accountReferences(h.Name(), login)
			if e != nil {
				return e
			}
			row["projects"] = refs
			api, e := accountAPI(h.Name(), login)
			if e == nil {
				var observation identityObservation
				status, e := api.call("GET", "user", nil, &observation)
				row["expiresAt"] = expiryUTC(observation.Expiration)
				switch {
				case status == 401:
					row["status"] = "invalid"
				case e != nil:
					row["status"] = "unknown"
				case !strings.EqualFold(observation.Login, login):
					row["status"] = "identity_mismatch"
				default:
					row["status"] = "valid"
				}
			}
			result = append(result, row)
		}
	}
	return emitResult(out, *j, result)
}
