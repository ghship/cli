package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
)

const gitHelp = `ghship git ROLE -- GIT_ARGUMENTS...
Verifies the current project's author/reviewer/dispatcher identity, then invokes
Git with ephemeral gh credentials. No persistent helper/global account changes,
no exported token, no shell interpolation of user arguments. Native exit preserved.
Use git directly for local operations; use this for authenticated push/fetch.
Example: ghship git author -- push -u origin HEAD
`

func roleGit(args []string, in io.Reader, out io.Writer) error {
	if len(args) < 3 || args[1] != "--" {
		return usage("use ghship git ROLE -- GIT_ARGUMENTS...")
	}
	env, e := roleEnvironment(args[0])
	if e != nil {
		return e
	}
	host := "github.com"
	for _, v := range env {
		if strings.HasPrefix(v, "GH_HOST=") {
			host = strings.TrimPrefix(v, "GH_HOST=")
		}
	}
	a := append([]string{"-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential", "-c", "url.https://" + host + "/.insteadOf=git@" + host + ":"}, args[2:]...)
	c := exec.CommandContext(operationContext, "git", a...)
	c.Env = env
	c.Stdin = in
	c.Stdout = out
	c.Stderr = os.Stderr
	if e = c.Run(); e != nil {
		var exit *exec.ExitError
		if errors.As(e, &exit) {
			return cliError{exit.ExitCode(), errors.New("git command failed")}
		}
		return e
	}
	return nil
}
