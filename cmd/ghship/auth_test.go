package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func setupAuthHome(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	h, _ = filepath.EvalSymlinks(h)
	t.Setenv("HOME", h)
	t.Setenv("USERPROFILE", h)
	t.Setenv("APPDATA", filepath.Join(h, "AppData"))
	return h
}
func testAPI(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	s := httptest.NewServer(handler)
	old := apiBaseOverride
	apiBaseOverride = s.URL
	t.Cleanup(func() { apiBaseOverride = old; s.Close() })
}
func TestAuthIdentityRotationAndReferences(t *testing.T) {
	home := setupAuthHome(t)
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Error(r.URL.Path)
		}
		if r.Header.Get("Authorization") == "Bearer invalid" {
			w.WriteHeader(403)
			fmt.Fprint(w, "secret invalid")
			return
		}
		fmt.Fprint(w, `{"login":"developer"}`)
	})
	var out bytes.Buffer
	save := func(token string) error {
		return accountMutation([]string{"set", "--user", "developer", "--token-stdin", "--non-interactive", "--json"}, tokenJSONReader(token), &out)
	}
	if e := save("original-secret"); e != nil {
		t.Fatal(e)
	}
	p, _ := accountPath("github.com", "developer")
	before, _ := os.ReadFile(p)
	if e := save("invalid"); e == nil || strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatal("failed identity replaced credential")
	}
	if e := save("rotated-secret"); e != nil {
		t.Fatal(e)
	}
	api, e := accountAPI("github.com", "developer")
	if e != nil || api.token != "rotated-secret" {
		t.Fatal(e)
	}
	root := filepath.Join(home, "product")
	binding := projectBinding{SchemaVersion: 1, Root: root, Repo: "github.com/team/product", Author: "developer", Reviewer: "reviewer"}
	b, _ := json.Marshal(binding)
	bp, _ := bindingPath(root)
	if e = atomicConfig(bp, b); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e = listUsers([]string{"--json"}, &out); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(out.String(), "secret") || !strings.Contains(out.String(), "product") {
		t.Fatal(out.String())
	}
	if e = accountMutation([]string{"remove", "--user", "developer"}, nil, &out); e == nil {
		t.Fatal("deleted referenced credential")
	}
	if e = initialize([]string{"--project", root, "--remove-binding"}, nil, &out); e != nil {
		t.Fatal(e)
	}
	if e = accountMutation([]string{"remove", "--user", "developer"}, nil, &out); e != nil {
		t.Fatal(e)
	}
}
func TestInvitationTargetAndIdempotency(t *testing.T) {
	accepted := false
	invited := 0
	accepts := 0
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /user":
			fmt.Fprint(w, `{"login":"reviewer"}`)
		case "GET /repos/team/product/collaborators/reviewer/permission":
			if accepted {
				fmt.Fprint(w, `{"permission":"write"}`)
			} else {
				w.WriteHeader(404)
			}
		case "GET /repos/team/product/invitations":
			fmt.Fprint(w, `[]`)
		case "PUT /repos/team/product/collaborators/reviewer":
			invited++
			var b map[string]string
			json.NewDecoder(r.Body).Decode(&b)
			if b["permission"] != "push" {
				t.Fatal(b)
			}
			w.WriteHeader(201)
			fmt.Fprint(w, `{"id":42}`)
		case "GET /user/repository_invitations":
			fmt.Fprint(w, `[{"id":7,"repository":{"full_name":"other/repo"},"invitee":{"login":"reviewer"}},{"id":42,"repository":{"full_name":"team/product"},"invitee":{"login":"reviewer"}}]`)
		case "PATCH /user/repository_invitations/42":
			if r.Header.Get("Authorization") != "Bearer reviewer-token" {
				t.Fatal("wrong acceptance account")
			}
			accepted = true
			accepts++
			w.WriteHeader(204)
		default:
			t.Error("unexpected API", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	})
	admin := githubAPI{"github.com", "admin-token"}
	member := githubAPI{"github.com", "reviewer-token"}
	if e := ensureCollaborator(admin, member, "team/product", "reviewer", false); e == nil {
		t.Fatal("wrote without configuration authorization")
	}
	for i := 0; i < 2; i++ {
		if e := ensureCollaborator(admin, member, "team/product", "reviewer", true); e != nil {
			t.Fatal(e)
		}
	}
	if invited != 1 || accepts != 1 {
		t.Fatal(invited, accepts)
	}
}
func TestAuthNoninteractiveMissingAndWrongIdentity(t *testing.T) {
	setupAuthHome(t)
	var out bytes.Buffer
	if e := accountMutation([]string{"set", "--user", "dev", "--non-interactive"}, nil, &out); e == nil {
		t.Fatal("missing token accepted")
	}
	testAPI(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"login":"other"}`) })
	if e := accountMutation([]string{"set", "--user", "dev", "--token-stdin", "--non-interactive"}, tokenJSONReader("sensitive-token"), &out); e == nil || strings.Contains(e.Error(), "sensitive-token") {
		t.Fatal(e)
	}
	p, _ := accountPath("github.com", "dev")
	if _, e := os.Stat(p); !os.IsNotExist(e) {
		t.Fatal("wrong identity stored")
	}
}

func TestUserListValidityExpiryAndNoSecrets(t *testing.T) {
	setupAuthHome(t)
	phase := "save"
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if phase == "save" {
			fmt.Fprint(w, `{"login":"developer"}`)
			return
		}
		switch phase {
		case "valid":
			w.Header().Set("GitHub-Authentication-Token-Expiration", "2027-01-02 03:04:05 UTC")
			fmt.Fprint(w, `{"login":"developer"}`)
		case "invalid":
			w.WriteHeader(401)
			fmt.Fprint(w, "never-print-token")
		case "forbidden":
			w.WriteHeader(403)
		case "mismatch":
			fmt.Fprint(w, `{"login":"other"}`)
		}
	})
	var out bytes.Buffer
	add := []string{"add", "--user", "developer", "--token-stdin", "--non-interactive"}
	if e := users(add, tokenJSONReader("never-print-token"), &out); e != nil {
		t.Fatal(e)
	}
	if e := users(add, tokenJSONReader("never-print-token"), &out); e == nil {
		t.Fatal("duplicate add accepted")
	}
	for _, tc := range []struct{ phase, status string }{{"valid", "valid"}, {"invalid", "invalid"}, {"forbidden", "unknown"}, {"mismatch", "identity_mismatch"}} {
		phase = tc.phase
		out.Reset()
		if e := users([]string{"list", "--json"}, nil, &out); e != nil {
			t.Fatal(e)
		}
		var rows []map[string]any
		if e := json.Unmarshal(out.Bytes(), &rows); e != nil {
			t.Fatal(e)
		}
		if len(rows) != 1 || rows[0]["status"] != tc.status || strings.Contains(out.String(), "never-print-token") {
			t.Fatal(out.String())
		}
		if phase == "valid" && rows[0]["expiresAt"] != "2027-01-02T03:04:05Z" {
			t.Fatal(rows)
		}
		if phase != "valid" && rows[0]["expiresAt"] != nil {
			t.Fatal(rows)
		}
	}
}

func TestUnsafeExistingCredentialDirectoryRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file mode contract")
	}
	setupAuthHome(t)
	root, _ := configRoot()
	dir := filepath.Join(root, "accounts", "github.com")
	os.MkdirAll(dir, 0700)
	os.Chmod(dir, 0777)
	testAPI(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"login":"developer"}`) })
	var out bytes.Buffer
	e := users([]string{"add", "--user", "developer", "--token-stdin", "--non-interactive"}, tokenJSONReader("secret"), &out)
	if e == nil {
		t.Fatal("unsafe directory accepted")
	}
	p, _ := accountPath("github.com", "developer")
	if _, e = os.Stat(p); !os.IsNotExist(e) {
		t.Fatal("saved secret in unsafe directory")
	}
}

