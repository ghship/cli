package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/term"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"
)

var validLogin = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`)
var validHost = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*$`)
var apiBaseOverride string // Tests only; never exposed as a command or environment setting.
type account struct {
	SchemaVersion int    `json:"schemaVersion"`
	Host          string `json:"host"`
	Login         string `json:"login"`
	Secret        []byte `json:"secret"`
}
type githubAPI struct{ host, token string }

func (g githubAPI) call(method, path string, body any, target any) (int, error) {
	base := "https://api.github.com"
	if g.host != "github.com" {
		base = "https://" + g.host + "/api/v3"
	}
	if strings.HasSuffix(g.host, ".ghe.com") {
		base = "https://api." + g.host
	}
	if apiBaseOverride != "" {
		base = apiBaseOverride
	}
	var data io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return 0, e
		}
		data = bytes.NewReader(b)
	}
	req, e := http.NewRequestWithContext(operationContext, method, base+"/"+path, data)
	if e != nil {
		return 0, errors.New("invalid GitHub request")
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return 0, errors.New("GitHub request failed; check connectivity")
	}
	defer resp.Body.Close()
	if observation, ok := target.(*identityObservation); ok {
		observation.Expiration = resp.Header.Get("GitHub-Authentication-Token-Expiration")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("GitHub %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if target != nil && resp.StatusCode != 204 {
		if e = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(target); e != nil {
			return resp.StatusCode, errors.New("invalid GitHub response")
		}
	}
	return resp.StatusCode, nil
}
func (g githubAPI) identity(expected string) error {
	var user struct {
		Login string `json:"login"`
	}
	_, e := g.call("GET", "user", nil, &user)
	if e != nil {
		return e
	}
	if !strings.EqualFold(user.Login, expected) {
		return fmt.Errorf("wrong identity: expected %s, got %s", expected, user.Login)
	}
	return nil
}
func configRoot() (string, error) {
	h, e := os.UserHomeDir()
	if e != nil {
		return "", e
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(envDefault("APPDATA", filepath.Join(h, "AppData", "Roaming")), "ghship"), nil
	}
	return filepath.Join(h, ".config", "ghship"), nil
}
func accountPath(host, user string) (string, error) {
	if !validHost.MatchString(host) || !validLogin.MatchString(user) {
		return "", usage("invalid GitHub host or username")
	}
	root, e := configRoot()
	return filepath.Join(root, "accounts", strings.ToLower(host), strings.ToLower(user)+".json"), e
}
func readAccount(host, user string) (account, error) {
	var a account
	p, e := accountPath(host, user)
	if e != nil {
		return a, e
	}
	if e = noLinks(p); e != nil {
		return a, e
	}
	st, e := os.Stat(p)
	if e != nil {
		return a, errors.New("account unavailable; use ghship user add")
	}
	if !st.Mode().IsRegular() {
		return a, errors.New("invalid credential file")
	}
	if e = secureConfigDirs(filepath.Dir(p)); e != nil {
		return a, e
	}
	if e = checkSecretMode(st); e != nil {
		return a, e
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return a, e
	}
	if e = json.Unmarshal(b, &a); e != nil {
		return a, errors.New("invalid account metadata")
	}
	if a.SchemaVersion != 1 || a.Host != strings.ToLower(host) || a.Login != strings.ToLower(user) || len(a.Secret) == 0 {
		return a, errors.New("account metadata does not match requested identity")
	}
	return a, nil
}
func accountAPI(host, user string) (githubAPI, error) {
	a, e := readAccount(host, user)
	if e != nil {
		return githubAPI{}, e
	}
	b, e := unprotectSecret(a.Secret)
	if e != nil {
		return githubAPI{}, errors.New("credential cannot be decrypted by this user")
	}
	return githubAPI{a.Host, string(b)}, nil
}
func atomicConfig(path string, b []byte) error {
	if e := noLinks(path); e != nil {
		return e
	}
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	if e := secureConfigDirs(filepath.Dir(path)); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".ghship-save-*")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if e = noLinks(path); e != nil {
		return e
	}
	return os.Rename(name, path)
}
func readTokenFile(p string) (string, error) {
	absolute, e := filepath.Abs(p)
	if e != nil {
		return "", e
	}
	if e = noLinks(absolute); e != nil {
		return "", e
	}
	root, e := output(nil, "git", "rev-parse", "--show-toplevel")
	if e == nil {
		root, _ = filepath.EvalSymlinks(root)
		rel, relErr := filepath.Rel(root, absolute)
		if relErr != nil {
			// Windows cannot express a relative path between different volumes.
			if runtime.GOOS != "windows" || strings.EqualFold(filepath.VolumeName(root), filepath.VolumeName(absolute)) {
				return "", errors.New("cannot determine token file repository scope")
			}
		} else if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", errors.New("token file must be outside repository")
		}
	}
	st, e := os.Stat(absolute)
	if e != nil || !st.Mode().IsRegular() {
		return "", errors.New("token file unavailable")
	}
	if e = checkSecretMode(st); e != nil {
		return "", e
	}
	input, e := os.Open(absolute)
	if e != nil {
		return "", errors.New("token file unreadable")
	}
	defer input.Close()
	return readCredentialJSON(input)
}
func readCredentialJSON(input io.Reader) (string, error) {
	if input == nil {
		return "", usage("missing credential JSON input")
	}
	b, e := io.ReadAll(io.LimitReader(input, 16385))
	if e != nil {
		return "", errors.New("credential JSON unreadable")
	}
	if len(b) > 16384 {
		return "", usage("token input too large")
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	invalid := usage("credential JSON must contain exactly one string field: token")
	opening, e := decoder.Token()
	if e != nil || opening != json.Delim('{') {
		return "", invalid
	}
	key, e := decoder.Token()
	if e != nil || key != "token" {
		return "", invalid
	}
	value, e := decoder.Token()
	token, ok := value.(string)
	if e != nil || !ok {
		return "", invalid
	}
	closing, e := decoder.Token()
	if e != nil || closing != json.Delim('}') {
		return "", invalid
	}
	if _, e = decoder.Token(); e != io.EOF {
		return "", invalid
	}
	return strings.TrimSpace(token), nil
}

type tokenInput struct {
	Text, File                      string
	Stdin, Explicit, NonInteractive bool
}

func readTokenInput(source tokenInput, in io.Reader) (string, error) {
	var token string
	var e error
	switch {
	case source.Text != "":
		if len(source.Text) > 16384 {
			return "", usage("token input too large")
		}
		token = strings.TrimSpace(source.Text)
	case source.File != "":
		token, e = readTokenFile(source.File)
	case source.Stdin:
		token, e = readCredentialJSON(in)
	case source.Explicit:
		return "", usage("empty token input source")
	case !source.NonInteractive && term.IsTerminal(int(os.Stdin.Fd())):
		fmt.Fprint(os.Stderr, "GitHub token (hidden): ")
		var b []byte
		b, e = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if len(b) > 16384 {
			return "", usage("token input too large")
		}
		token = strings.TrimSpace(string(b))
	default:
		return "", usage("missing token input; use --token, --token-file or --token-stdin")
	}
	if e != nil {
		return "", e
	}
	if token == "" || len(token) > 16384 || strings.ContainsAny(token, "\"'{}=#") || strings.IndexFunc(token, unicode.IsSpace) >= 0 {
		return "", usage("invalid token input")
	}
	return token, nil
}
func accountMutation(args []string, in io.Reader, out io.Writer) error {
	if len(args) == 0 {
		return usage("account mutation requires set or remove")
	}
	f := flag.NewFlagSet("user", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	host := f.String("host", "github.com", "GitHub host")
	user := f.String("user", "", "username")
	text := f.String("token", "", "token text")
	file := f.String("token-file", "", "external token file")
	stdin := f.Bool("token-stdin", false, "read token from stdin")
	non := f.Bool("non-interactive", false, "no prompts")
	j := f.Bool("json", false, "JSON result")
	if e := f.Parse(args[1:]); e != nil || f.NArg() != 0 {
		return usage("invalid user arguments; use ghship help user")
	}
	sources := 0
	f.Visit(func(v *flag.Flag) {
		if v.Name == "token" || v.Name == "token-file" || v.Name == "token-stdin" {
			sources++
		}
	})
	if sources > 1 {
		return usage("choose exactly one of --token, --token-file or --token-stdin")
	}
	if args[0] == "remove" && sources != 0 {
		return usage("delete does not accept token input")
	}
	*host = strings.ToLower(*host)
	*user = strings.ToLower(*user)
	if !validHost.MatchString(*host) {
		return usage("invalid host")
	}
	switch args[0] {
	case "set", "add", "update", "remove":
		p, e := accountPath(*host, *user)
		if e != nil {
			return e
		}
		if e = noLinks(p); e != nil {
			return e
		}
		_, statErr := os.Lstat(p)
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		if args[0] == "add" && statErr == nil {
			return errors.New("account exists; use ghship user update")
		}
		if args[0] == "update" && os.IsNotExist(statErr) {
			return errors.New("account missing; use ghship user add")
		}
		if args[0] == "remove" {
			refs, e := accountReferences(*host, *user)
			if e != nil {
				return e
			}
			if len(refs) > 0 {
				return errors.New("account is referenced; remove or change project bindings first")
			}
			if _, e = readAccount(*host, *user); e != nil {
				return e
			}
			if e = os.Remove(p); e != nil {
				return e
			}
			return emitResult(out, *j, map[string]any{"removed": *user})
		}
		token, e := readTokenInput(tokenInput{*text, *file, *stdin, sources != 0, *non}, in)
		if e != nil {
			return e
		}
		api := githubAPI{*host, token}
		if e = api.identity(*user); e != nil {
			return e
		}
		secret, e := protectSecret([]byte(token))
		if e != nil {
			return errors.New("credential protection failed; not saved")
		}
		b, _ := json.Marshal(account{1, *host, *user, secret})
		if e = atomicConfig(p, b); e != nil {
			return e
		}
		return emitResult(out, *j, map[string]any{"host": *host, "login": *user, "saved": true})
	default:
		return usage("unknown account operation")
	}
}
func emitResult(out io.Writer, j bool, value any) error {
	if j {
		return json.NewEncoder(out).Encode(value)
	}
	b, e := json.MarshalIndent(value, "", "  ")
	if e == nil {
		_, e = fmt.Fprintln(out, string(b))
	}
	return e
}
func cleanGHEnv() []string {
	e := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GH_TOKEN=") && !strings.HasPrefix(v, "GITHUB_TOKEN=") && !strings.HasPrefix(v, "GH_HOST=") && !strings.HasPrefix(v, "GH_ENTERPRISE_TOKEN=") && !strings.HasPrefix(v, "GITHUB_ENTERPRISE_TOKEN=") {
			e = append(e, v)
		}
	}
	return e
}
func escaped(s string) string { return url.PathEscape(s) }

func githubTokenEnv(host string) string {
	if host == "github.com" || strings.HasSuffix(host, ".ghe.com") {
		return "GH_TOKEN"
	}
	return "GH_ENTERPRISE_TOKEN"
}
func secureConfigDirs(path string) error {
	root, e := configRoot()
	if e != nil {
		return e
	}
	rel, e := filepath.Rel(root, path)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("configuration path is outside configuration root")
	}
	for p := path; ; p = filepath.Dir(p) {
		if e = noLinks(p); e != nil {
			return e
		}
		st, e := os.Stat(p)
		if e != nil {
			return e
		}
		if !st.IsDir() {
			return errors.New("configuration path must be a directory")
		}
		if e = prepareConfigDir(p, st); e != nil {
			return e
		}
		if p == root {
			break
		}
		if filepath.Dir(p) == p {
			return errors.New("configuration path is outside configuration root")
		}
	}
	return nil
}