func TestUserTextTokenInputsShareStore(t *testing.T) {
	setupAuthHome(t)
	calls := 0
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") == "Bearer rejected-secret" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"login":"developer"}`)
	})
	var out bytes.Buffer
	add := []string{"add", "--user", "developer", "--token", "  original-secret \n", "--non-interactive", "--json"}
	if e := users(add, nil, &out); e != nil {
		t.Fatal(e)
	}
	p, _ := accountPath("github.com", "developer")
	before, _ := os.ReadFile(p)
	if e := users([]string{"update", "--user", "developer", "--token=rejected-secret"}, nil, &out); e == nil {
		t.Fatal("invalid token saved")
	}
	after, _ := os.ReadFile(p)
	if !bytes.Equal(before, after) {
		t.Fatal("failed update changed account")
	}
	if e := users([]string{"update", "--user=developer", "--token=rotated-secret"}, nil, &out); e != nil {
		t.Fatal(e)
	}
	api, e := accountAPI("github.com", "developer")
	if e != nil || api.token != "rotated-secret" {
		t.Fatal("different store", e)
	}
	for _, tail := range [][]string{
		{"--token=secret", "--token-stdin"}, {"--token=secret", "--token-file=/unused"},
		{"--token-file=", "--token-stdin"}, {"--token="}, {"--token-file="}, {"--token-stdin=false"},
		{"--token=one\ntwo"}, {"--token=token=secret"}, {"--token=#secret"}, {"--token=\"secret\""},
		{"--token={'token':'secret'}"}, {"--token=one two"}, {"--token=" + strings.Repeat("x", 16385)},
	} {
		n := calls
		err := users(append([]string{"update", "--user", "developer", "--non-interactive"}, tail...), strings.NewReader("secret"), &out)
		if err == nil || calls != n || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid input reached API or leaked token", tail[0][:min(20, len(tail[0]))])
		}
	}
	if strings.Contains(out.String(), "secret") {
		t.Fatal("token leaked in output")
	}
	n := calls
	if e := users([]string{"delete", "--user", "developer", "--token=secret"}, nil, &out); e == nil || calls != n {
		t.Fatal("delete accepted token")
	}
}

func TestUserJSONFileAndTextContract(t *testing.T) {
	setupAuthHome(t)
	calls := 0
	testAPI(t, func(w http.ResponseWriter, r *http.Request) { calls++; fmt.Fprint(w, `{"login":"developer"}`) })
	tokenDir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	tokenPath := filepath.Join(tokenDir, "token.json")
	if e := os.WriteFile(tokenPath, []byte(`{
 "token": "  file-secret "
}
`), 0600); e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	if e := users([]string{"add", "--user", "developer", "--token-file", tokenPath}, nil, &out); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(tokenPath); e != nil {
		t.Fatal("input file removed")
	}
	for _, source := range []string{"file", "stdin", "text"} {
		for _, value := range []string{"", "one\ntwo", "one\u00a0two", "token=secret", "#secret", "\"secret\"", "{\"token\":\"secret\"}", strings.Repeat(" ", 16384) + "x"} {
			args := []string{"update", "--user", "developer", "--non-interactive"}
			switch source {
			case "file":
				encoded, _ := json.Marshal(map[string]string{"token": value})
				if e := os.WriteFile(tokenPath, encoded, 0600); e != nil {
					t.Fatal(e)
				}
				args = append(args, "--token-file", tokenPath)
			case "stdin":
				args = append(args, "--token-stdin")
			case "text":
				args = append(args, "--token="+value)
			}
			n := calls
			input := io.Reader(strings.NewReader(value))
			if source == "stdin" {
				input = tokenJSONReader(value)
			}
			if e := users(args, input, &out); e == nil || calls != n {
				t.Fatal("invalid source reached API", source)
			}
		}
	}
	api, e := accountAPI("github.com", "developer")
	if e != nil || api.token != "file-secret" {
		t.Fatal("rejected input changed account", e)
	}
	if strings.Contains(out.String(), "secret") {
		t.Fatal("secret leaked")
	}
}

func tokenJSONReader(token string) io.Reader {
	b, _ := json.Marshal(map[string]string{"token": token})
	return bytes.NewReader(b)
}

func TestCredentialJSONSourcesStrictAndAtomic(t *testing.T) {
	setupAuthHome(t)
	calls := 0
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") == "Bearer rejected-secret" {
			w.WriteHeader(401)
			return
		}
		fmt.Fprint(w, `{"login":"developer"}`)
	})
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(dir, "developer.json")
	var out bytes.Buffer
	if e = users([]string{"add", "--user", "developer", "--token", "original-secret"}, nil, &out); e != nil {
		t.Fatal(e)
	}
	for _, source := range []string{"file", "stdin"} {
		args := []string{"update", "--user", "developer", "--non-interactive"}
		if source == "file" {
			args = append(args, "--token-file", file)
		} else {
			args = append(args, "--token-stdin")
		}
		run := func(value string) error {
			if e := os.WriteFile(file, []byte(value), 0600); e != nil {
				t.Fatal(e)
			}
			return users(args, strings.NewReader(value), &out)
		}
		if e = run(`{"token":"rotated-secret"}`); e != nil {
			t.Fatal(source, e)
		}
		api, e := accountAPI("github.com", "developer")
		if e != nil || api.token != "rotated-secret" {
			t.Fatal(source, e)
		}
		p, _ := accountPath("github.com", "developer")
		before, _ := os.ReadFile(p)
		for _, value := range []string{
			`secret`, `{}`, `[]`, `null`, `{"token":null}`, `{"token":1}`, `{"token":{}}`,
			`{"token":"secret","token":"second"}`, `{"token":"secret","host":"github.com"}`,
			`{"user":"developer","token":"secret"}`, `{"token":"secret"} {}`, `{"token":"secret"} garbage`,
			`{"token":"secret",}`, `{"token":""}`, `{"token":"one\ntwo"}`, `{"token":"secret"`,
			strings.Repeat(" ", 16384) + `{"token":"secret"}`,
		} {
			n := calls
			e = run(value)
			if e == nil || calls != n || strings.Contains(e.Error(), "secret") {
				t.Fatal("invalid JSON reached API or leaked secret", source)
			}
		}
		if e = run(`{"token":"rejected-secret"}`); e == nil || strings.Contains(e.Error(), "secret") {
			t.Fatal("invalid credential accepted/leaked")
		}
		after, _ := os.ReadFile(p)
		if !bytes.Equal(before, after) {
			t.Fatal("failed input changed account")
		}
		if _, e = os.Stat(file); e != nil {
			t.Fatal("input removed")
		}
	}
	if strings.Contains(out.String(), "secret") {
		t.Fatal("secret output")
	}
}

func TestOwnedConfigurationDirectoriesPreparedAutomatically(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix directory permissions")
	}
	setupAuthHome(t)
	root, _ := configRoot()
	hostDir := filepath.Join(root, "accounts", "github.com")
	if e := os.MkdirAll(hostDir, 0700); e != nil {
		t.Fatal(e)
	}
	parent := filepath.Dir(root)
	if e := os.Chmod(parent, 0755); e != nil {
		t.Fatal(e)
	}
	directories := []string{root, filepath.Join(root, "accounts"), hostDir}
	for _, dir := range directories {
		if e := os.Chmod(dir, 0755); e != nil {
			t.Fatal(e)
		}
	}
	marker := filepath.Join(root, "user-owned.txt")
	os.WriteFile(marker, []byte("preserve"), 0644)
	testAPI(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"login":"developer"}`) })
	var out bytes.Buffer
	if e := users([]string{"add", "--user", "developer", "--token", "fixture-secret", "--non-interactive"}, nil, &out); e != nil {
		t.Fatal(e)
	}
	for _, dir := range directories {
		st, e := os.Stat(dir)
		if e != nil || st.Mode().Perm() != 0700 {
			t.Fatal("not secured", dir, e)
		}
	}
	expected := filepath.Join(root, "accounts", "github.com", "developer.json")
	st, e := os.Stat(expected)
	if e != nil || st.Mode().Perm() != 0600 {
		t.Fatal("account path/mode incorrect", e)
	}
	os.Chmod(root, 0755)
	if _, e := readAccount("github.com", "developer"); e != nil {
		t.Fatal(e)
	}
	st, _ = os.Stat(root)
	if st.Mode().Perm() != 0700 {
		t.Fatal("read did not prepare owned directory")
	}
	if _, e := readAccount("github.com", "developer"); e != nil {
		t.Fatal("not idempotent", e)
	}
	st, _ = os.Stat(parent)
	if st.Mode().Perm() != 0755 {
		t.Fatal("shared config parent mutated")
	}
	b, _ := os.ReadFile(marker)
	st, _ = os.Stat(marker)
	if string(b) != "preserve" || st.Mode().Perm() != 0644 {
		t.Fatal("unrelated file mutated")
	}
	if e := secureConfigDirs(parent); e == nil {
		t.Fatal("outside root accepted")
	}
}

func TestUserLifecycleWithSystemFiles(t *testing.T) {
	setupAuthHome(t)
	root, _ := configRoot()
	dir := filepath.Join(root, "accounts")
	host := filepath.Join(dir, "github.com")
	if e := os.MkdirAll(host, 0700); e != nil {
		t.Fatal(e)
	}
	ignored := []string{filepath.Join(dir, ".DS_Store"), filepath.Join(dir, "notes.txt"), filepath.Join(host, ".DS_Store"), filepath.Join(host, ".ghship-save-interrupted"), filepath.Join(host, ".metadata.json")}
	for _, p := range ignored {
		if e := os.WriteFile(p, []byte("preserve-system-file"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	testAPI(t, func(w http.ResponseWriter, r *http.Request) {
		login := "developer"
		if r.Header.Get("Authorization") == "Bearer acceptor-secret" {
			login = "acceptor"
		}
		fmt.Fprintf(w, `{"login":%q}`, login)
	})
	var out bytes.Buffer
	invoke := func(args ...string) {
		out.Reset()
		if e := run(append([]string{"user"}, args...), nil, &out); e != nil {
			t.Fatal(args, e)
		}
		if strings.Contains(out.String(), "secret") {
			t.Fatal("secret output")
		}
	}
	list := func(args ...string) []map[string]any {
		invoke(append([]string{"list", "--json"}, args...)...)
		var rows []map[string]any
		if e := json.Unmarshal(out.Bytes(), &rows); e != nil {
			t.Fatal(e)
		}
		return rows
	}
	for _, login := range []string{"developer", "acceptor"} {
		invoke("add", "--user", login, "--token", login+"-secret", "--non-interactive")
	}
	rows := list()
	if len(rows) != 2 {
		t.Fatal("accounts missing", rows)
	}
	for _, r := range rows {
		if r["status"] != "valid" || r["host"] != "github.com" {
			t.Fatal(r)
		}
	}
	rows = list("--host", "github.com", "--user", "developer")
	if len(rows) != 1 || rows[0]["login"] != "developer" {
		t.Fatal("filter incorrect", rows)
	}
	invoke("update", "--user", "developer", "--token", "rotated-secret", "--non-interactive")
	rows = list()
	if len(rows) != 2 {
		t.Fatal("update lost accounts")
	}
	invoke("delete", "--user", "developer")
	rows = list()
	if len(rows) != 1 || rows[0]["login"] != "acceptor" {
		t.Fatal("delete/list incorrect", rows)
	}
	invoke("delete", "--user", "acceptor")
	rows = list()
	if len(rows) != 0 || strings.TrimSpace(out.String()) != "[]" {
		t.Fatal("empty list incorrect", out.String())
	}
	for _, p := range ignored {
		b, e := os.ReadFile(p)
		if e != nil || string(b) != "preserve-system-file" {
			t.Fatal("unrelated file changed", p, e)
		}
	}
}

func TestUserListUnavailableAccountDoesNotHideHealthyAccount(t *testing.T) {
	setupAuthHome(t)
	testAPI(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"login":"developer"}`) })
	var out bytes.Buffer
	if e := users([]string{"add", "--user", "developer", "--token", "fixture-secret"}, nil, &out); e != nil {
		t.Fatal(e)
	}
	root, _ := configRoot()
	if e := os.WriteFile(filepath.Join(root, "accounts", "github.com", "acceptor.json"), []byte("invalid metadata"), 0600); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	if e := users([]string{"list", "--json"}, nil, &out); e != nil {
		t.Fatal(e)
	}
	var rows []map[string]any
	json.Unmarshal(out.Bytes(), &rows)
	status := map[string]any{}
	for _, r := range rows {
		status[r["login"].(string)] = r["status"]
	}
	if len(rows) != 2 || status["developer"] != "valid" || status["acceptor"] != "unavailable" {
		t.Fatal("healthy account hidden or unknown fabricated", rows)
	}
}

func TestUserListDoesNotFollowHostSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges differ")
	}
	setupAuthHome(t)
	root, _ := configRoot()
	dir := filepath.Join(root, "accounts")
	os.MkdirAll(dir, 0700)
	if e := os.Symlink(t.TempDir(), filepath.Join(dir, "github.com")); e != nil {
		t.Fatal(e)
	}
	if e := users([]string{"list", "--json"}, nil, &bytes.Buffer{}); e == nil {
		t.Fatal("followed host symlink")
	}
}
